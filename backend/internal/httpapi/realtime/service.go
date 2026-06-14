package realtime

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

const (
	dashboardChannel                  = "dashboard"
	dashboardSnapshotMessageType      = "dashboard.snapshot"
	dashboardActivityMessageType      = "dashboard.activity"
	analyticsSnapshotMessageType      = "analytics.snapshot"
	analyticsErrorMessageType         = "analytics.error"
	defaultAsyncDashboardQueueSize    = 64
	defaultAsyncDashboardWorkerCount  = 1
	defaultAsyncDashboardTimeout      = 2 * time.Second
	defaultAsyncDashboardDrainTimeout = 3 * time.Second
	defaultRealtimeWriteTimeout       = 2 * time.Second
)

var supportedRealtimeChannels = map[string]struct{}{
	dashboardChannel: {},
	analyticsChannel: {},
}

var supportedAnalyticsPresets = map[string]struct{}{
	"1h":  {},
	"6h":  {},
	"24h": {},
	"7d":  {},
	"30d": {},
	"all": {},
}

type Options struct {
	RealtimePool       *pgxpool.Pool
	AuthService        *managementauth.Service
	CORSOriginProvider platformcors.OriginProvider
	Now                func() time.Time
	DashboardSnapshots *statsdomain.DashboardAggregateStore
}

type pendingDashboardSnapshotPublisher interface {
	PublishPendingDashboardSnapshot(context.Context, int) (bool, error)
}

type pendingAnalyticsUpdatePublisher interface {
	PublishAnalyticsUpdates(context.Context, int) (bool, error)
}

type Service struct {
	pool                       *pgxpool.Pool
	authService                *managementauth.Service
	corsOriginProvider         platformcors.OriginProvider
	manager                    *ConnectionManager
	latestAnalyticsSequenceMu  sync.Mutex
	latestAnalyticsSequenceIDs map[string]int64
	pendingDashboardSnapshots  pendingDashboardSnapshotPublisher
	pendingAnalyticsUpdates    pendingAnalyticsUpdatePublisher
	now                        func() time.Time
	dashboardSnapshots         *statsdomain.DashboardAggregateStore
	limiter                    *realtimeLimiter
	upgrader                   websocket.Upgrader
}

type inboundMessage struct {
	Type      string `json:"type"`
	ProfileID int    `json:"profile_id"`
	Channel   string `json:"channel"`
	Preset    string `json:"preset"`
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	pool := options.RealtimePool
	if pool == nil {
		return nil, fmt.Errorf("realtime database pool is required")
	}
	if options.AuthService == nil {
		return nil, fmt.Errorf("auth service is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	corsOriginProvider := options.CORSOriginProvider
	if corsOriginProvider == nil {
		corsOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}
	dashboardSnapshots := options.DashboardSnapshots
	if dashboardSnapshots == nil {
		dashboardSnapshots = statsdomain.NewDashboardAggregateStore()
	}
	service := &Service{
		pool:                       pool,
		authService:                options.AuthService,
		corsOriginProvider:         corsOriginProvider,
		manager:                    NewConnectionManager(defaultRealtimeWriteTimeout),
		latestAnalyticsSequenceIDs: map[string]int64{},
		now:                        now,
		dashboardSnapshots:         dashboardSnapshots,
		limiter:                    newRealtimeLimiter(),
	}
	dashboardSnapshots.RegisterInvalidationListener(service.handleDashboardAggregateInvalidation)
	options.AuthService.RegisterRealtimeAuthRevocationListener(service.handleRealtimeAuthRevocation)
	service.upgrader = websocket.Upgrader{CheckOrigin: service.checkOrigin}
	return service, nil
}

func (s *Service) Close() {
	if s == nil || s.manager == nil {
		return
	}
	s.manager.Close()
}

func (s *Service) SetAsyncDashboardPublisher(publisher *AsyncDashboardPublisher) {
	if s == nil {
		return
	}
	s.pendingDashboardSnapshots = publisher
}

func (s *Service) SetAsyncAnalyticsPublisher(publisher *AsyncAnalyticsPublisher) {
	if s == nil {
		return
	}
	s.pendingAnalyticsUpdates = publisher
}

func (s *Service) corsSnapshot() platformcors.Snapshot {
	if s == nil || s.corsOriginProvider == nil {
		return platformcors.Snapshot{}
	}
	return s.corsOriginProvider.CORSSnapshot()
}

func (s *Service) MountManagementRoutes(api chi.Router) {
	api.Route("/realtime", func(router chi.Router) {
		router.Get("/ws", s.handleWebSocket)
	})
}

func (s *Service) handleRealtimeAuthRevocation(event managementauth.RealtimeAuthRevocation) {
	if s == nil || s.manager == nil {
		return
	}
	s.manager.CloseAuthenticatedSubject(event.SubjectID, websocket.ClosePolicyViolation)
}

func (s *Service) checkOrigin(request *http.Request) bool {
	corsSnapshot := s.corsSnapshot()
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if corsSnapshot.AllowsOrigin(origin) {
		return true
	}
	originURL, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if !originURL.IsAbs() {
		return false
	}
	host := strings.TrimSpace(request.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(request.Host)
	}
	if host == "" || !strings.EqualFold(originURL.Host, host) {
		return false
	}
	scheme := strings.TrimSpace(request.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	return strings.EqualFold(originURL.Scheme, scheme)
}

func (s *Service) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	releaseAdmission := admission.ReleaseFromContext(r.Context())
	defer releaseAdmission()

	socket, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	connectionID := s.manager.Connect(socket)
	defer s.manager.Disconnect(connectionID)

	connection := s.manager.GetConnection(connectionID)
	if connection == nil {
		_ = socket.Close()
		return
	}

	authState, err := s.authService.ResolveRealtimeAuthState(r.Context(), s.authService.AccessTokenFromRequest(r))
	if err != nil {
		connection.closeWithCode(websocket.CloseInternalServerErr)
		return
	}
	if authState.AuthEnabled && !authState.Authenticated {
		connection.closeWithCode(websocket.ClosePolicyViolation)
		return
	}

	releaseLimiter, ok := s.limiter.Acquire(realtimeLimiterSubject(authState))
	if !ok {
		connection.closeWithCode(websocket.CloseTryAgainLater)
		return
	}
	defer releaseLimiter()

	if !s.manager.MarkAuthenticated(connectionID, authState.SubjectID, authState.TokenVersion) {
		connection.closeWithCode(websocket.CloseInternalServerErr)
		return
	}
	sessionContext := context.WithoutCancel(r.Context())
	releaseAdmission()
	if !connection.SendJSON(map[string]any{"type": "authenticated", "username": authState.Username}) {
		return
	}
	if !connection.SendJSON(map[string]any{"type": "heartbeat"}) {
		return
	}

	for {
		var message inboundMessage
		if err := socket.ReadJSON(&message); err != nil {
			if websocket.IsUnexpectedCloseError(err) || !isMalformedMessageError(err) {
				return
			}
			if !s.sendAnalyticsError(connection, nil, "", "malformed_message", "Malformed realtime message") {
				return
			}
			continue
		}
		if !s.handleInboundMessage(sessionContext, connectionID, connection, message) {
			return
		}
	}
}

func (s *Service) handleInboundMessage(ctx context.Context, connectionID string, connection *RealtimeConnection, message inboundMessage) bool {
	switch message.Type {
	case "subscribe":
		if !connection.authenticated {
			return connection.SendJSON(map[string]any{"type": "error", "message": "Authentication required"})
		}
		channel := normalizeRealtimeChannel(message.Channel)
		if channel == analyticsChannel {
			return s.handleAnalyticsSubscribe(ctx, connectionID, connection, message)
		}
		return s.handleDashboardSubscribe(ctx, connectionID, connection, message, channel)
	case "refresh":
		if normalizeRealtimeChannel(message.Channel) == analyticsChannel {
			return s.handleAnalyticsRefresh(ctx, connectionID, connection, message)
		}
		return true
	case "unsubscribe":
		s.manager.Unsubscribe(connectionID)
		return connection.SendJSON(map[string]any{"type": "unsubscribed"})
	case "unsubscribe_channel":
		channel := strings.TrimSpace(message.Channel)
		if channel == analyticsChannel {
			return s.handleAnalyticsUnsubscribeChannel(connectionID, connection, message)
		}
		if channel == "" {
			return connection.SendJSON(map[string]any{"type": "error", "message": "channel required"})
		}
		if !s.manager.UnsubscribeChannel(connectionID, channel) {
			return connection.SendJSON(map[string]any{"type": "error", "message": "Channel unsubscribe failed"})
		}
		return connection.SendJSON(map[string]any{"type": "unsubscribed", "channel": channel})
	case "ping":
		return connection.SendJSON(map[string]any{"type": "pong"})
	case "pong":
		return true
	default:
		return connection.SendJSON(map[string]any{"type": "error", "message": fmt.Sprintf("Unknown message type: %s", message.Type)})
	}
}

func (s *Service) handleDashboardSubscribe(ctx context.Context, connectionID string, connection *RealtimeConnection, message inboundMessage, channel string) bool {
	if channel == "" {
		channel = dashboardChannel
	}
	if message.ProfileID <= 0 {
		return connection.SendJSON(map[string]any{"type": "error", "message": "profile_id required"})
	}
	if _, ok := supportedRealtimeChannels[channel]; !ok {
		return connection.SendJSON(map[string]any{"type": "error", "message": fmt.Sprintf("Unsupported channel: %s", channel)})
	}
	exists, err := s.profileExists(ctx, message.ProfileID)
	if err != nil {
		return false
	}
	if !exists {
		return connection.SendJSON(map[string]any{"type": "error", "message": fmt.Sprintf("Profile %d not found", message.ProfileID)})
	}
	if !s.manager.Subscribe(connectionID, message.ProfileID, channel) {
		return connection.SendJSON(map[string]any{"type": "error", "message": "Subscription failed"})
	}
	if !connection.SendJSON(map[string]any{"type": "subscribed", "profile_id": message.ProfileID, "channel": channel}) {
		return false
	}
	return true
}

func (s *Service) handleAnalyticsSubscribe(ctx context.Context, connectionID string, connection *RealtimeConnection, message inboundMessage) bool {
	profileID, preset, ok := s.validateAnalyticsScope(ctx, connection, message)
	if !ok {
		return true
	}
	if !s.manager.Subscribe(connectionID, profileID, analyticsChannel, preset) {
		return s.sendAnalyticsError(connection, &profileID, preset, "subscription_failed", "Analytics subscription failed")
	}
	if !connection.SendJSON(map[string]any{"type": "subscribed", "profile_id": profileID, "channel": analyticsChannel, "preset": preset}) {
		return false
	}
	return s.sendAnalyticsSnapshotOrError(ctx, connection, profileID, preset)
}

func (s *Service) handleAnalyticsRefresh(ctx context.Context, connectionID string, connection *RealtimeConnection, message inboundMessage) bool {
	profileID, preset, ok := s.validateAnalyticsScope(ctx, connection, message)
	if !ok {
		return true
	}
	if !s.connectionHasSubscription(connectionID, profileID, analyticsChannel, preset) {
		return s.sendAnalyticsError(connection, &profileID, preset, "scope_not_subscribed", "Analytics scope is not subscribed")
	}
	return s.sendAnalyticsSnapshotOrError(ctx, connection, profileID, preset)
}

func (s *Service) handleAnalyticsUnsubscribeChannel(connectionID string, connection *RealtimeConnection, message inboundMessage) bool {
	preset, ok := s.validateAnalyticsPreset(connection, message)
	if !ok {
		return true
	}
	if !s.manager.UnsubscribeChannel(connectionID, analyticsChannel, preset) {
		return s.sendAnalyticsError(connection, nil, preset, "scope_not_subscribed", "Analytics scope is not subscribed")
	}
	return connection.SendJSON(map[string]any{"type": "unsubscribed", "channel": analyticsChannel, "preset": preset})
}

func (s *Service) validateAnalyticsScope(ctx context.Context, connection *RealtimeConnection, message inboundMessage) (int, string, bool) {
	preset, ok := s.validateAnalyticsPreset(connection, message)
	if !ok {
		return message.ProfileID, preset, false
	}
	if message.ProfileID <= 0 {
		s.sendAnalyticsError(connection, nil, preset, "profile_id_required", "profile_id is required")
		return 0, preset, false
	}
	exists, err := s.profileExists(ctx, message.ProfileID)
	if err != nil {
		return message.ProfileID, preset, false
	}
	if !exists {
		s.sendAnalyticsError(connection, &message.ProfileID, preset, "profile_not_found", fmt.Sprintf("Profile %d not found", message.ProfileID))
		return message.ProfileID, preset, false
	}
	return message.ProfileID, preset, true
}

func (s *Service) validateAnalyticsPreset(connection *RealtimeConnection, message inboundMessage) (string, bool) {
	preset := normalizeAnalyticsPreset(message.Preset)
	if _, ok := supportedAnalyticsPresets[preset]; !ok {
		s.sendAnalyticsError(connection, optionalPositiveProfileID(message.ProfileID), preset, "invalid_preset", "Invalid analytics preset")
		return preset, false
	}
	return preset, true
}

func (s *Service) sendAnalyticsSnapshotOrError(ctx context.Context, connection *RealtimeConnection, profileID int, preset string) bool {
	message, err := s.BuildAnalyticsSnapshot(ctx, profileID, preset, s.now().UTC())
	if err != nil {
		return s.sendAnalyticsError(connection, &profileID, preset, "snapshot_failed", "Failed to build analytics snapshot")
	}
	return connection.SendJSON(message)
}

func (s *Service) sendAnalyticsError(connection *RealtimeConnection, profileID *int, preset string, code string, message string) bool {
	var presetPtr *string
	if strings.TrimSpace(preset) != "" {
		presetPtr = stringPtr(strings.TrimSpace(preset))
	}
	return connection.SendJSON(AnalyticsErrorMessage{Type: analyticsErrorMessageType, Channel: analyticsChannel, ProfileID: profileID, Preset: presetPtr, Code: code, Message: message})
}

func (s *Service) connectionHasSubscription(connectionID string, profileID int, channel string, preset string) bool {
	s.manager.mu.RLock()
	defer s.manager.mu.RUnlock()
	connection := s.manager.connections[connectionID]
	if connection == nil {
		return false
	}
	_, ok := connection.channels[subscriptionRoomKey(profileID, channel, preset)]
	return ok
}

func usageModelStatisticsFromEndpoint(items []statsdomain.EndpointModelStatistic) []statsdomain.UsageModelStatistic {
	models := make([]statsdomain.UsageModelStatistic, 0, len(items))
	for _, item := range items {
		models = append(models, statsdomain.UsageModelStatistic{ModelID: item.ModelID, ModelLabel: item.ModelLabel, RequestCount: item.RequestCount, SuccessCount: item.SuccessCount, FailedCount: item.FailedCount, PricedRequestCount: item.PricedRequestCount, UnpricedRequestCount: item.UnpricedRequestCount, SuccessRate: item.SuccessRate, P50TTFTMS: item.P50TTFTMS, P95TTFTMS: item.P95TTFTMS, TotalTokens: item.TotalTokens, TotalCostMicros: item.TotalCostMicros, AvgOutputRateTPS: item.AvgOutputRateTPS})
	}
	return models
}

func (s *Service) nextAnalyticsSequence(profileID int, preset string) int64 {
	key := fmt.Sprintf("%d:%s", profileID, preset)
	s.latestAnalyticsSequenceMu.Lock()
	defer s.latestAnalyticsSequenceMu.Unlock()
	s.latestAnalyticsSequenceIDs[key]++
	return s.latestAnalyticsSequenceIDs[key]
}

func isMalformedMessageError(err error) bool {
	message := err.Error()
	return strings.Contains(message, "invalid character") || strings.Contains(message, "unknown field") || strings.Contains(message, "cannot unmarshal")
}

func normalizeRealtimeChannel(channel string) string {
	trimmed := strings.TrimSpace(channel)
	if trimmed == "" {
		return dashboardChannel
	}
	return trimmed
}

func normalizeAnalyticsPreset(preset string) string {
	return strings.ToLower(strings.TrimSpace(preset))
}

func optionalPositiveProfileID(profileID int) *int {
	if profileID <= 0 {
		return nil
	}
	return &profileID
}

func realtimeLimiterSubject(authState managementauth.RealtimeAuthState) string {
	if !authState.AuthEnabled || !authState.Authenticated || authState.SubjectID <= 0 {
		return ""
	}
	return fmt.Sprintf("subject:%d", authState.SubjectID)
}

func stringPtr(value string) *string {
	resolved := value
	return &resolved
}

func (s *Service) publishPendingDashboardSnapshot(ctx context.Context, profileID int) (bool, error) {
	if s.pendingDashboardSnapshots != nil {
		return s.pendingDashboardSnapshots.PublishPendingDashboardSnapshot(ctx, profileID)
	}
	return s.PublishPendingDashboardSnapshot(ctx, profileID)
}

func (s *Service) PublishAnalyticsUpdates(ctx context.Context, profileID int) (bool, error) {
	if s.pendingAnalyticsUpdates != nil {
		return s.pendingAnalyticsUpdates.PublishAnalyticsUpdates(ctx, profileID)
	}
	delivered := false
	for _, preset := range s.ActiveAnalyticsScopes(profileID) {
		if scopeDelivered, err := s.PublishLatestAnalyticsSnapshot(ctx, profileID, preset); err != nil {
			return delivered, err
		} else if scopeDelivered {
			delivered = true
		}
	}
	return delivered, nil
}

func (s *Service) PublishLatestAnalyticsSnapshot(ctx context.Context, profileID int, preset string) (bool, error) {
	preset = normalizeAnalyticsPreset(preset)
	if !s.manager.HasSubscribers(profileID, analyticsChannel, preset) {
		return false, nil
	}
	message, err := s.BuildAnalyticsSnapshot(ctx, profileID, preset, s.now().UTC())
	if err != nil {
		return false, err
	}
	return s.manager.BroadcastToProfile(profileID, analyticsChannel, message, preset) > 0, nil
}

func (s *Service) ActiveAnalyticsScopes(profileID int) []string {
	return s.manager.ActiveScopes(profileID, analyticsChannel)
}

func (s *Service) profileExists(ctx context.Context, profileID int) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM profiles WHERE id = $1 AND deleted_at IS NULL)`, profileID).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("load profile %d: %w", profileID, err)
	}
	return found, nil
}

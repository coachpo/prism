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

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

const (
	dashboardChannel           = "dashboard"
	dashboardUpdateMessageType = "dashboard.update"
)

var supportedRealtimeChannels = map[string]struct{}{
	dashboardChannel: {},
}

type Options struct {
	Pool        *pgxpool.Pool
	AuthService *managementauth.Service
	Now         func() time.Time
}

type Service struct {
	pool                *pgxpool.Pool
	ownsPool            bool
	authService         *managementauth.Service
	accessCookieName    string
	allowedOrigins      map[string]struct{}
	manager             *ConnectionManager
	latestMu            sync.Mutex
	latestRequestLogIDs map[int]int
	now                 func() time.Time
	upgrader            websocket.Upgrader
}

type inboundMessage struct {
	Type      string `json:"type"`
	ProfileID int    `json:"profile_id"`
	Channel   string `json:"channel"`
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	pool := options.Pool
	ownsPool := false
	if pool == nil {
		if strings.TrimSpace(settings.DatabaseURL) == "" {
			return nil, fmt.Errorf("database URL is required")
		}
		createdPool, err := pgxpool.New(context.Background(), settings.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("create realtime database pool: %w", err)
		}
		pool = createdPool
		ownsPool = true
	}
	if options.AuthService == nil {
		if ownsPool {
			pool.Close()
		}
		return nil, fmt.Errorf("auth service is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	allowedOrigins := map[string]struct{}{}
	for _, origin := range settings.CORSAllowedOriginsList() {
		allowedOrigins[origin] = struct{}{}
	}
	service := &Service{
		pool:                pool,
		ownsPool:            ownsPool,
		authService:         options.AuthService,
		accessCookieName:    settings.AuthCookieName,
		allowedOrigins:      allowedOrigins,
		manager:             NewConnectionManager(),
		latestRequestLogIDs: map[int]int{},
		now:                 now,
	}
	service.upgrader = websocket.Upgrader{CheckOrigin: service.checkOrigin}
	return service, nil
}

func (s *Service) Close() {
	if s != nil && s.ownsPool && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Service) MountManagementRoutes(api chi.Router) {
	api.Route("/realtime", func(router chi.Router) {
		router.Get("/ws", s.handleWebSocket)
	})
}

func (s *Service) checkOrigin(request *http.Request) bool {
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	_, ok := s.allowedOrigins[origin]
	if ok {
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

	authState, err := s.authService.ResolveRealtimeAuthState(r.Context(), s.accessTokenFromRequest(r))
	if err != nil {
		connection.closeWithCode(websocket.CloseInternalServerErr)
		return
	}
	if authState.AuthEnabled && !authState.Authenticated {
		connection.closeWithCode(websocket.ClosePolicyViolation)
		return
	}

	connection.authenticated = true
	if !connection.SendJSON(map[string]any{"type": "authenticated", "username": authState.Username}) {
		return
	}
	if !connection.SendJSON(map[string]any{"type": "heartbeat"}) {
		return
	}

	for {
		var message inboundMessage
		if err := socket.ReadJSON(&message); err != nil {
			return
		}
		if !s.handleInboundMessage(connectionID, connection, message) {
			return
		}
	}
}

func (s *Service) handleInboundMessage(connectionID string, connection *RealtimeConnection, message inboundMessage) bool {
	switch message.Type {
	case "subscribe":
		if !connection.authenticated {
			return connection.SendJSON(map[string]any{"type": "error", "message": "Authentication required"})
		}
		channel := strings.TrimSpace(message.Channel)
		if channel == "" {
			channel = dashboardChannel
		}
		if message.ProfileID <= 0 {
			return connection.SendJSON(map[string]any{"type": "error", "message": "profile_id required"})
		}
		if _, ok := supportedRealtimeChannels[channel]; !ok {
			return connection.SendJSON(map[string]any{"type": "error", "message": fmt.Sprintf("Unsupported channel: %s", channel)})
		}
		exists, err := s.profileExists(context.Background(), message.ProfileID)
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
		_, _ = s.PublishPendingDashboardUpdate(context.Background(), message.ProfileID)
		return true
	case "unsubscribe":
		s.manager.Unsubscribe(connectionID)
		return connection.SendJSON(map[string]any{"type": "unsubscribed"})
	case "unsubscribe_channel":
		channel := strings.TrimSpace(message.Channel)
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

func (s *Service) accessTokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(s.accessCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func (s *Service) profileExists(ctx context.Context, profileID int) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM profiles WHERE id = $1 AND deleted_at IS NULL)`, profileID).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("load profile %d: %w", profileID, err)
	}
	return found, nil
}

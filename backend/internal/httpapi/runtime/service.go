package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
)

type Options struct {
	ExecutionPool              *pgxpool.Pool
	TelemetryPool              *pgxpool.Pool
	FeedbackPool               *pgxpool.Pool
	HTTPClient                 *http.Client
	RuntimeProxyConfigProvider RuntimeProxyConfigProvider
	Now                        func() time.Time
	Cache                      *SharedCache
	RuntimeState               *loadbalancedomain.LocalRuntimeStateStore
	LogPartitionEnsurer        LogPartitionEnsurer
	AssumeLogPartitionHorizon  bool
	TelemetryOutbox            TelemetryOutboxOptions
	FeedbackPipeline           RuntimeFeedbackPipelineOptions
	SideEffects                RuntimeSideEffectOptions
	Scheduler                  *background.Scheduler
}

type RuntimeProxyConfigSnapshot struct {
	HTTPClient *http.Client
}

type RuntimeProxyConfigProvider interface {
	RuntimeProxyConfigSnapshot() RuntimeProxyConfigSnapshot
}

type Service struct {
	executionPool                *pgxpool.Pool
	telemetryPool                *pgxpool.Pool
	feedbackPool                 *pgxpool.Pool
	feedbackStore                *runtimeFeedbackStore
	httpClient                   *http.Client
	ownsHTTPClient               bool
	runtimeProxyConfigProvider   RuntimeProxyConfigProvider
	staticRuntimeProxyConfig     RuntimeProxyConfigSnapshot
	now                          func() time.Time
	secretEncryptionKey          string
	cache                        *SharedCache
	runtimeState                 *loadbalancedomain.LocalRuntimeStateStore
	requireDurableSuccessHandoff bool
	telemetryOutbox              *runtimeTelemetryOutbox
	feedbackPipeline             *runtimeFeedbackPipeline
	runtimeSideEffects           *RuntimeSideEffectManager
	ownedScheduler               *background.Scheduler
	failedResponseSamplerOnce    sync.Once
	failedResponseSamplers       *failedResponseSamplerLimiter
}

type domainError struct {
	StatusCode               int
	ErrorCode                string
	Detail                   string
	Fields                   map[string]any
	ResolvedTargetModelID    *string
	SelectedTerminalTargetID *int
	PlanningFailure          *runtimePlanningFailureTelemetry
}

func (err *domainError) Error() string {
	return err.Detail
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	executionPool, telemetryPool, feedbackPool, err := resolveRuntimeServicePools(settings, options)
	if err != nil {
		return nil, err
	}
	client := options.HTTPClient
	ownsHTTPClient := false
	if client == nil {
		client = newRuntimeHTTPClient()
		ownsHTTPClient = true
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}
	runtimeState := options.RuntimeState
	if runtimeState == nil {
		runtimeState = loadbalancedomain.NewLocalRuntimeStateStore()
	}
	partitionEnsurer := options.LogPartitionEnsurer
	if partitionEnsurer == nil {
		partitionEnsurer = logretention.NewStore(logretention.Options{Pool: telemetryPool, Now: now})
	}
	logPartitions := newRuntimeLogPartitionCache(partitionEnsurer, now, options.AssumeLogPartitionHorizon)

	scheduler := options.Scheduler
	if scheduler == nil {
		scheduler = background.NewScheduler(background.Config{})
	}
	service := &Service{
		executionPool:                executionPool,
		telemetryPool:                telemetryPool,
		feedbackPool:                 feedbackPool,
		feedbackStore:                newRuntimeFeedbackStore(feedbackPool),
		httpClient:                   client,
		ownsHTTPClient:               ownsHTTPClient,
		runtimeProxyConfigProvider:   options.RuntimeProxyConfigProvider,
		staticRuntimeProxyConfig:     RuntimeProxyConfigSnapshot{HTTPClient: client},
		now:                          now,
		secretEncryptionKey:          settings.SecretEncryptionKey,
		cache:                        options.Cache,
		runtimeState:                 runtimeState,
		requireDurableSuccessHandoff: true,
	}
	telemetryOptions := options.TelemetryOutbox
	telemetryOptions.Scheduler = scheduler
	service.telemetryOutbox = newRuntimeTelemetryOutbox(telemetryPool, service.nowUTC, logPartitions, telemetryOptions)
	service.feedbackPipeline = newRuntimeFeedbackPipeline(service.feedbackStore, service.runtimeState, logPartitions, options.FeedbackPipeline)
	sideEffectOptions := options.SideEffects
	service.runtimeSideEffects = NewRuntimeSideEffectManager(service.telemetryOutbox, sideEffectOptions)
	if options.Scheduler == nil {
		if err := service.RegisterBackgroundWorkers(scheduler); err != nil {
			return nil, err
		}
		if err := scheduler.Start(context.Background()); err != nil {
			return nil, err
		}
		service.ownedScheduler = scheduler
	}
	return service, nil
}

func (s *Service) RegisterBackgroundWorkers(scheduler *background.Scheduler) error {
	if s == nil {
		return nil
	}
	if s.feedbackPipeline != nil {
		if err := s.feedbackPipeline.RegisterBackgroundWorker(scheduler); err != nil {
			return err
		}
	}
	if s.runtimeSideEffects != nil {
		if err := s.runtimeSideEffects.RegisterBackgroundWorker(scheduler); err != nil {
			return err
		}
	}
	if s.telemetryOutbox != nil {
		return s.telemetryOutbox.RegisterBackgroundWorker(scheduler)
	}
	return nil
}

// newRuntimeHTTPClient builds the outbound upstream client. All connection
// counts, idle lifetimes, and timeouts were removed with runtime.transport:
// outbound requests are now subject to no connection or deadline limits.
// MaxIdleConnsPerHost is explicitly unlimited instead of leaving it at the Go
// default of 2 idle connections per host.
func newRuntimeHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DisableCompression:  true,
			MaxIdleConnsPerHost: math.MaxInt32,
		},
	}
}

func (s *Service) runtimeProxyConfigSnapshot() RuntimeProxyConfigSnapshot {
	if s == nil {
		return RuntimeProxyConfigSnapshot{}
	}
	if s.runtimeProxyConfigProvider != nil {
		return s.runtimeProxyConfigProvider.RuntimeProxyConfigSnapshot()
	}
	return s.staticRuntimeProxyConfig
}

func (s *Service) DrainSideEffects() {
	if s == nil {
		return
	}
	if s.runtimeSideEffects != nil {
		result := s.runtimeSideEffects.Close()
		if result.TimedOut || result.ForcedAbandoned > 0 {
			slog.Warn("runtime side effects close timed out", "elapsed", result.Elapsed, "pending", result.Pending, "forced_abandoned", result.ForcedAbandoned)
		}
	}
	if s.telemetryOutbox != nil {
		result := s.telemetryOutbox.Close()
		if result.TimedOut {
			slog.Warn("runtime telemetry outbox close timed out", "elapsed", result.Elapsed, "pending_rows", result.PendingRows, "inflight", result.Inflight)
		}
	}
	if s.feedbackPipeline != nil {
		s.feedbackPipeline.Close()
	}
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.DrainSideEffects()
	if s.ownedScheduler != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.ownedScheduler.Stop(ctx, time.Now().Add(5*time.Second))
	}
	if s.ownsHTTPClient && s.httpClient != nil {
		if closer, ok := s.httpClient.Transport.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
}

func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(s.handleProxy)
}

func (s *Service) nowUTC() time.Time {
	return s.now().UTC()
}

func (s *Service) RuntimeState() *loadbalancedomain.LocalRuntimeStateStore {
	if s == nil {
		return nil
	}
	return s.runtimeState
}

func resolveRuntimeServicePools(settings config.Settings, options Options) (*pgxpool.Pool, *pgxpool.Pool, *pgxpool.Pool, error) {
	_ = settings
	executionPool := options.ExecutionPool
	telemetryPool := options.TelemetryPool
	feedbackPool := options.FeedbackPool
	if executionPool == nil {
		return nil, nil, nil, fmt.Errorf("runtime execution pool is required")
	}
	if telemetryPool == nil {
		return nil, nil, nil, fmt.Errorf("runtime telemetry pool is required")
	}
	if feedbackPool == nil {
		return nil, nil, nil, fmt.Errorf("runtime feedback pool is required")
	}
	if executionPool == telemetryPool {
		return nil, nil, nil, fmt.Errorf("runtime execution and telemetry pools must be distinct")
	}
	if executionPool == feedbackPool {
		return nil, nil, nil, fmt.Errorf("runtime execution and feedback pools must be distinct")
	}
	if telemetryPool == feedbackPool {
		return nil, nil, nil, fmt.Errorf("runtime telemetry and feedback pools must be distinct")
	}
	return executionPool, telemetryPool, feedbackPool, nil
}

func (s *Service) handleProxy(w http.ResponseWriter, r *http.Request) {
	s.handleStreamingProxy(w, r)
}

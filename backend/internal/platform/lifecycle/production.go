package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementbootstrapconfig "github.com/coachpo/prism/backend/internal/httpapi/management/bootstrapconfig"
	managementconfigbundle "github.com/coachpo/prism/backend/internal/httpapi/management/configbundle"
	managementconfigrules "github.com/coachpo/prism/backend/internal/httpapi/management/configrules"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	managementprofiles "github.com/coachpo/prism/backend/internal/httpapi/management/profiles"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementsidecars "github.com/coachpo/prism/backend/internal/httpapi/management/sidecars"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	managementvendors "github.com/coachpo/prism/backend/internal/httpapi/management/vendors"
	realtimeapi "github.com/coachpo/prism/backend/internal/httpapi/realtime"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformdb "github.com/coachpo/prism/backend/internal/platform/db"
	"github.com/coachpo/prism/backend/internal/platform/email/outbox"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
	"github.com/coachpo/prism/backend/internal/platform/managementsideeffects"
	"github.com/coachpo/prism/backend/internal/platform/version"
)

type BootstrapConfigOptions = platformhttp.BootstrapConfigOptions

type ProductionOptions struct {
	BootstrapConfig BootstrapConfigOptions
}

type productionResources struct {
	deps             platformhttp.Dependencies
	scheduler        *background.Scheduler
	realtimeShutdown []ShutdownHook
	sideEffectDrain  []ShutdownHook
	serviceClose     []ShutdownHook
	dbClose          ShutdownHook
}

func NewProductionApp(ctx context.Context, settings config.Settings, options ProductionOptions) (*App, *http.Server, error) {
	resources, err := buildProductionResources(ctx, settings, options)
	if err != nil {
		return nil, nil, err
	}
	server, err := platformhttp.NewServer(settings, platformhttp.ServerOptions{
		BootstrapConfig: options.BootstrapConfig,
		Dependencies:    resources.deps,
	})
	if err != nil {
		return nil, nil, errors.Join(err, resources.cleanupForSetupFailure(ctx))
	}
	if resources.scheduler != nil {
		if err := resources.scheduler.Start(ctx); err != nil {
			return nil, nil, errors.Join(err, resources.cleanupForSetupFailure(ctx))
		}
	}
	app := NewApp(Options{
		HTTPServer:       server,
		RealtimeShutdown: resources.realtimeShutdown,
		SideEffectDrain:  resources.sideEffectDrain,
		SchedulerStop:    resources.schedulerStopHook(),
		ServiceClose:     resources.serviceClose,
		DBClose:          resources.dbClose,
	})
	return app, server, nil
}

func buildProductionResources(ctx context.Context, settings config.Settings, options ProductionOptions) (*productionResources, error) {
	resources := &productionResources{}
	if err := resources.configureHTTPAssembly(settings, options); err != nil {
		return nil, err
	}
	if strings.TrimSpace(settings.DatabaseURL) == "" {
		return resources, nil
	}
	databasePools, err := platformdb.OpenDatabasePools(ctx, settings.DatabaseURL, settings.PostgresPoolsBudgetOrDefault())
	if err != nil {
		return nil, err
	}
	resources.deps.DatabasePools = databasePools
	resources.dbClose = func(context.Context) error {
		databasePools.Close()
		return nil
	}
	if err := resources.configureDatabaseBackedServices(ctx, settings, databasePools); err != nil {
		return nil, errors.Join(err, resources.cleanupForSetupFailure(ctx))
	}
	return resources, nil
}

func (resources *productionResources) configureHTTPAssembly(settings config.Settings, options ProductionOptions) error {
	loadedVersion, err := version.Load()
	if err != nil {
		return err
	}
	hotBootstrapConfigRuntime, err := platformhttp.NewHotBootstrapConfigRuntime(settings)
	if err != nil {
		return err
	}
	resources.deps.Version = loadedVersion
	resources.deps.HotBootstrapConfigRuntime = hotBootstrapConfigRuntime
	resources.deps.CORSOriginProvider = hotBootstrapConfigRuntime
	if strings.TrimSpace(options.BootstrapConfig.ConfigPath) != "" {
		bootstrapConfigService, bootstrapErr := managementbootstrapconfig.NewService(settings, managementbootstrapconfig.Options{
			ConfigPath:         options.BootstrapConfig.ConfigPath,
			LoadedRevision:     options.BootstrapConfig.LoadedRevision,
			LoadedDocumentETag: options.BootstrapConfig.LoadedDocumentETag,
			CORSOriginProvider: hotBootstrapConfigRuntime,
			HotApplyRuntime:    hotBootstrapConfigRuntime,
		})
		if bootstrapErr != nil {
			return bootstrapErr
		}
		resources.deps.BootstrapConfigService = bootstrapConfigService
	}
	return nil
}

type databaseLanePools struct {
	management       *pgxpool.Pool
	runtimeExecution *pgxpool.Pool
	runtimeTelemetry *pgxpool.Pool
	runtimeFeedback  *pgxpool.Pool
	realtime         *pgxpool.Pool
	cacheRefresh     *pgxpool.Pool
	backgroundJobs   *pgxpool.Pool
}

func newDatabaseLanePools(databasePools *platformdb.DatabasePools) databaseLanePools {
	managementPool := databasePools.Management.Raw()
	runtimeExecutionPool := databasePools.RuntimeExecution.Raw()
	runtimeTelemetryPool := databasePools.RuntimeTelemetry.Raw()
	runtimeFeedbackPool := databasePools.RuntimeFeedback.Raw()
	realtimePool := databasePools.Realtime.Raw()
	cacheRefreshPool := databasePools.CacheRefresh.Raw()
	backgroundJobsPool := databasePools.BackgroundJobs.Raw()
	return databaseLanePools{
		management:       managementPool,
		runtimeExecution: runtimeExecutionPool,
		runtimeTelemetry: runtimeTelemetryPool,
		runtimeFeedback:  runtimeFeedbackPool,
		realtime:         realtimePool,
		cacheRefresh:     cacheRefreshPool,
		backgroundJobs:   backgroundJobsPool,
	}
}

type databaseBackgroundServices struct {
	scheduler             *background.Scheduler
	managementSideEffects *managementsideeffects.Dispatcher
	logRetention          *logretention.Store
	managementJobs        *managementjobs.Store
}

type runtimePlanningServices struct {
	cache     *runtimeapi.SharedCache
	state     *loadbalancedomain.LocalRuntimeStateStore
	authCache *managementauth.RuntimeCache
}

type authServices struct {
	emailOutbox *outbox.Store
	management  *managementauth.Service
	runtime     *managementauth.Service
}

type managementServices struct {
	dashboardSnapshots *statsdomain.DashboardAggregateStore
	profiles           *managementprofiles.Service
	vendors            *managementvendors.Service
	models             *managementmodels.Service
	endpoints          *managementendpoints.Service
	connections        *managementconnections.Service
	settings           *managementsettings.Service
	sidecars           *managementsidecars.Service
	loadbalance        *managementloadbalance.Service
	audit              *managementaudit.Service
	stats              *managementstats.Service
	configRules        *managementconfigrules.Service
	configBundle       *managementconfigbundle.Service
}

type realtimeServices struct {
	service            *realtimeapi.Service
	dashboardPublisher *realtimeapi.AsyncDashboardPublisher
	analyticsPublisher *realtimeapi.AsyncAnalyticsPublisher
}

func (resources *productionResources) configureDatabaseBackedServices(ctx context.Context, settings config.Settings, databasePools *platformdb.DatabasePools) error {
	lanes := newDatabaseLanePools(databasePools)
	backgroundServices, err := resources.buildDatabaseBackgroundServices(ctx, lanes)
	if err != nil {
		return err
	}
	planning, err := buildRuntimePlanningServices(ctx, settings, lanes, backgroundServices.scheduler)
	if err != nil {
		return err
	}
	auth, err := resources.buildAuthServices(settings, lanes, backgroundServices, planning)
	if err != nil {
		return err
	}
	management, err := resources.buildManagementServices(settings, lanes, backgroundServices, planning)
	if err != nil {
		return err
	}
	realtime, err := resources.buildRealtimeServices(settings, lanes, backgroundServices.scheduler, auth.management, management.dashboardSnapshots)
	if err != nil {
		return err
	}
	runtimeService, err := resources.buildRuntimeService(settings, lanes, backgroundServices, planning, realtime)
	if err != nil {
		return err
	}
	if err := registerDatabaseBackgroundWorkers(backgroundServices, planning, auth, management, realtime, runtimeService); err != nil {
		return err
	}
	resources.publishDatabaseBackedDependencies(auth, management, realtime, runtimeService, planning)
	return nil
}

func (resources *productionResources) buildDatabaseBackgroundServices(ctx context.Context, lanes databaseLanePools) (databaseBackgroundServices, error) {
	backgroundJobsPool := lanes.backgroundJobs
	backgroundScheduler := background.NewScheduler(background.Config{})
	resources.scheduler = backgroundScheduler
	managementSideEffects := managementsideeffects.NewDispatcher(managementsideeffects.Options{Pool: backgroundJobsPool, Scheduler: backgroundScheduler})
	logRetentionStore := logretention.NewStore(logretention.Options{Pool: backgroundJobsPool})
	managementJobs := managementjobs.NewStore(managementjobs.Options{Pool: backgroundJobsPool, Scheduler: backgroundScheduler, LogRetention: logRetentionStore})
	services := databaseBackgroundServices{
		scheduler:             backgroundScheduler,
		managementSideEffects: managementSideEffects,
		logRetention:          logRetentionStore,
		managementJobs:        managementJobs,
	}
	if err := logRetentionStore.EnsurePartitionHorizon(ctx); err != nil {
		return services, fmt.Errorf("bootstrap log partition horizon: %w", err)
	}
	return services, nil
}

func buildRuntimePlanningServices(ctx context.Context, settings config.Settings, lanes databaseLanePools, scheduler *background.Scheduler) (runtimePlanningServices, error) {
	cacheRefreshPool := lanes.cacheRefresh
	runtimePlanningCache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: cacheRefreshPool, SecretEncryptionKey: settings.SecretEncryptionKey, Scheduler: scheduler})
	runtimeState := loadbalancedomain.NewLocalRuntimeStateStore()
	runtimeAuthCache := managementauth.NewRuntimeCacheFromShared(runtimePlanningCache)
	services := runtimePlanningServices{
		cache:     runtimePlanningCache,
		state:     runtimeState,
		authCache: runtimeAuthCache,
	}
	if err := runtimePlanningCache.Bootstrap(ctx); err != nil {
		return services, err
	}
	return services, nil
}

func (resources *productionResources) buildAuthServices(settings config.Settings, lanes databaseLanePools, backgroundServices databaseBackgroundServices, planning runtimePlanningServices) (authServices, error) {
	managementPool := lanes.management
	runtimeExecutionPool := lanes.runtimeExecution
	backgroundJobsPool := lanes.backgroundJobs
	runtimeAuthCache := planning.authCache
	backgroundScheduler := backgroundServices.scheduler
	emailOutbox := outbox.NewStore(outbox.Options{Pool: backgroundJobsPool, MailerProvider: resources.deps.HotBootstrapConfigRuntime, SecretEncryptionKey: settings.SecretEncryptionKey, Scheduler: backgroundScheduler})
	services := authServices{emailOutbox: emailOutbox}
	managementAuthService, err := managementauth.NewService(settings, managementauth.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, AuthRuntimeConfigProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool, ProxyKeyUsagePool: backgroundJobsPool, EmailOutbox: emailOutbox, Scheduler: backgroundScheduler})
	if err != nil {
		return services, err
	}
	services.management = managementAuthService
	resources.registerSideEffectDrain(closeFuncHook(managementAuthService.DrainSideEffects))
	resources.registerServiceClose(closeFuncHook(managementAuthService.Close))

	runtimeAuthService, err := managementauth.NewService(settings, managementauth.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, AuthRuntimeConfigProvider: resources.deps.HotBootstrapConfigRuntime, Pool: runtimeExecutionPool, RuntimeCache: runtimeAuthCache})
	if err != nil {
		return services, err
	}
	services.runtime = runtimeAuthService
	resources.registerServiceClose(closeFuncHook(runtimeAuthService.Close))
	return services, nil
}

func (resources *productionResources) buildManagementServices(settings config.Settings, lanes databaseLanePools, backgroundServices databaseBackgroundServices, planning runtimePlanningServices) (managementServices, error) {
	managementPool := lanes.management
	managementJobs := backgroundServices.managementJobs
	managementSideEffects := backgroundServices.managementSideEffects
	runtimeState := planning.state
	dashboardSnapshots := statsdomain.NewDashboardAggregateStore()
	services := managementServices{dashboardSnapshots: dashboardSnapshots}
	profileService, err := managementprofiles.NewService(settings, managementprofiles.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool})
	if err != nil {
		return services, err
	}
	services.profiles = profileService
	resources.registerServiceClose(closeFuncHook(profileService.Close))

	vendorService, err := managementvendors.NewService(settings, managementvendors.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool})
	if err != nil {
		return services, err
	}
	services.vendors = vendorService
	resources.registerServiceClose(closeFuncHook(vendorService.Close))
	modelsService, err := managementmodels.NewService(settings, managementmodels.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool})
	if err != nil {
		return services, err
	}
	services.models = modelsService
	resources.registerServiceClose(closeFuncHook(modelsService.Close))

	endpointsService, err := managementendpoints.NewService(settings, managementendpoints.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool})
	if err != nil {
		return services, err
	}
	services.endpoints = endpointsService
	resources.registerServiceClose(closeFuncHook(endpointsService.Close))

	connectionsService, err := managementconnections.NewService(settings, managementconnections.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool})
	if err != nil {
		return services, err
	}
	services.connections = connectionsService
	resources.registerServiceClose(closeFuncHook(connectionsService.Close))

	settingsService, err := managementsettings.NewService(settings, managementsettings.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool, Jobs: managementJobs})
	if err != nil {
		return services, err
	}
	services.settings = settingsService
	resources.registerServiceClose(closeFuncHook(settingsService.Close))

	sidecarsService, err := managementsidecars.NewService(settings, managementsidecars.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool})
	if err != nil {
		return services, err
	}
	services.sidecars = sidecarsService
	resources.registerServiceClose(closeFuncHook(sidecarsService.Close))
	loadbalanceService, err := managementloadbalance.NewService(settings, managementloadbalance.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool, RuntimeState: runtimeState})
	if err != nil {
		return services, err
	}
	services.loadbalance = loadbalanceService
	resources.registerServiceClose(closeFuncHook(loadbalanceService.Close))

	auditService, err := managementaudit.NewService(settings, managementaudit.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool, Jobs: managementJobs})
	if err != nil {
		return services, err
	}
	services.audit = auditService
	resources.registerServiceClose(closeFuncHook(auditService.Close))

	statsService, err := managementstats.NewService(settings, managementstats.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool, DashboardSnapshots: dashboardSnapshots, SideEffects: managementSideEffects})
	if err != nil {
		return services, err
	}
	services.stats = statsService
	resources.registerServiceClose(closeFuncHook(statsService.Close))

	configRulesService, err := managementconfigrules.NewService(settings, managementconfigrules.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool})
	if err != nil {
		return services, err
	}
	services.configRules = configRulesService
	resources.registerServiceClose(closeFuncHook(configRulesService.Close))
	configBundleService, err := managementconfigbundle.NewService(settings, managementconfigbundle.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool})
	if err != nil {
		return services, err
	}
	services.configBundle = configBundleService
	resources.registerServiceClose(closeFuncHook(configBundleService.Close))
	return services, nil
}

func (resources *productionResources) buildRealtimeServices(settings config.Settings, lanes databaseLanePools, scheduler *background.Scheduler, managementAuthService *managementauth.Service, dashboardSnapshots *statsdomain.DashboardAggregateStore) (realtimeServices, error) {
	realtimePool := lanes.realtime
	realtimeService, err := realtimeapi.NewService(settings, realtimeapi.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, RealtimePool: realtimePool, AuthService: managementAuthService, DashboardSnapshots: dashboardSnapshots})
	if err != nil {
		return realtimeServices{}, err
	}
	services := realtimeServices{service: realtimeService}
	resources.realtimeShutdown = append(resources.realtimeShutdown, closeFuncHook(realtimeService.Close))

	asyncDashboardPublisher := realtimeapi.NewAsyncDashboardPublisher(realtimeService, realtimeapi.AsyncDashboardPublisherOptions{Scheduler: scheduler})
	realtimeService.SetAsyncDashboardPublisher(asyncDashboardPublisher)
	services.dashboardPublisher = asyncDashboardPublisher
	resources.registerSideEffectDrain(closeFuncHook(asyncDashboardPublisher.Close))
	resources.registerServiceClose(closeFuncHook(asyncDashboardPublisher.Close))

	asyncAnalyticsPublisher := realtimeapi.NewAsyncAnalyticsPublisher(realtimeService, realtimeapi.AsyncAnalyticsPublisherOptions{Scheduler: scheduler})
	realtimeService.SetAsyncAnalyticsPublisher(asyncAnalyticsPublisher)
	services.analyticsPublisher = asyncAnalyticsPublisher
	resources.registerSideEffectDrain(closeFuncHook(asyncAnalyticsPublisher.Close))
	resources.registerServiceClose(closeFuncHook(asyncAnalyticsPublisher.Close))
	return services, nil
}

func (resources *productionResources) buildRuntimeService(settings config.Settings, lanes databaseLanePools, backgroundServices databaseBackgroundServices, planning runtimePlanningServices, realtime realtimeServices) (*runtimeapi.Service, error) {
	runtimeExecutionPool := lanes.runtimeExecution
	runtimeTelemetryPool := lanes.runtimeTelemetry
	runtimeFeedbackPool := lanes.runtimeFeedback
	backgroundScheduler := backgroundServices.scheduler
	logRetentionStore := backgroundServices.logRetention
	runtimePlanningCache := planning.cache
	runtimeState := planning.state
	asyncDashboardPublisher := realtime.dashboardPublisher
	asyncAnalyticsPublisher := realtime.analyticsPublisher
	runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{ExecutionPool: runtimeExecutionPool, TelemetryPool: runtimeTelemetryPool, FeedbackPool: runtimeFeedbackPool, RuntimeProxyConfigProvider: resources.deps.HotBootstrapConfigRuntime, DashboardUpdates: asyncDashboardPublisher, AnalyticsUpdates: asyncAnalyticsPublisher, Cache: runtimePlanningCache, RuntimeState: runtimeState, LogPartitionEnsurer: logRetentionStore, AssumeLogPartitionHorizon: true, Scheduler: backgroundScheduler, SideEffects: runtimeSideEffectOptions(settings)})
	if err != nil {
		return nil, err
	}
	resources.registerSideEffectDrain(closeFuncHook(runtimeService.DrainSideEffects))
	resources.registerServiceClose(closeFuncHook(runtimeService.Close))
	return runtimeService, nil
}

func registerDatabaseBackgroundWorkers(backgroundServices databaseBackgroundServices, planning runtimePlanningServices, auth authServices, management managementServices, realtime realtimeServices, runtimeService *runtimeapi.Service) error {
	backgroundScheduler := backgroundServices.scheduler
	runtimePlanningCache := planning.cache
	managementAuthService := auth.management
	emailOutbox := auth.emailOutbox
	managementJobs := backgroundServices.managementJobs
	managementSideEffects := backgroundServices.managementSideEffects
	logRetentionStore := backgroundServices.logRetention
	sidecarsService := management.sidecars
	asyncDashboardPublisher := realtime.dashboardPublisher
	asyncAnalyticsPublisher := realtime.analyticsPublisher
	for _, register := range []func(*background.Scheduler) error{
		runtimePlanningCache.RegisterBackgroundWorker,
		managementAuthService.RegisterBackgroundWorkers,
		emailOutbox.RegisterBackgroundWorker,
		managementJobs.RegisterBackgroundWorker,
		managementSideEffects.RegisterBackgroundWorker,
		logRetentionStore.RegisterBackgroundWorker,
		sidecarsService.RegisterBackgroundWorker,
		asyncDashboardPublisher.RegisterBackgroundWorker,
		asyncAnalyticsPublisher.RegisterBackgroundWorker,
		runtimeService.RegisterBackgroundWorkers,
	} {
		if err := register(backgroundScheduler); err != nil {
			return err
		}
	}
	return nil
}

func (resources *productionResources) publishDatabaseBackedDependencies(auth authServices, management managementServices, realtime realtimeServices, runtimeService *runtimeapi.Service, planning runtimePlanningServices) {
	resources.deps.AuditService = management.audit
	resources.deps.AuthService = auth.management
	resources.deps.RuntimeAuthService = auth.runtime
	resources.deps.ConfigBundleService = management.configBundle
	resources.deps.ConfigRulesService = management.configRules
	resources.deps.ConnectionsService = management.connections
	resources.deps.EndpointsService = management.endpoints
	resources.deps.LoadbalanceService = management.loadbalance
	resources.deps.ModelsService = management.models
	resources.deps.ProfilesService = management.profiles
	resources.deps.RealtimeService = realtime.service
	resources.deps.RuntimeService = runtimeService
	resources.deps.RuntimeCache = planning.cache
	resources.deps.RuntimeState = planning.state
	resources.deps.SettingsService = management.settings
	resources.deps.SidecarsService = management.sidecars
	resources.deps.StatsService = management.stats
	resources.deps.VendorsService = management.vendors
}

func runtimeSideEffectOptions(settings config.Settings) runtimeapi.RuntimeSideEffectOptions {
	return runtimeapi.RuntimeSideEffectOptions{AttemptTimeout: settings.RuntimeSideEffects().AttemptTimeout}
}

func (resources *productionResources) registerSideEffectDrain(hook ShutdownHook) {
	if hook == nil {
		return
	}
	resources.sideEffectDrain = append(resources.sideEffectDrain, hook)
}

func (resources *productionResources) registerServiceClose(hook ShutdownHook) {
	if hook == nil {
		return
	}
	resources.serviceClose = append([]ShutdownHook{hook}, resources.serviceClose...)
}

func (resources *productionResources) schedulerStopHook() ShutdownHook {
	if resources == nil || resources.scheduler == nil {
		return nil
	}
	return func(ctx context.Context) error {
		deadline := time.Now().Add(5 * time.Second)
		if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
			deadline = ctxDeadline
		}
		return resources.scheduler.Stop(ctx, deadline)
	}
}

func (resources *productionResources) cleanup(ctx context.Context) error {
	if resources == nil {
		return nil
	}
	var errs []error
	for _, hook := range resources.realtimeShutdown {
		errs = appendError(errs, hook(ctx))
	}
	for _, hook := range resources.sideEffectDrain {
		errs = appendError(errs, hook(ctx))
	}
	if schedulerStop := resources.schedulerStopHook(); schedulerStop != nil {
		errs = appendError(errs, schedulerStop(ctx))
	}
	for _, hook := range resources.serviceClose {
		errs = appendError(errs, hook(ctx))
	}
	if resources.dbClose != nil {
		errs = appendError(errs, resources.dbClose(ctx))
	}
	return errors.Join(errs...)
}

func closeFuncHook(closeFn func()) ShutdownHook {
	return func(context.Context) error {
		if closeFn != nil {
			closeFn()
		}
		return nil
	}
}
func (resources *productionResources) cleanupForSetupFailure(ctx context.Context) error {
	cleanupCtx := context.WithoutCancel(ctx)
	if _, ok := cleanupCtx.Deadline(); ok {
		return resources.cleanup(cleanupCtx)
	}
	cleanupCtx, cancel := context.WithTimeout(cleanupCtx, 5*time.Second)
	defer cancel()
	return resources.cleanup(cleanupCtx)
}

package platformhttp

import (
	"fmt"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementconfigrules "github.com/coachpo/prism/backend/internal/httpapi/management/configrules"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	managementprofiles "github.com/coachpo/prism/backend/internal/httpapi/management/profiles"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	realtimeapi "github.com/coachpo/prism/backend/internal/httpapi/realtime"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	platformdb "github.com/coachpo/prism/backend/internal/platform/db"
	"github.com/coachpo/prism/backend/internal/platform/version"
)

type Dependencies struct {
	Version                   string
	HotBootstrapConfigRuntime *HotBootstrapConfigRuntime
	CORSOriginProvider        platformcors.OriginProvider
	AuditService              *managementaudit.Service
	AuthService               *managementauth.Service
	RuntimeAuthService        *managementauth.Service
	ConfigRulesService        *managementconfigrules.Service
	ConnectionsService        *managementconnections.Service
	EndpointsService          *managementendpoints.Service
	LoadbalanceService        *managementloadbalance.Service
	ModelsService             *managementmodels.Service
	ProfilesService           *managementprofiles.Service
	RealtimeService           *realtimeapi.Service
	RuntimeService            *runtimeapi.Service
	RuntimeCache              *runtimeapi.SharedCache
	RuntimeState              *loadbalancedomain.LocalRuntimeStateStore
	DatabasePools             *platformdb.DatabasePools
	SettingsService           *managementsettings.Service
	StatsService              *managementstats.Service
}

type ServerOptions struct {
	Dependencies Dependencies
}

func completeDependencies(settings config.Settings, options ServerOptions) (Dependencies, error) {
	deps := options.Dependencies
	var err error
	if deps.Version == "" {
		deps.Version, err = version.Load()
		if err != nil {
			return Dependencies{}, err
		}
	}
	if deps.HotBootstrapConfigRuntime == nil {
		deps.HotBootstrapConfigRuntime, err = NewHotBootstrapConfigRuntime(settings)
		if err != nil {
			return Dependencies{}, err
		}
	}
	if deps.CORSOriginProvider == nil && deps.HotBootstrapConfigRuntime != nil {
		deps.CORSOriginProvider = deps.HotBootstrapConfigRuntime
	}
	return deps, nil
}

func completeHandlerDependencies(settings config.Settings, deps Dependencies) (Dependencies, error) {
	if deps.Version == "" {
		return Dependencies{}, fmt.Errorf("version is required")
	}
	if deps.RuntimeState == nil && deps.RuntimeService != nil {
		deps.RuntimeState = deps.RuntimeService.RuntimeState()
	}
	if deps.CORSOriginProvider == nil && deps.HotBootstrapConfigRuntime != nil {
		deps.CORSOriginProvider = deps.HotBootstrapConfigRuntime
	}
	if deps.CORSOriginProvider == nil {
		deps.CORSOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}
	return deps, nil
}

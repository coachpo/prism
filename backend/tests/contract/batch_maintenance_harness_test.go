package contracttest

import (
	"context"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
)

func newBatchContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	return newContractHarnessFor(t, "batch_contract", contractHarnessOptions{
		SecretEncryptionKey: "batch-contract-secret", Version: "batch-contract-test",
		DependenciesBuilder: func(t *testing.T, ctx context.Context, h *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies {
			cache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
			if err := cache.Bootstrap(ctx); err != nil {
				t.Fatal(err)
			}
			endpoints, err := managementendpoints.NewService(settings, managementendpoints.Options{Pool: pool})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(endpoints.Close)
			targets, err := managementconnections.NewService(settings, managementconnections.Options{Pool: pool})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(targets.Close)
			models, err := managementmodels.NewService(settings, managementmodels.Options{Pool: pool})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(models.Close)
			models.SetTerminalTargetCreator(targets)
			h.runtimeCache = cache
			return platformhttp.Dependencies{EndpointsService: endpoints, ConnectionsService: targets, ModelsService: models, RuntimeCache: cache}
		},
	})
}

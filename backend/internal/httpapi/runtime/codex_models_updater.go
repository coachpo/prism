package runtime

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	codexCatalogRefreshInterval = 24 * time.Hour
	codexCatalogMaxBodyBytes    = 1 << 20
)

var (
	codexCatalogSources = []string{
		"https://raw.githubusercontent.com/openai/codex/main/codex-rs/models-manager/models.json",
		"https://cdn.jsdelivr.net/gh/openai/codex@main/codex-rs/models-manager/models.json",
	}
	codexCatalogHTTPClient  = &http.Client{Timeout: 30 * time.Second}
	codexCatalogUpdaterOnce sync.Once
)

// StartCodexCatalogUpdater refreshes the Codex client model catalog from the
// openai/codex repository at startup and then every 24h. Failures are logged
// and the current (initially embedded) catalog stays in effect.
func StartCodexCatalogUpdater(ctx context.Context) {
	codexCatalogUpdaterOnce.Do(func() {
		go func() {
			refreshCodexCatalog(ctx, codexCatalogHTTPClient)
			ticker := time.NewTicker(codexCatalogRefreshInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					refreshCodexCatalog(ctx, codexCatalogHTTPClient)
				}
			}
		}()
	})
}

func refreshCodexCatalog(ctx context.Context, client *http.Client) {
	failures := make([]error, 0, len(codexCatalogSources))
	for _, source := range codexCatalogSources {
		body, err := fetchCodexCatalog(ctx, client, source)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			failures = append(failures, fmt.Errorf("%s: %w", source, err))
			continue
		}
		templates, err := parseCodexCatalog(body)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", source, err))
			slog.Warn("Codex catalog refresh source invalid", "source", source, "error", err)
			continue
		}

		hash := sha256.Sum256(body)
		codexModelTemplatesMu.Lock()
		changed := hash != codexModelTemplatesHash
		codexModelTemplatesStore = templates
		codexModelTemplatesHash = hash
		codexModelTemplatesMu.Unlock()
		slog.Info("Codex catalog refreshed", "source", source, "changed", changed)
		return
	}
	if ctx.Err() == nil {
		slog.Warn("Codex catalog refresh failed; retaining current catalog", "error", errors.Join(failures...))
	}
}

func fetchCodexCatalog(ctx context.Context, client *http.Client, source string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, codexCatalogMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > codexCatalogMaxBodyBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", codexCatalogMaxBodyBytes)
	}
	return body, nil
}

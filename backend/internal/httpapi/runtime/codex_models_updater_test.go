package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseCodexCatalogValidatesRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{name: "valid", payload: `{"models":[{"slug":"gpt-5.5","priority":1},{"slug":"future","priority":2}]}`},
		{name: "empty slug", payload: `{"models":[{"slug":" ","priority":1},{"slug":"gpt-5.5","priority":2}]}`, wantErr: true},
		{name: "non-string slug", payload: `{"models":[{"slug":55,"priority":1},{"slug":"gpt-5.5","priority":2}]}`, wantErr: true},
		{name: "duplicate slug", payload: `{"models":[{"slug":"gpt-5.5","priority":1},{"slug":"gpt-5.5","priority":2}]}`, wantErr: true},
		{name: "missing priority", payload: `{"models":[{"slug":"gpt-5.5"}]}`, wantErr: true},
		{name: "fractional priority", payload: `{"models":[{"slug":"gpt-5.5","priority":1.5}]}`, wantErr: true},
		{name: "missing fallback", payload: `{"models":[{"slug":"future","priority":1}]}`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			templates, err := parseCodexCatalog([]byte(test.payload))
			if (err != nil) != test.wantErr {
				t.Fatalf("parseCodexCatalog() error = %v, wantErr %t", err, test.wantErr)
			}
			if !test.wantErr && (len(templates.bySlug) != 2 || templates.maxPriority != 2) {
				t.Fatalf("parseCodexCatalog() = %+v, want two templates with maximum priority 2", templates)
			}
		})
	}
}

func TestRefreshCodexCatalogSwapsOnlyValidatedSources(t *testing.T) {
	preserveCodexCatalogTestState(t)

	var failureHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/valid":
			_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-5.5","priority":7},{"slug":"future","priority":11}]}`))
		case "/missing-fallback":
			_, _ = w.Write([]byte(`{"models":[{"slug":"future","priority":1}]}`))
		case "/duplicate":
			_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-5.5","priority":1},{"slug":"gpt-5.5","priority":2}]}`))
		default:
			failureHits.Add(1)
			http.Error(w, "unavailable", http.StatusBadGateway)
		}
	}))
	defer server.Close()

	codexCatalogSources = []string{server.URL + "/missing-fallback", server.URL + "/valid"}
	refreshCodexCatalog(context.Background(), server.Client())
	templates, maxPriority := loadCodexTemplates()
	if _, ok := templates["future"]; !ok || maxPriority != 11 {
		t.Fatalf("valid fallback source did not replace catalog: slugs=%v maxPriority=%d", reflect.ValueOf(templates).MapKeys(), maxPriority)
	}
	validTemplates := templates
	validMaxPriority := maxPriority
	validHash := sha256.Sum256([]byte(`{"models":[{"slug":"gpt-5.5","priority":7},{"slug":"future","priority":11}]}`))
	codexModelTemplatesMu.RLock()
	gotHash := codexModelTemplatesHash
	codexModelTemplatesMu.RUnlock()
	if gotHash != validHash {
		t.Fatalf("valid refresh hash = %x, want %x", gotHash, validHash)
	}

	for _, path := range []string{"/missing-fallback", "/duplicate"} {
		codexCatalogSources = []string{server.URL + path}
		refreshCodexCatalog(context.Background(), server.Client())
		gotTemplates, gotMaxPriority := loadCodexTemplates()
		if !reflect.DeepEqual(gotTemplates, validTemplates) || gotMaxPriority != validMaxPriority {
			t.Fatalf("invalid refresh %q replaced last good catalog", path)
		}
	}

	codexCatalogSources = []string{server.URL + "/fail-one", server.URL + "/fail-two"}
	refreshCodexCatalog(context.Background(), server.Client())
	gotTemplates, gotMaxPriority := loadCodexTemplates()
	if !reflect.DeepEqual(gotTemplates, validTemplates) || gotMaxPriority != validMaxPriority {
		t.Fatal("all-URL failure replaced last good catalog")
	}
	if got := failureHits.Load(); got != 2 {
		t.Fatalf("all-URL failure attempted %d sources, want 2", got)
	}
}

func TestRefreshCodexCatalogFetchFailuresWarnOnlyWhenCycleFails(t *testing.T) {
	preserveCodexCatalogTestState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/valid" {
			_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-5.5","priority":1}]}`))
			return
		}
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer server.Close()
	logs := captureCodexCatalogUpdaterLogs(t)

	codexCatalogSources = []string{server.URL + "/fail-first", server.URL + "/valid"}
	refreshCodexCatalog(context.Background(), server.Client())
	if got := strings.Count(logs.String(), "level=WARN"); got != 0 {
		t.Fatalf("successful fallback cycle logged %d warnings, want 0:\n%s", got, logs.String())
	}

	logs.Reset()
	codexCatalogSources = []string{server.URL + "/fail-one", server.URL + "/fail-two"}
	refreshCodexCatalog(context.Background(), server.Client())
	logText := logs.String()
	if got := strings.Count(logText, "level=WARN"); got != 1 {
		t.Fatalf("failed cycle logged %d warnings, want 1:\n%s", got, logText)
	}
	for _, source := range codexCatalogSources {
		if !strings.Contains(logText, source) {
			t.Errorf("failed cycle warning omitted source %q:\n%s", source, logText)
		}
	}
}

func TestRefreshCodexCatalogRejectsOversizedSource(t *testing.T) {
	preserveCodexCatalogTestState(t)
	beforeTemplates, beforeMaxPriority := loadCodexTemplates()
	codexModelTemplatesMu.RLock()
	beforeHash := codexModelTemplatesHash
	codexModelTemplatesMu.RUnlock()
	payload := []byte(`{"models":[{"slug":"gpt-5.5","priority":1,"description":"` + strings.Repeat("x", 2<<20) + `"}]}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	codexCatalogSources = []string{server.URL}
	refreshCodexCatalog(context.Background(), server.Client())
	afterTemplates, afterMaxPriority := loadCodexTemplates()
	codexModelTemplatesMu.RLock()
	afterHash := codexModelTemplatesHash
	codexModelTemplatesMu.RUnlock()
	if !reflect.DeepEqual(afterTemplates, beforeTemplates) || afterMaxPriority != beforeMaxPriority || afterHash != beforeHash {
		t.Fatal("oversized source replaced last good catalog")
	}
}

func preserveCodexCatalogTestState(t *testing.T) {
	t.Helper()
	originalSources := append([]string(nil), codexCatalogSources...)
	codexModelTemplatesMu.RLock()
	originalTemplates := codexModelTemplatesStore
	originalHash := codexModelTemplatesHash
	codexModelTemplatesMu.RUnlock()
	t.Cleanup(func() {
		codexCatalogSources = originalSources
		codexModelTemplatesMu.Lock()
		codexModelTemplatesStore = originalTemplates
		codexModelTemplatesHash = originalHash
		codexModelTemplatesMu.Unlock()
	})
}

func captureCodexCatalogUpdaterLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	originalLogger := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })
	return &output
}

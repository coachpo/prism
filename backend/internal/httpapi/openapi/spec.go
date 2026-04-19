package openapi

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Document struct {
	raw   []byte
	paths map[string]json.RawMessage
}

type decodedDocument struct {
	Paths map[string]json.RawMessage `json:"paths"`
}

var docsTemplate = template.Must(template.New("docs").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ .Title }}</title>
  <style>
    :root {
      color-scheme: light dark;
      font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    body {
      margin: 0;
      padding: 2rem;
      background: #0f172a;
      color: #e2e8f0;
    }
    main {
      max-width: 72rem;
      margin: 0 auto;
    }
    a {
      color: #7dd3fc;
    }
    .lede {
      color: #cbd5e1;
      margin-bottom: 1rem;
    }
    pre {
      white-space: pre-wrap;
      word-break: break-word;
      background: rgba(15, 23, 42, 0.92);
      border: 1px solid rgba(148, 163, 184, 0.35);
      border-radius: 0.75rem;
      padding: 1rem;
      overflow: auto;
      min-height: 12rem;
    }
  </style>
</head>
<body>
  <main>
    <h1>{{ .Heading }}</h1>
    <p class="lede">{{ .Description }}</p>
    <p><a href="/openapi.json">OpenAPI JSON</a></p>
    <pre id="openapi-viewer">Loading /openapi.json...</pre>
  </main>
  <script>
    (async function loadSpec() {
      const target = document.getElementById('openapi-viewer');
      try {
        const response = await fetch('/openapi.json', { headers: { 'Accept': 'application/json' } });
        if (!response.ok) {
          throw new Error('unexpected status ' + response.status);
        }

        const payload = await response.json();
        target.textContent = JSON.stringify(payload, null, 2);
      } catch (error) {
        target.textContent = 'Failed to load OpenAPI document: ' + error;
      }
    })();
  </script>
</body>
</html>`))

func Load() (*Document, error) {
	return LoadFromPath(DefaultPath())
}

func LoadFromPath(path string) (*Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read checked-in OpenAPI document: %w", err)
	}

	var decoded decodedDocument
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode checked-in OpenAPI document: %w", err)
	}

	document := &Document{
		raw:   raw,
		paths: decoded.Paths,
	}

	if err := document.ValidateManagementOnly(); err != nil {
		return nil, err
	}

	if !document.HasPath("/health") {
		return nil, fmt.Errorf("checked-in OpenAPI document is missing /health")
	}

	return document, nil
}

func DefaultPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("docs", "openapi.json")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "docs", "openapi.json"))
}

func (d *Document) HasPath(path string) bool {
	_, ok := d.paths[path]
	return ok
}

func (d *Document) ValidateManagementOnly() error {
	for path := range d.paths {
		if strings.HasPrefix(path, "/v1") || strings.HasPrefix(path, "/v1beta") {
			return fmt.Errorf("checked-in OpenAPI document must stay management-only, found runtime path %q", path)
		}
	}

	return nil
}

func (d *Document) ServeJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(d.raw)
}

func (d *Document) ServeDocs(w http.ResponseWriter, _ *http.Request) {
	d.serveHTML(
		w,
		htmlViewData{
			Title:       "Prism API Docs",
			Heading:     "Prism API Docs",
			Description: "Management and health contract served from the checked-in OpenAPI artifact.",
		},
	)
}

func (d *Document) ServeRedoc(w http.ResponseWriter, _ *http.Request) {
	d.serveHTML(
		w,
		htmlViewData{
			Title:       "Prism ReDoc",
			Heading:     "Prism ReDoc",
			Description: "Minimal ReDoc-style shell backed by the checked-in OpenAPI artifact.",
		},
	)
}

type htmlViewData struct {
	Title       string
	Heading     string
	Description string
}

func (d *Document) serveHTML(w http.ResponseWriter, data htmlViewData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = docsTemplate.Execute(w, data)
}

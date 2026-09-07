package runtimetest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type opencodeClient struct {
	binary string
	root   string
	env    []string
	t      *testing.T
	logs   *bytes.Buffer
}

func newOpenCodeClient(t *testing.T, binary, config, key string, logs *bytes.Buffer) *opencodeClient {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"home", "config", "data", "cache", "state", "work", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "models.json"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// A deny proxy catches incidental internet requests; only loopback servers
	// are exempt. The empty model catalog also removes built-in free providers.
	var deniedMu sync.Mutex
	var denied []string
	deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deniedMu.Lock()
		denied = append(denied, r.Method+" "+r.URL.String())
		deniedMu.Unlock()
		http.Error(w, "external network disabled by acceptance fixture", http.StatusForbidden)
	}))
	t.Cleanup(deny.Close)
	t.Cleanup(func() {
		deniedMu.Lock()
		defer deniedMu.Unlock()
		if len(denied) > 0 {
			t.Errorf("client attempted external network: %v", denied)
		}
	})
	overlay, _ := json.Marshal(map[string]any{"enabled_providers": []string{"prism-acceptance"}, "plugin": []any{}, "share": "disabled", "permission": map[string]string{"*": "deny", "read": "allow"}, "agent": map[string]any{"title": map[string]bool{"disable": true}, "summary": map[string]bool{"disable": true}, "build": map[string]int{"steps": 3}}})
	rg, err := exec.LookPath("rg")
	if err != nil {
		t.Fatal("real client requires existing rg executable: ", err)
	}
	client := &opencodeClient{binary: binary, root: root, t: t, logs: logs, env: []string{
		"PATH=" + filepath.Dir(rg) + ":/usr/local/bin:/usr/bin:/bin", "HOME=" + filepath.Join(root, "home"),
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "XDG_DATA_HOME=" + filepath.Join(root, "data"), "XDG_CACHE_HOME=" + filepath.Join(root, "cache"), "XDG_STATE_HOME=" + filepath.Join(root, "state"), "TMPDIR=" + filepath.Join(root, "tmp"),
		"OPENCODE_CONFIG=" + config, "OPENCODE_CONFIG_CONTENT=" + string(overlay), "OPENCODE_MODELS_PATH=" + filepath.Join(root, "models.json"),
		"OPENCODE_DISABLE_MODELS_FETCH=true", "OPENCODE_DISABLE_AUTOUPDATE=true", "OPENCODE_DISABLE_PROJECT_CONFIG=true", "OPENCODE_EXPERIMENTAL_DISABLE_FILEWATCHER=true", "OPENCODE_DISABLE_FFF=true", "OPENCODE_DISABLE_AUTOCOMPACT=true",
		"HTTP_PROXY=" + deny.URL, "HTTPS_PROXY=" + deny.URL, "ALL_PROXY=" + deny.URL, "NO_PROXY=localhost,127.0.0.1,::1", "DO_NOT_TRACK=1", "CI=1", "TERM=dumb",
	}}
	if key != "" {
		client.env = append(client.env, "PRISM_API_KEY="+key)
	}
	return client
}

func (c *opencodeClient) run(args ...string) string {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binary, args...)
	cmd.Env = c.env
	cmd.Dir = filepath.Join(c.root, "work")
	out, err := cmd.CombinedOutput()
	fmt.Fprintf(c.logs, "$ opencode %s\n%s\nexit=%v\n", strings.Join(args, " "), out, err)
	if err != nil {
		summary := out
		if len(summary) > 12000 {
			summary = summary[len(summary)-12000:]
		}
		c.t.Fatalf("OpenCode %v: %v\n%s", args, err, summary)
	}
	return string(out)
}

// parseOpenCodeModels consumes the real CLI's verbose registry output, whose
// records are preceded by provider/model lines rather than a JSON container.
func parseOpenCodeModels(t *testing.T, output string) []map[string]any {
	t.Helper()
	models := []map[string]any{}
	for {
		start := strings.Index(output, "\n{")
		if start < 0 {
			break
		}
		decoder := json.NewDecoder(strings.NewReader(output[start+1:]))
		var model map[string]any
		if err := decoder.Decode(&model); err != nil {
			t.Fatalf("decode real model registry: %v: %s", err, output)
		}
		models = append(models, model)
		output = output[start+1+int(decoder.InputOffset()):]
	}
	return models
}

func opencodeEvidence(t *testing.T, name string, body []byte) {
	t.Helper()
	dir := os.Getenv("PRISM_OPENCODE_EVIDENCE_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
		t.Fatal(err)
	}
}

func opencodeEvidenceJSON(t *testing.T, name string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	opencodeEvidence(t, name, append(body, '\n'))
}

func recordOpenCodeIngress(next http.Handler, mu *sync.Mutex, observations *[]opencodeExchange) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1") {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			mu.Lock()
			*observations = append(*observations, opencodeExchange{Path: r.URL.RequestURI(), Authorization: r.Header.Get("Authorization"), APIKey: r.Header.Get("X-Api-Key"), GoogleKey: r.Header.Get("X-Goog-Api-Key"), Body: body, ToolResult: strings.Contains(string(body), opencodeToolMarker)})
			mu.Unlock()
		}
		next.ServeHTTP(w, r)
	})
}

// Assert observable CLI events separately from registration and HTTP traces.
func assertOpenCodeClientResult(t *testing.T, output string) {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	before, tool, final, finished := false, false, false, false
	for scanner.Scan() {
		var event struct {
			Type string `json:"type"`
			Part struct {
				Text   string `json:"text"`
				Tool   string `json:"tool"`
				Reason string `json:"reason"`
				State  struct {
					Status string `json:"status"`
					Output string `json:"output"`
				} `json:"state"`
			} `json:"part"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		switch event.Type {
		case "error":
			t.Fatalf("real client returned error event: %s", scanner.Text())
		case "text":
			before = before || event.Part.Text == "before-tool"
			final = final || event.Part.Text == opencodeFinalMarker
		case "tool_use":
			if event.Part.Tool == "read" && event.Part.State.Status == "completed" && strings.Contains(event.Part.State.Output, opencodeToolMarker) {
				tool = true
			}
		case "step_finish":
			finished = finished || event.Part.Reason == "stop"
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !before || !tool || !final || !finished {
		t.Fatalf("real CLI result missing before=%v tool=%v final=%v stop=%v", before, tool, final, finished)
	}
}

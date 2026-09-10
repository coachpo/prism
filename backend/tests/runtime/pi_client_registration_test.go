package runtimetest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
)

// This boundary loads a management-generated file with the actual pinned Pi
// CLI and its own model runtime. No vendor request is needed for registration.
func TestPiClientRegistration(t *testing.T) {
	packageDir := os.Getenv("PRISM_PI_PACKAGE_DIR")
	if packageDir == "" {
		t.Skip("set PRISM_PI_PACKAGE_DIR to actual @earendil-works/pi-coding-agent 0.84.3")
	}
	if !filepath.IsAbs(packageDir) {
		t.Fatal("PRISM_PI_PACKAGE_DIR must be absolute")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	h := newRuntimeHarness(t)
	h.enableRuntimeProxyAPIKeyAuth(t)
	loginRuntimeHarness(t, h, "runtime-proxy-user", "runtime-proxy-password-123")
	cases := []opencodeProtocolCase{
		{Name: "chat", Family: "openai", Mode: "chat_completions_only", ModelID: "fixture/chat"},
		{Name: "responses", Family: "openai", Mode: "responses_only", ModelID: "fixture/responses"},
		{Name: "dual", Family: "openai", Mode: "dual_native", ModelID: "fixture/dual"},
		{Name: "anthropic", Family: "anthropic", ModelID: "fixture-anthropic"},
		{Name: "gemini", Family: "gemini", ModelID: "fixture-gemini"},
	}
	ids := []int{}
	selections := map[string]any{}
	for i := range cases {
		tc := &cases[i]
		tc.ModelConfigID = seedOpenCodeProtocol(t, h, 1, "http://127.0.0.1:1", *tc)
		var mode *string
		if tc.Mode != "" {
			mode = &tc.Mode
		}
		api := modelexport.PiAPIForModel(tc.Family, mode)
		_, err := h.conn.Exec(context.Background(), `INSERT INTO model_pi_catalog_bindings (
			model_config_id, provider_id, catalog_model_id, api, prism_model_id_at_bind, bind_source, catalog_revision, fetched_at,
			source_name, source_reasoning, source_input, source_context_window, source_max_tokens, source_dropped_fields, updated_at
		) VALUES ($1, 'controlled-pi', $2, $3, $2, 'manual', 'controlled-pi-0.84.3', NOW(),
			'Controlled Pi model', FALSE, '["text"]'::jsonb, 32768, 2048, '[]'::jsonb, NOW())`, tc.ModelConfigID, tc.ModelID, api)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, tc.ModelConfigID)
		selections[fmt.Sprint(tc.ModelConfigID)] = map[string]any{"provider_id": "controlled-pi", "model_id": tc.ModelID, "api": api}
	}
	sourceResponse := h.requestJSON(t, http.MethodGet, "/api/models/exports/pi/source", nil, nil)
	assertStatus(t, sourceResponse, http.StatusOK)
	var source map[string]any
	decodeJSONResponse(t, sourceResponse, &source)
	logs := &bytes.Buffer{}
	t.Cleanup(func() { piClientEvidence(t, "client-run.log", logs.Bytes()) })
	for _, includeKey := range []bool{false, true} {
		mode := "without-credential"
		if includeKey {
			mode = "manual-credential"
		}
		render := h.requestJSON(t, http.MethodPost, "/api/models/exports/pi/render", map[string]any{
			"expected_source_digest": source["source_digest"], "model_config_ids": ids, "selections": selections,
			"base_url": "http://127.0.0.1:1", "provider_id": "prism-acceptance",
			"credential": map[string]any{"include": includeKey, "api_key": map[bool]string{true: "controlled-pi-key", false: ""}[includeKey]},
		}, nil)
		assertStatus(t, render, http.StatusOK)
		var rendered struct {
			Content string `json:"content"`
		}
		decodeJSONResponse(t, render, &rendered)
		root := t.TempDir()
		config := filepath.Join(root, "models.json")
		if err := os.WriteFile(config, []byte(rendered.Content), 0600); err != nil {
			t.Fatal(err)
		}
		piClientEvidence(t, "generated-"+mode+".json", []byte(rendered.Content))
		guard := filepath.Join(root, "deny-network.mjs")
		if err := os.WriteFile(guard, []byte(piNetworkGuardScript), 0600); err != nil {
			t.Fatal(err)
		}
		run := func(args ...string) string {
			t.Helper()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			args = append([]string{"--import", guard}, args...)
			cmd := exec.CommandContext(ctx, node, args...)
			cmd.Dir = root
			cmd.Env = []string{"PATH=" + filepath.Dir(node) + ":/usr/bin:/bin", "HOME=" + root, "PI_CODING_AGENT_DIR=" + root, "PI_OFFLINE=1", "DO_NOT_TRACK=1", "TERM=dumb"}
			out, err := cmd.CombinedOutput()
			fmt.Fprintf(logs, "$ node %s\n%s\nexit=%v\n", strings.Join(args, " "), out, err)
			if err != nil {
				t.Fatalf("actual Pi failed: %v\n%s", err, out)
			}
			return string(out)
		}
		cli := filepath.Join(packageDir, "dist/bundle/cli.js")
		if version := strings.TrimSpace(run(cli, "--version")); version != "0.84.3" {
			t.Fatalf("Pi version %q, want 0.84.3", version)
		}
		if includeKey {
			listing := run(cli, "--offline", "--list-models", "prism-acceptance")
			for _, tc := range cases {
				if !strings.Contains(listing, tc.ModelID) {
					t.Fatalf("Pi CLI did not list %s: %s", tc.ModelID, listing)
				}
			}
		}
		script := filepath.Join(root, "register.mjs")
		if err := os.WriteFile(script, []byte(piRegistrationScript), 0600); err != nil {
			t.Fatal(err)
		}
		out := run(script, packageDir, config)
		var result struct {
			Models []struct {
				ID            string `json:"id"`
				API           string `json:"api"`
				ContextWindow int    `json:"contextWindow"`
				MaxTokens     int    `json:"maxTokens"`
			} `json:"models"`
			NetworkCalls   int `json:"networkCalls"`
			AvailableCount int `json:"availableCount"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		if result.NetworkCalls != 0 || len(result.Models) != len(cases) {
			t.Fatalf("Pi registration boundary failed: %s", out)
		}
		if result.AvailableCount != map[bool]int{false: 0, true: len(cases)}[includeKey] {
			t.Fatalf("Pi configured credential availability mismatch: %s", out)
		}
		for _, tc := range cases {
			found := false
			for _, model := range result.Models {
				if model.ID == tc.ModelID {
					found = true
					if model.API != selections[fmt.Sprint(tc.ModelConfigID)].(map[string]any)["api"] || model.ContextWindow != 32768 || model.MaxTokens != 2048 {
						t.Fatalf("Pi changed native API/limits: %+v", model)
					}
				}
			}
			if !found {
				t.Fatalf("Pi missing model %s", tc.ModelID)
			}
		}
		piClientEvidence(t, "registered-"+mode+".json", []byte(out))
	}
}

func piClientEvidence(t *testing.T, name string, body []byte) {
	t.Helper()
	dir := os.Getenv("PRISM_PI_EVIDENCE_DIR")
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

const piNetworkGuardScript = `
import net from 'node:net';
import tls from 'node:tls';
globalThis.piAcceptanceNetworkCalls = 0;
const deny = () => { globalThis.piAcceptanceNetworkCalls++; throw new Error('network forbidden in Pi registration acceptance'); };
globalThis.fetch = deny;
net.connect = deny;
net.createConnection = deny;
net.Socket.prototype.connect = deny;
tls.connect = deny;
process.on('exit', () => { if (globalThis.piAcceptanceNetworkCalls) process.exitCode = 1; });
`

const piRegistrationScript = `
import { pathToFileURL } from 'node:url';
import { join, dirname } from 'node:path';
const { ModelRuntime } = await import(pathToFileURL(join(process.argv[2], 'dist/core/model-runtime.js')));
const runtime = await ModelRuntime.create({ modelsPath: process.argv[3], authPath: join(dirname(process.argv[3]), 'auth.json'), allowModelNetwork: false });
if (runtime.getError()) throw new Error(runtime.getError());
const models = runtime.getModels().filter(model => model.provider === 'prism-acceptance');
const availableCount = runtime.getAvailableSnapshot().filter(model => model.provider === 'prism-acceptance').length;
if (globalThis.piAcceptanceNetworkCalls) throw new Error('unexpected network attempt');
console.log(JSON.stringify({ piVersion: '0.84.3', networkCalls: globalThis.piAcceptanceNetworkCalls, availableCount, models }, null, 2));
`

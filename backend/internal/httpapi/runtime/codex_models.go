package runtime

// Codex client model catalog, served when /v1/models carries the
// client_version query parameter.
//
// Template source: https://github.com/openai/codex
//   codex-rs/models-manager/models.json @ 3380969a29134630d56feb6218e8e8dcc5e8196d
//   fetched 2026-07-10.
//
import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/coachpo/prism/backend/internal/providerauth"
)

//go:embed codex_client_models.json
var codexClientModelsJSON []byte

type codexModelsCatalogResponse struct {
	Models []map[string]any `json:"models"`
}

type codexModelTemplates struct {
	bySlug      map[string]map[string]any
	maxPriority int
}

var (
	codexModelTemplatesMu    sync.RWMutex
	codexModelTemplatesStore codexModelTemplates
	codexModelTemplatesHash  [sha256.Size]byte
)

func init() {
	templates, err := parseCodexCatalog(codexClientModelsJSON)
	if err != nil {
		panic(fmt.Sprintf("parse embedded Codex models catalog: %v", err))
	}
	codexModelTemplatesStore = templates
	codexModelTemplatesHash = sha256.Sum256(codexClientModelsJSON)
}

func parseCodexCatalog(data []byte) (codexModelTemplates, error) {
	var payload codexModelsCatalogResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return codexModelTemplates{}, fmt.Errorf("decode Codex models catalog: %w", err)
	}

	templates := codexModelTemplates{
		bySlug: make(map[string]map[string]any, len(payload.Models)),
	}
	for _, model := range payload.Models {
		slug, ok := model["slug"].(string)
		if !ok || strings.TrimSpace(slug) == "" {
			return codexModelTemplates{}, fmt.Errorf("Codex models catalog contains a model without a slug")
		}
		if _, exists := templates.bySlug[slug]; exists {
			return codexModelTemplates{}, fmt.Errorf("Codex models catalog contains duplicate slug %q", slug)
		}
		priority, ok := codexCatalogPriority(model)
		if !ok {
			return codexModelTemplates{}, fmt.Errorf("Codex model %q has an invalid priority", slug)
		}
		templates.bySlug[slug] = model
		templates.maxPriority = max(templates.maxPriority, priority)
	}
	if _, ok := templates.bySlug["gpt-5.5"]; !ok {
		return codexModelTemplates{}, fmt.Errorf(`Codex models catalog is missing fallback template "gpt-5.5"`)
	}
	return templates, nil
}

func loadCodexTemplates() (map[string]map[string]any, int) {
	codexModelTemplatesMu.RLock()
	defer codexModelTemplatesMu.RUnlock()
	templates := codexModelTemplatesStore
	return templates.bySlug, templates.maxPriority
}

func buildCodexModelsCatalogResponse(snapshot *planningSnapshot) codexModelsCatalogResponse {
	templates, maxPriority := loadCodexTemplates()
	models := enabledOpenAIModels(snapshot)
	entries := make([]map[string]any, 0, len(models))
	unknownSequence := 0
	for _, model := range models {
		if model.OpenAIAcceptedFormat != nil && *model.OpenAIAcceptedFormat == providerauth.OpenAITextCapabilityChatCompletionsOnly {
			continue
		}
		template, ok := templates[model.ModelID]
		if !ok {
			unknownSequence++
			template = templates["gpt-5.5"]
		}
		entry := cloneCodexModelTemplate(template)
		if !ok {
			entry["slug"] = model.ModelID
			entry["display_name"] = model.ModelID
			entry["description"] = ""
			entry["priority"] = maxPriority + 100*unknownSequence
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		leftPriority, _ := codexCatalogPriority(entries[i])
		rightPriority, _ := codexCatalogPriority(entries[j])
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		leftSlug, _ := entries[i]["slug"].(string)
		rightSlug, _ := entries[j]["slug"].(string)
		return leftSlug < rightSlug
	})
	return codexModelsCatalogResponse{Models: entries}
}

func cloneCodexModelTemplate(template map[string]any) map[string]any {
	encoded, err := json.Marshal(template)
	if err != nil {
		panic(fmt.Sprintf("encode embedded Codex model template: %v", err))
	}
	var clone map[string]any
	if err := json.Unmarshal(encoded, &clone); err != nil {
		panic(fmt.Sprintf("clone embedded Codex model template: %v", err))
	}
	return clone
}

func codexCatalogPriority(model map[string]any) (int, bool) {
	switch priority := model["priority"].(type) {
	case float64:
		value := int(priority)
		return value, priority == float64(value)
	case int:
		return priority, true
	default:
		return 0, false
	}
}

func (s *Service) writeCodexModelsCatalog(w http.ResponseWriter, r *http.Request, snapshot *planningSnapshot) {
	body, err := json.Marshal(buildCodexModelsCatalogResponse(snapshot))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "", "Failed to encode Codex models catalog", nil)
		return
	}
	etag := fmt.Sprintf(`W/"%x"`, sha256.Sum256(body))
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

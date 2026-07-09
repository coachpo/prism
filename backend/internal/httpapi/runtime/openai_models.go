package runtime

import (
	"net/http"
	"sort"
	"strings"
)

type openAIModelsListResponse struct {
	Object string              `json:"object"`
	Data   []openAIModelObject `json:"data"`
}

type openAIModelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func (s *Service) handleOpenAIModelsList(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		writeDomainError(w, runtimeSnapshotDomainError(ErrPublishedRuntimeSnapshotUnavailable))
		return
	}
	_, snapshot, err := s.cache.LoadFreshDefaultRuntimePlan(r.Context())
	if err != nil {
		writeDomainError(w, runtimeSnapshotDomainError(err))
		return
	}
	if r.URL.Query().Has("client_version") {
		s.writeCodexModelsCatalog(w, r, snapshot)
		return
	}
	writeJSON(w, http.StatusOK, buildOpenAIModelsListResponse(snapshot))
}

func buildOpenAIModelsListResponse(snapshot *planningSnapshot) openAIModelsListResponse {
	models := enabledOpenAIModels(snapshot)
	data := make([]openAIModelObject, 0, len(models))
	for _, model := range models {
		data = append(data, openAIModelObject{
			ID:      model.ModelID,
			Object:  "model",
			Created: model.CreatedAt.UTC().Unix(),
			OwnedBy: "prism",
		})
	}
	return openAIModelsListResponse{Object: "list", Data: data}
}

func enabledOpenAIModels(snapshot *planningSnapshot) []runtimeModelRecord {
	models := make([]runtimeModelRecord, 0)
	if snapshot != nil {
		for _, model := range snapshot.ModelsByID {
			if strings.EqualFold(strings.TrimSpace(model.APIFamily), "openai") && strings.TrimSpace(model.ModelID) != "" {
				models = append(models, model)
			}
		}
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].ModelID < models[j].ModelID
	})
	return models
}

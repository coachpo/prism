package models

import (
	"errors"
	"fmt"
	"github.com/coachpo/prism/backend/internal/domain/modelexport"
)

// Readiness executes the same pure renderer used by render with a harmless
// origin and no credential. Discovery is intentionally not an input or gate.
type clientReadinessWire struct {
	Status          string   `json:"status"`
	BlockingReasons []string `json:"blocking_reasons"`
	RepairPath      string   `json:"repair_path"`
}

func clientReadiness(id int, err error) clientReadinessWire {
	result := clientReadinessWire{Status: "ready", BlockingReasons: []string{}, RepairPath: fmt.Sprintf("/route/models/%d", id)}
	if err == nil {
		return result
	}
	result.Status = "blocked"
	reason := "invalid_metadata"
	var unavailable *modelexport.ErrUnselectableModel
	var unbound *modelexport.ErrCandidateUnselected
	var drifted *modelexport.ErrCandidateInvalid
	switch {
	case errors.As(err, &unavailable):
		reason = unavailable.Reason
	case errors.As(err, &unbound):
		reason = "pi_binding_required"
	case errors.As(err, &drifted):
		reason = "pi_binding_identity_changed"
	}
	result.BlockingReasons = []string{reason}
	return result
}

func openCodeReadiness(fact modelexport.OpenCodeModelFact) clientReadinessWire {
	_, err := modelexport.RenderOpenCode(modelexport.OpenCodeInput{
		Facts:     modelexport.OpenCodeSourceFacts{TargetVersion: modelexport.OpenCodeTargetVersion, Models: []modelexport.OpenCodeModelFact{fact}},
		Selection: []int{fact.ModelConfigID}, BaseURL: "http://localhost",
	})
	return clientReadiness(fact.ModelConfigID, err)
}

func piReadiness(fact modelexport.ModelFact) clientReadinessWire {
	_, err := modelexport.RenderPi(modelexport.PiInput{
		Facts:     modelexport.SourceFacts{TargetVersion: modelexport.PiTargetVersion, Models: []modelexport.ModelFact{fact}},
		Selection: []int{fact.ModelConfigID}, BaseURL: "http://localhost",
	})
	return clientReadiness(fact.ModelConfigID, err)
}

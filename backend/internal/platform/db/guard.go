package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/priority"
)

var ErrPriorityEscalation = errors.New("database work priority escalation rejected")

type GuardSpec struct {
	Name     string
	Metadata priority.Metadata
}

func RequireWorkload(ctx context.Context, spec GuardSpec) (admission.Workload, error) {
	if strings.TrimSpace(spec.Name) == "" {
		return admission.Workload{}, fmt.Errorf("database guard name is required")
	}
	if err := spec.Metadata.Validate(); err != nil {
		return admission.Workload{}, err
	}
	workload, err := admission.RequireWorkload(ctx)
	if err != nil {
		return admission.Workload{}, err
	}
	if admission.CompareMetadata(spec.Metadata, workload.Metadata) > 0 {
		return admission.Workload{}, fmt.Errorf("%w: workload=%s operation=%s", ErrPriorityEscalation, metadataLabel(workload.Metadata), metadataLabel(spec.Metadata))
	}
	return workload, nil
}

func metadataLabel(metadata priority.Metadata) string {
	switch metadata.Priority {
	case priority.PriorityManagement:
		return string(metadata.Priority) + ":" + string(metadata.ManagementTier)
	case priority.PriorityBackground:
		return string(metadata.Priority) + ":" + string(metadata.BackgroundSubclass)
	default:
		return string(metadata.Priority)
	}
}

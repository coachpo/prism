package admission

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/priority"
)

var ErrPriorityEscalation = errors.New("priority escalation rejected")

type EscalationError struct {
	Parent priority.Metadata
	Child  priority.Metadata
}

func ChildContext(parent context.Context, spec Spec) (context.Context, context.CancelFunc, error) {
	parentWorkload, err := RequireWorkload(parent)
	if err != nil {
		return nil, nil, err
	}
	if err := spec.Validate(); err != nil {
		return nil, nil, err
	}
	if CompareMetadata(spec.Metadata, parentWorkload.Metadata) > 0 {
		return nil, nil, EscalationError{Parent: parentWorkload.Metadata, Child: spec.Metadata}
	}
	childContext, cancel := attachWorkload(parent, spec, time.Now())
	return childContext, cancel, nil
}

func CompareMetadata(left priority.Metadata, right priority.Metadata) int {
	if leftRank, rightRank := logicalPriorityRank(left.Priority), logicalPriorityRank(right.Priority); leftRank != rightRank {
		return leftRank - rightRank
	}
	switch left.Priority {
	case priority.PriorityManagement:
		return managementTierRank(left.ManagementTier) - managementTierRank(right.ManagementTier)
	case priority.PriorityBackground:
		return backgroundSubclassRank(left.BackgroundSubclass) - backgroundSubclassRank(right.BackgroundSubclass)
	default:
		return 0
	}
}

func (e EscalationError) Error() string {
	return fmt.Sprintf("%s: parent=%s child=%s", ErrPriorityEscalation, metadataLabel(e.Parent), metadataLabel(e.Child))
}

func (e EscalationError) Unwrap() error {
	return ErrPriorityEscalation
}

func logicalPriorityRank(value priority.LogicalPriority) int {
	switch value {
	case priority.PriorityProxy:
		return 3
	case priority.PriorityManagement:
		return 2
	case priority.PriorityBackground:
		return 1
	default:
		return 0
	}
}

func managementTierRank(value priority.ManagementTier) int {
	switch value {
	case priority.ManagementTierM1:
		return 3
	case priority.ManagementTierM2:
		return 2
	case priority.ManagementTierM3:
		return 1
	default:
		return 0
	}
}

func backgroundSubclassRank(value priority.BackgroundSubclass) int {
	switch value {
	case priority.BackgroundSubclassHigh:
		return 3
	case priority.BackgroundSubclassNormal:
		return 2
	case priority.BackgroundSubclassLow:
		return 1
	default:
		return 0
	}
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

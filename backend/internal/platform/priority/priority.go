package priority

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type LogicalPriority string

const (
	PriorityProxy      LogicalPriority = "proxy"
	PriorityManagement LogicalPriority = "management"
	PriorityBackground LogicalPriority = "background"
)

type ManagementTier string

const (
	ManagementTierM1 ManagementTier = "M1"
	ManagementTierM2 ManagementTier = "M2"
	ManagementTierM3 ManagementTier = "M3"
)

type BackgroundSubclass string

const (
	BackgroundSubclassHigh   BackgroundSubclass = "high_background"
	BackgroundSubclassNormal BackgroundSubclass = "normal_background"
	BackgroundSubclassLow    BackgroundSubclass = "low_background"
)

var ErrMissingPriority = errors.New("priority metadata missing")

type Metadata struct {
	Priority           LogicalPriority
	ManagementTier     ManagementTier
	BackgroundSubclass BackgroundSubclass
}

type metadataContextKey struct{}

func ParseLogicalPriority(value string) (LogicalPriority, error) {
	switch normalized := strings.TrimSpace(strings.ToLower(value)); normalized {
	case string(PriorityProxy):
		return PriorityProxy, nil
	case string(PriorityManagement):
		return PriorityManagement, nil
	case string(PriorityBackground):
		return PriorityBackground, nil
	default:
		return "", fmt.Errorf("invalid logical priority %q", value)
	}
}

func ParseManagementTier(value string) (ManagementTier, error) {
	switch normalized := strings.ToUpper(strings.TrimSpace(value)); normalized {
	case string(ManagementTierM1):
		return ManagementTierM1, nil
	case string(ManagementTierM2):
		return ManagementTierM2, nil
	case string(ManagementTierM3):
		return ManagementTierM3, nil
	default:
		return "", fmt.Errorf("invalid management tier %q", value)
	}
}

func ParseBackgroundSubclass(value string) (BackgroundSubclass, error) {
	switch normalized := strings.TrimSpace(strings.ToLower(value)); normalized {
	case string(BackgroundSubclassHigh):
		return BackgroundSubclassHigh, nil
	case string(BackgroundSubclassNormal):
		return BackgroundSubclassNormal, nil
	case string(BackgroundSubclassLow):
		return BackgroundSubclassLow, nil
	default:
		return "", fmt.Errorf("invalid background subclass %q", value)
	}
}

func (m Metadata) Validate() error {
	switch m.Priority {
	case PriorityProxy:
		if m.ManagementTier != "" {
			return fmt.Errorf("proxy priority cannot declare management tier %q", m.ManagementTier)
		}
		if m.BackgroundSubclass != "" {
			return fmt.Errorf("proxy priority cannot declare background subclass %q", m.BackgroundSubclass)
		}
		return nil
	case PriorityManagement:
		if _, err := ParseManagementTier(string(m.ManagementTier)); err != nil {
			return err
		}
		if m.BackgroundSubclass != "" {
			return fmt.Errorf("management priority cannot declare background subclass %q", m.BackgroundSubclass)
		}
		return nil
	case PriorityBackground:
		if m.ManagementTier != "" {
			return fmt.Errorf("background priority cannot declare management tier %q", m.ManagementTier)
		}
		if m.BackgroundSubclass != "" {
			if _, err := ParseBackgroundSubclass(string(m.BackgroundSubclass)); err != nil {
				return err
			}
		}
		return nil
	default:
		return ErrMissingPriority
	}
}

func WithMetadata(ctx context.Context, metadata Metadata) context.Context {
	return context.WithValue(ctx, metadataContextKey{}, metadata)
}

func MetadataFromContext(ctx context.Context) (*Metadata, bool) {
	metadataValue := ctx.Value(metadataContextKey{})
	metadata, ok := metadataValue.(Metadata)
	if !ok {
		return nil, false
	}
	return &Metadata{
		Priority:           metadata.Priority,
		ManagementTier:     metadata.ManagementTier,
		BackgroundSubclass: metadata.BackgroundSubclass,
	}, true
}

func RequireMetadata(ctx context.Context) (Metadata, error) {
	metadata, ok := MetadataFromContext(ctx)
	if !ok {
		return Metadata{}, ErrMissingPriority
	}
	if err := metadata.Validate(); err != nil {
		return Metadata{}, err
	}
	return *metadata, nil
}

package admission

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/priority"
)

var ErrMissingWorkload = errors.New("admission workload context missing")

type Spec struct {
	Name     string
	Metadata priority.Metadata
	Timeout  time.Duration
}

type Workload struct {
	Name       string
	Metadata   priority.Metadata
	AdmittedAt time.Time
	Deadline   time.Time
}

type workloadContextKey struct{}

func (s Spec) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("admission workload name is required")
	}
	if s.Timeout < 0 {
		return fmt.Errorf("admission workload timeout cannot be negative")
	}
	if err := s.Metadata.Validate(); err != nil {
		return err
	}
	return nil
}

func WorkloadFromContext(ctx context.Context) (*Workload, bool) {
	workloadValue := ctx.Value(workloadContextKey{})
	workload, ok := workloadValue.(Workload)
	if !ok {
		return nil, false
	}
	return &Workload{
		Name:       workload.Name,
		Metadata:   workload.Metadata,
		AdmittedAt: workload.AdmittedAt,
		Deadline:   workload.Deadline,
	}, true
}

func RequireWorkload(ctx context.Context) (Workload, error) {
	workload, ok := WorkloadFromContext(ctx)
	if !ok {
		return Workload{}, ErrMissingWorkload
	}
	if err := workload.Metadata.Validate(); err != nil {
		return Workload{}, err
	}
	return *workload, nil
}

func attachWorkload(ctx context.Context, spec Spec, admittedAt time.Time) (context.Context, context.CancelFunc) {
	ctx = priority.WithMetadata(ctx, spec.Metadata)
	cancel := func() {}
	if spec.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, spec.Timeout)
	}
	deadline, _ := ctx.Deadline()
	workload := Workload{
		Name:       strings.TrimSpace(spec.Name),
		Metadata:   spec.Metadata,
		AdmittedAt: admittedAt,
		Deadline:   deadline,
	}
	return context.WithValue(ctx, workloadContextKey{}, workload), cancel
}

package sideeffects

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/platform/managementsideeffects"
)

type managementAfterCommitContextKey string

func TestManagementAfterCommitSemantics(t *testing.T) {
	t.Run("side effect failure does not fail committed primary state", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), managementAfterCommitContextKey("key"), "value")
		var calls []string
		managementsideeffects.AfterCommit(ctx,
			func(got context.Context) error {
				if got != ctx {
					t.Fatal("wake received unexpected context")
				}
				calls = append(calls, "wake")
				return errors.New("dispatcher unavailable")
			},
			func(got context.Context) error {
				if got != ctx {
					t.Fatal("hook received unexpected context")
				}
				calls = append(calls, "hook-1")
				return errors.New("noncritical cache invalidation failed")
			},
			func(got context.Context) error {
				if got != ctx {
					t.Fatal("second hook received unexpected context")
				}
				calls = append(calls, "hook-2")
				return nil
			},
		)
		if strings.Join(calls, ",") != "hook-1,hook-2,wake" {
			t.Fatalf("unexpected after-commit execution order: %v", calls)
		}
	})
}

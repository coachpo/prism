package priority

import (
	"context"
	"errors"
	"testing"
)

func TestParseLogicalPriorityRejectsBackgroundSubclass(t *testing.T) {
	t.Parallel()

	if _, err := ParseLogicalPriority("high_background"); err == nil {
		t.Fatal("expected background subclass to be rejected as a logical priority")
	}
}

func TestParseManagementTier(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"M1", "m2", " M3 "} {
		tier, err := ParseManagementTier(value)
		if err != nil {
			t.Fatalf("ParseManagementTier(%q) returned error: %v", value, err)
		}
		if tier == "" {
			t.Fatalf("ParseManagementTier(%q) returned an empty tier", value)
		}
	}
	if _, err := ParseManagementTier("M4"); err == nil {
		t.Fatal("expected invalid management tier to be rejected")
	}
}

func TestRequireMetadataFailsClosedWhenMissing(t *testing.T) {
	t.Parallel()

	_, err := RequireMetadata(context.Background())
	if !errors.Is(err, ErrMissingPriority) {
		t.Fatalf("expected missing metadata to fail closed, got %v", err)
	}
}

func TestPriorityMetadataRoundTrip(t *testing.T) {
	t.Parallel()

	want := Metadata{Priority: PriorityManagement, ManagementTier: ManagementTierM2}
	got, err := RequireMetadata(WithMetadata(context.Background(), want))
	if err != nil {
		t.Fatalf("RequireMetadata returned error: %v", err)
	}
	if got != want {
		t.Fatalf("RequireMetadata() = %+v, want %+v", got, want)
	}
}

func TestMetadataValidationRejectsWrongSubclassScope(t *testing.T) {
	t.Parallel()

	invalid := Metadata{Priority: PriorityManagement, ManagementTier: ManagementTierM1, BackgroundSubclass: BackgroundSubclassHigh}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected management metadata with background subclass to be rejected")
	}

	valid := Metadata{Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid background metadata, got %v", err)
	}
}

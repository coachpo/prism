package stats

import "testing"

func TestOutputRateTPSFromEvidenceRejectsNegativeTokensAndPreservesZero(t *testing.T) {
	span := 80
	if got := OutputRateTPSFromEvidence(-1, true, OutputRateStateMeasured, &span); got != nil {
		t.Fatalf("negative output tokens must not produce a rate, got %v", *got)
	}
	got := OutputRateTPSFromEvidence(0, true, OutputRateStateMeasured, &span)
	if got == nil || *got != 0 {
		t.Fatalf("measured zero output tokens must remain a real zero, got %v", got)
	}
}

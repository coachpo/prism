package stats

import (
	"testing"
	"time"
)

func TestChainCursorBindsCohortAndCarriesFrozenWindow(t *testing.T) {
	from := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	status := 503
	params := ChainQueryParams{
		ProfileID: 1, StatusCode: &status, CoveragePreset: "24h",
		FromTime: &from, ToTime: &to, CoverageRequestedFrom: &from, CoverageRequestedTo: &to,
		SortBy: "created_at", SortOrder: "desc",
	}
	hash, err := chainCohortFingerprint(params)
	if err != nil {
		t.Fatal(err)
	}
	shiftedFrom, shiftedTo := from.Add(time.Minute), to.Add(time.Minute)
	shifted := params
	shifted.FromTime, shifted.ToTime = &shiftedFrom, &shiftedTo
	shifted.CoverageRequestedFrom, shifted.CoverageRequestedTo = &shiftedFrom, &shiftedTo
	shiftedHash, err := chainCohortFingerprint(shifted)
	if err != nil {
		t.Fatal(err)
	}
	if hash != shiftedHash {
		t.Fatal("wall-clock movement must not change the cohort fingerprint; the signed cursor carries the frozen window")
	}
	otherStatus := 429
	changed := params
	changed.StatusCode = &otherStatus
	changedHash, err := chainCohortFingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedHash == hash {
		t.Fatal("filter change did not change the cohort fingerprint")
	}

	windowFrom, windowTo := chainCursorWindow(params)
	encoded, err := encodeChainCursor(chainCursorPayload{
		Version: 1, ProfileID: 1, OrderAt: to.Format(time.RFC3339Nano), IngressID: "ingress-1",
		Limit: 20, SortOrder: "desc", CohortHash: hash, WindowFrom: windowFrom, WindowTo: windowTo,
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeChainCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var resumed ChainQueryParams
	if err := applyChainCursorWindow(&resumed, decoded.WindowFrom, decoded.WindowTo); err != nil {
		t.Fatal(err)
	}
	if resumed.FromTime == nil || !resumed.FromTime.Equal(from) || resumed.ToTime == nil || !resumed.ToTime.Equal(to) {
		t.Fatalf("resumed window = %v..%v, want %v..%v", resumed.FromTime, resumed.ToTime, from, to)
	}
}

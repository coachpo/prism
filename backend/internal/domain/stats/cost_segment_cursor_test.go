package stats

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestCostSegmentCursorAuthentication(t *testing.T) {
	payload := costSegmentCursorPayload{Version: 1, ProfileID: 1, LastSegmentKey: "e.2", Consumed: 1}
	signingKey := DeriveCostSegmentCursorSigningKey("primary-secret")
	if string(signingKey) == string(DeriveQuerySigningKey("primary-secret")) {
		t.Fatal("cost-segment and query-context signing keys must be domain-separated")
	}
	cursor, err := encodeCostSegmentCursor(payload, signingKey)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	decoded, err := decodeCostSegmentCursor(cursor, signingKey)
	if err != nil || decoded != payload {
		t.Fatalf("round trip cursor: decoded=%+v err=%v", decoded, err)
	}

	parts := strings.Split(cursor, ".")
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode cursor payload: %v", err)
	}
	publicDigest := sha256.Sum256(raw)
	forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString(publicDigest[:])
	for name, candidate := range map[string]struct {
		cursor string
		key    []byte
	}{
		"wrong key":         {cursor: cursor, key: DeriveCostSegmentCursorSigningKey("other-secret")},
		"public SHA digest": {cursor: forged, key: signingKey},
		"unsigned payload":  {cursor: parts[0], key: signingKey},
	} {
		if _, err := decodeCostSegmentCursor(candidate.cursor, candidate.key); err == nil {
			t.Fatalf("%s cursor was accepted", name)
		}
	}
}

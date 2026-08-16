package endpoints

import "testing"

func referenceInt(value int) *int { return &value }

func referenceString(value string) *string { return &value }

func referenceBool(value bool) *bool { return &value }

func ownedReferenceRow(connectionID int, displayName string, position int) referenceRow {
	return referenceRow{
		ConnectionID:       connectionID,
		ConnectionName:     referenceString("target-" + displayName),
		APIFamily:          "openai",
		ConnectionIsActive: true,
		OwnerMatID:         referenceInt(connectionID + 100),
		OwnerMatPosition:   referenceInt(position),
		OwnerMatEnabled:    referenceBool(true),
		OwnerModelConfigID: referenceInt(connectionID + 200),
		OwnerModelID:       referenceString("model-" + displayName),
		OwnerDisplayName:   referenceString(displayName),
		OwnerModelEnabled:  referenceBool(true),
	}
}

func TestDeriveCanonicalSetKeepsItemsAndCursorKeysAligned(t *testing.T) {
	t.Parallel()

	set := deriveCanonicalSet(7, []referenceRow{
		ownedReferenceRow(3, "Zulu", 2),
		{
			ConnectionID:       4,
			ConnectionName:     referenceString("orphan"),
			APIFamily:          "openai",
			ConnectionIsActive: false,
		},
		ownedReferenceRow(2, "Alpha", 1),
	})

	if len(set.Items) != 3 || len(set.OrderKeys) != 3 {
		t.Fatalf("expected three aligned rows, got items=%d keys=%d", len(set.Items), len(set.OrderKeys))
	}
	wantIDs := []int{2, 3, 4}
	for index, wantID := range wantIDs {
		if set.Items[index].ConnectionID != wantID {
			t.Fatalf("item %d: expected connection %d, got %d", index, wantID, set.Items[index].ConnectionID)
		}
		if set.OrderKeys[index].Connection != wantID {
			t.Fatalf("cursor key %d: expected connection %d, got %d", index, wantID, set.OrderKeys[index].Connection)
		}
	}
	if set.Items[0].Enabled != true || set.Items[1].Enabled != true || set.Items[2].Enabled {
		t.Fatalf("unexpected enabled projection: %+v", set.Items)
	}
}

func TestDecodeReferenceCursorRejectsTampering(t *testing.T) {
	t.Parallel()

	cursor, err := encodeReferenceCursor(referenceCursor{
		Version:      1,
		ProfileID:    1,
		EndpointID:   7,
		Limit:        50,
		SnapshotHash: "snapshot",
		LastKey:      "0|alpha|model|2|1|3",
	}, "test-secret")
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	if _, err := decodeReferenceCursor(cursor, "wrong-secret"); err == nil {
		t.Fatal("expected cursor signed with another key to be rejected")
	}
}

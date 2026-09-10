package models

import "testing"

func TestBatchRequestBoundsAndActionSeparation(t *testing.T) {
	cases := []struct {
		name, action string
		input        batchRequest
	}{
		{"empty", "model_strategy", batchRequest{ReferenceID: 1}},
		{"duplicates", "model_strategy", batchRequest{ReferenceID: 1, Items: []batchItemInput{{ID: 1}, {ID: 1}}}},
		{"negative", "model_strategy", batchRequest{ReferenceID: 1, Items: []batchItemInput{{ID: -1}}}},
		{"unavailable reference", "target_pricing", batchRequest{Items: []batchItemInput{{ID: 1}}}},
		{"independent Pi owner", "model_limits", batchRequest{Client: "pi", Items: []batchItemInput{{ID: 1}}}},
	}
	many := batchRequest{ReferenceID: 1}
	for id := 1; id <= 21; id++ {
		many.Items = append(many.Items, batchItemInput{ID: id})
	}
	cases = append(cases, struct {
		name, action string
		input        batchRequest
	}{"unbounded", "model_strategy", many})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateBatchRequest(tc.action, &tc.input); err == nil {
				t.Fatal("invalid batch accepted")
			}
		})
	}
}

func TestBatchLimitContract(t *testing.T) {
	ptr := func(v int64) *int64 { return &v }
	for _, item := range []batchItemInput{{}, {ContextLimit: ptr(100), OutputLimit: ptr(101)}, {ContextLimit: ptr(0), OutputLimit: ptr(1)}, {ContextLimit: ptr(9007199254740992), OutputLimit: ptr(1)}} {
		if limitInputError(item) == "" {
			t.Fatalf("invalid limits accepted: %+v", item)
		}
	}
	if limitInputError(batchItemInput{ContextLimit: ptr(100), OutputLimit: ptr(100)}) != "" {
		t.Fatal("equal context/output is valid")
	}
}

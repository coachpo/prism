package responseutil

import (
	"net/http/httptest"
	"testing"
)

func TestSetPrivateNoStoreHeadersMergesVary(t *testing.T) {
	recorder := httptest.NewRecorder()
	recorder.Header().Add("Vary", "Origin, authorization")
	recorder.Header().Add("Vary", "Accept-Encoding")

	SetPrivateNoStoreHeaders(recorder)

	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("expected private no-store cache control, got %q", got)
	}
	if got := recorder.Header().Get("Vary"); got != "Origin, authorization, Accept-Encoding, Cookie, X-Profile-Id" {
		t.Fatalf("expected merged, de-duplicated Vary fields, got %q", got)
	}
}

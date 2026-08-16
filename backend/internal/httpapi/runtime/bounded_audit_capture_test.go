package runtime

import (
	"bytes"
	"testing"
)

// Bounded audit capture contract (Requests SPEC §3.1): the per-body cap is
// enforced during the copy, observed counts all bytes, stored retains the
// first 4 MiB only, and disabled capture allocates nothing.

func TestBoundedAuditBufferEnforcesPerBodyCapDuringCopy(t *testing.T) {
	buffer := newBoundedAuditBuffer(true)
	payload := bytes.Repeat([]byte("a"), auditBodyCapBytes+4096)
	written, err := buffer.Write(payload)
	if err != nil || written != len(payload) {
		t.Fatalf("expected full-count write, got %d/%v", written, err)
	}
	retained, observed, stored, truncated := buffer.snapshot()
	if observed != int64(len(payload)) {
		t.Fatalf("expected observed to count all bytes (%d), got %d", len(payload), observed)
	}
	if stored != auditBodyCapBytes {
		t.Fatalf("expected stored capped at 4 MiB, got %d", stored)
	}
	if !truncated {
		t.Fatal("expected truncated=true for an over-cap body")
	}
	if len(retained) != auditBodyCapBytes {
		t.Fatalf("expected retained prefix of exactly 4 MiB, got %d", len(retained))
	}
	if !bytes.Equal(retained, payload[:auditBodyCapBytes]) {
		t.Fatal("expected retained prefix to be the first 4 MiB byte-for-byte")
	}
}

func TestBoundedAuditBufferMultipleWritesShareCap(t *testing.T) {
	buffer := newBoundedAuditBuffer(true)
	half := auditBodyCapBytes / 2
	first := bytes.Repeat([]byte("x"), half)
	second := bytes.Repeat([]byte("y"), half+1024)
	if _, err := buffer.Write(first); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := buffer.Write(second); err != nil {
		t.Fatalf("second write: %v", err)
	}
	retained, observed, stored, truncated := buffer.snapshot()
	if observed != int64(half+half+1024) {
		t.Fatalf("expected observed to count both writes, got %d", observed)
	}
	if stored != auditBodyCapBytes {
		t.Fatalf("expected stored capped at 4 MiB across writes, got %d", stored)
	}
	if !truncated {
		t.Fatal("expected truncated=true")
	}
	expected := append(append([]byte(nil), first...), second[:half]...)
	if !bytes.Equal(retained, expected) {
		t.Fatal("expected retained prefix across writes to be byte-exact")
	}
}

func TestBoundedAuditBufferDisabledCaptureAllocatesNothing(t *testing.T) {
	buffer := newBoundedAuditBuffer(false)
	payload := bytes.Repeat([]byte("z"), 8192)
	if written, err := buffer.Write(payload); err != nil || written != len(payload) {
		t.Fatalf("expected pass-through write, got %d/%v", written, err)
	}
	retained, observed, stored, truncated := buffer.snapshot()
	if retained != nil || observed != 0 || stored != 0 || truncated {
		t.Fatalf("expected disabled capture to stay empty, got retained=%v observed=%d stored=%d truncated=%v", retained != nil, observed, stored, truncated)
	}
	if buffer.buffer != nil {
		t.Fatal("expected disabled capture to allocate no buffer")
	}
}

func TestBoundedAuditWriterNilWhenDisabled(t *testing.T) {
	_, writer := newBoundedAuditWriter(false)
	if writer != nil {
		t.Fatal("expected nil writer when capture is disabled")
	}
	buffer, writer := newBoundedAuditWriter(true)
	if writer == nil || buffer == nil {
		t.Fatal("expected non-nil buffer and writer when capture is enabled")
	}
}

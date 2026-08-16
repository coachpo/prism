package runtime

import (
	"io"
	"sync"
)

// auditBodyCapBytes is the per-body audit capture cap (4 MiB raw bytes) from
// Requests SPEC §3.1. The bounded counting writer enforces the cap during the
// copy, never at INSERT time.
const auditBodyCapBytes = 4 * 1024 * 1024

// boundedAuditBuffer is the bounded counting writer for audit body capture.
// It counts every observed byte but retains only the first auditBodyCapBytes.
// When audit capture is disabled no buffer is allocated and no false counts
// are produced.
type boundedAuditBuffer struct {
	mu       sync.Mutex
	observed int64
	stored   int64
	buffer   []byte
	enabled  bool
}

func newBoundedAuditBuffer(enabled bool) *boundedAuditBuffer {
	return &boundedAuditBuffer{enabled: enabled}
}

// Write implements io.Writer: counts all bytes, retains the first 4 MiB.
func (buffer *boundedAuditBuffer) Write(payload []byte) (int, error) {
	if buffer == nil || !buffer.enabled {
		return len(payload), nil
	}
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	buffer.observed += int64(len(payload))
	remaining := auditBodyCapBytes - buffer.stored
	if remaining > 0 {
		admitted := int64(len(payload))
		if admitted > remaining {
			admitted = remaining
		}
		buffer.buffer = append(buffer.buffer, payload[:admitted]...)
		buffer.stored += admitted
	}
	return len(payload), nil
}

// snapshot returns the retained prefix and the observed/stored counts.
func (buffer *boundedAuditBuffer) snapshot() ([]byte, int64, int64, bool) {
	if buffer == nil || !buffer.enabled {
		return nil, 0, 0, false
	}
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if len(buffer.buffer) == 0 {
		return nil, buffer.observed, buffer.stored, buffer.stored > 0 && buffer.stored < buffer.observed
	}
	retained := append([]byte(nil), buffer.buffer...)
	return retained, buffer.observed, buffer.stored, buffer.stored > 0 && buffer.stored < buffer.observed
}

// boundedAuditWriteCloser adapts a boundedAuditBuffer to an io.WriteCloser
// that can be drained by copy loops.
type boundedAuditWriteCloser struct {
	buffer *boundedAuditBuffer
}

func (writer *boundedAuditWriteCloser) Write(payload []byte) (int, error) {
	return writer.buffer.Write(payload)
}

func (writer *boundedAuditWriteCloser) Close() error { return nil }

// newBoundedAuditWriter returns an io.Writer for the copy path; the buffer is
// non-nil only when capture is enabled (no allocation otherwise).
func newBoundedAuditWriter(enabled bool) (*boundedAuditBuffer, io.Writer) {
	buffer := newBoundedAuditBuffer(enabled)
	if !enabled {
		return buffer, nil
	}
	return buffer, &boundedAuditWriteCloser{buffer: buffer}
}

// auditCaptureResult is the finalized capture evidence for one body.
type auditCaptureResult struct {
	Body      []byte
	Observed  int64
	Stored    int64
	Truncated bool
	Enabled   bool
}

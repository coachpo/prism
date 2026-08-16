package runtime

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestRuntimeAttemptLeaseBodyReleasesAtBodyTermination(t *testing.T) {
	tests := []struct {
		name string
		body io.ReadCloser
		act  func(*runtimeAttemptLeaseBody) error
	}{
		{
			name: "eof",
			body: io.NopCloser(strings.NewReader("payload")),
			act: func(body *runtimeAttemptLeaseBody) error {
				_, err := io.ReadAll(body)
				return err
			},
		},
		{
			name: "read error",
			body: &leaseBodyReadError{err: errors.New("read failed")},
			act: func(body *runtimeAttemptLeaseBody) error {
				_, err := body.Read(make([]byte, 1))
				return err
			},
		},
		{
			name: "close",
			body: io.NopCloser(strings.NewReader("payload")),
			act: func(body *runtimeAttemptLeaseBody) error {
				return body.Close()
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			releases := 0
			body := &runtimeAttemptLeaseBody{ReadCloser: test.body, release: func() { releases++ }}
			_ = test.act(body)
			_ = body.Close()
			if releases != 1 {
				t.Fatalf("expected one idempotent lease release, got %d", releases)
			}
		})
	}
}

type leaseBodyReadError struct {
	err error
}

func (body *leaseBodyReadError) Read([]byte) (int, error) { return 0, body.err }
func (body *leaseBodyReadError) Close() error             { return nil }

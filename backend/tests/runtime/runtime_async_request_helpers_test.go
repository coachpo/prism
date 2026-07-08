package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type benchmarkAsyncRequestResult struct {
	StatusCode int
	Body       string
	Err        error
}

func startAsyncBenchmarkPriorityRequest(tb testing.TB, client *http.Client, method string, url string, body any, headers map[string]string) <-chan benchmarkAsyncRequestResult {
	tb.Helper()
	var rawBody []byte
	if body != nil {
		encodedBody, err := json.Marshal(body)
		if err != nil {
			tb.Fatalf("marshal async request body for %s %s: %v", method, url, err)
		}
		rawBody = encodedBody
	}

	resultCh := make(chan benchmarkAsyncRequestResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		var requestBody io.Reader
		if rawBody != nil {
			requestBody = bytes.NewReader(rawBody)
		}
		request, err := http.NewRequestWithContext(ctx, method, url, requestBody)
		if err != nil {
			resultCh <- benchmarkAsyncRequestResult{Err: fmt.Errorf("build async request %s %s: %w", method, url, err)}
			return
		}
		if rawBody != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		response, err := client.Do(request)
		if err != nil {
			resultCh <- benchmarkAsyncRequestResult{Err: fmt.Errorf("perform async request %s %s: %w", method, url, err)}
			return
		}
		defer func() { _ = response.Body.Close() }()
		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			resultCh <- benchmarkAsyncRequestResult{Err: fmt.Errorf("read async response body for %s %s: %w", method, url, err)}
			return
		}
		resultCh <- benchmarkAsyncRequestResult{StatusCode: response.StatusCode, Body: string(bytes.TrimSpace(responseBody))}
	}()
	return resultCh
}

func assertAsyncBenchmarkRequestStatus(tb testing.TB, resultCh <-chan benchmarkAsyncRequestResult, wantStatus int) {
	tb.Helper()
	result := awaitAsyncBenchmarkRequest(tb, resultCh, 5*time.Second)
	if result.Err != nil {
		tb.Fatalf("expected async request to succeed, got error: %v", result.Err)
	}
	if result.StatusCode != wantStatus {
		tb.Fatalf("expected async request status %d, got %d with body %s", wantStatus, result.StatusCode, result.Body)
	}
}

func awaitAsyncBenchmarkRequest(tb testing.TB, resultCh <-chan benchmarkAsyncRequestResult, timeout time.Duration) benchmarkAsyncRequestResult {
	tb.Helper()
	select {
	case result := <-resultCh:
		return result
	case <-time.After(timeout):
		tb.Fatalf("timed out waiting for async request result after %s", timeout)
		return benchmarkAsyncRequestResult{}
	}
}

func waitForStatsLockWaitersTB(tb testing.TB, conn *pgx.Conn, want int, timeout time.Duration) {
	tb.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var waiters int
		if err := conn.QueryRow(
			context.Background(),
			`SELECT COUNT(*)
            FROM pg_stat_activity
            WHERE pid <> pg_backend_pid()
              AND datname = current_database()
              AND usename = current_user
              AND state = 'active'
              AND wait_event_type = 'Lock'`,
		).Scan(&waiters); err != nil {
			tb.Fatalf("count blocked stats waiters: %v", err)
		}
		if waiters >= want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	tb.Fatalf("timed out waiting for %d blocked stats requests", want)
}

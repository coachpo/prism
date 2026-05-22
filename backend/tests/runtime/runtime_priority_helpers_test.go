package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type runtimeStatsTablesLock struct {
	conn *pgx.Conn
	tx   pgx.Tx
	once sync.Once
}

func holdRuntimeStatsTablesLock(t *testing.T, databaseName string) *runtimeStatsTablesLock {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := connectDatabase(t, ctx, sharedPostgresHarness.connectionString(databaseName))
	tx, err := conn.Begin(ctx)
	if err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("begin stats lock transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE request_logs IN ACCESS EXCLUSIVE MODE`); err != nil {
		_ = tx.Rollback(ctx)
		_ = conn.Close(ctx)
		t.Fatalf("lock request_logs table: %v", err)
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE usage_request_events IN ACCESS EXCLUSIVE MODE`); err != nil {
		_ = tx.Rollback(ctx)
		_ = conn.Close(ctx)
		t.Fatalf("lock usage_request_events table: %v", err)
	}
	return &runtimeStatsTablesLock{conn: conn, tx: tx}
}

func (lock *runtimeStatsTablesLock) release(t *testing.T) {
	t.Helper()
	lock.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = lock.tx.Rollback(ctx)
		_ = lock.conn.Close(ctx)
	})
}

func startAsyncPriorityRequest(t *testing.T, client *http.Client, method string, url string, body any, headers map[string]string) <-chan concurrentRuntimeRequestResult {
	t.Helper()
	var rawBody []byte
	if body != nil {
		encodedBody, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal async request body for %s %s: %v", method, url, err)
		}
		rawBody = encodedBody
	}

	resultCh := make(chan concurrentRuntimeRequestResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var requestBody io.Reader
		if rawBody != nil {

			requestBody = bytes.NewReader(rawBody)
		}
		request, err := http.NewRequestWithContext(ctx, method, url, requestBody)
		if err != nil {
			resultCh <- concurrentRuntimeRequestResult{Err: fmt.Errorf("build request %s %s: %w", method, url, err)}
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
			resultCh <- concurrentRuntimeRequestResult{Err: fmt.Errorf("perform request %s %s: %w", method, url, err)}
			return
		}
		defer func() { _ = response.Body.Close() }()
		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			resultCh <- concurrentRuntimeRequestResult{Err: fmt.Errorf("read response body for %s %s: %w", method, url, err)}
			return
		}
		resultCh <- concurrentRuntimeRequestResult{StatusCode: response.StatusCode, Body: strings.TrimSpace(string(responseBody))}
	}()
	return resultCh
}

func performPriorityRequest(t *testing.T, client *http.Client, timeout time.Duration, method string, url string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var rawBody []byte
	if body != nil {
		encodedBody, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body for %s %s: %v", method, url, err)
		}
		rawBody = encodedBody
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	var requestBody io.Reader
	if rawBody != nil {
		requestBody = bytes.NewReader(rawBody)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, requestBody)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, url, err)
	}
	if rawBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("perform request %s %s: %v", method, url, err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func awaitAsyncRequest(t *testing.T, resultCh <-chan concurrentRuntimeRequestResult, timeout time.Duration) concurrentRuntimeRequestResult {
	t.Helper()
	select {
	case result := <-resultCh:
		return result
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for async request result after %s", timeout)
		return concurrentRuntimeRequestResult{}
	}
}

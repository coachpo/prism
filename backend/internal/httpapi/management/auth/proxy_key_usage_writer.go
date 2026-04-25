package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultProxyKeyUsageFlushInterval   = 500 * time.Millisecond
	defaultProxyKeyUsageShutdownTimeout = 3 * time.Second
	defaultProxyKeyUsageWriteTimeout    = 5 * time.Second
	proxyKeyUsageBackgroundPoolMaxConns = 1
)

type proxyAPIKeyUsageUpdate struct {
	KeyID      int
	LastUsedAt time.Time
	LastUsedIP string
}

type proxyAPIKeyUsageWriter struct {
	write func(context.Context, int, time.Time, string) error

	flushInterval   time.Duration
	shutdownTimeout time.Duration
	writeTimeout    time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
	done            chan struct{}

	mu      sync.Mutex
	pending map[int]proxyAPIKeyUsageUpdate
	closed  bool
}

func newProxyKeyUsagePool(databaseURL string) (*pgxpool.Pool, error) {
	parsedConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse proxy api key usage pool config: %w", err)
	}
	parsedConfig.MaxConns = proxyKeyUsageBackgroundPoolMaxConns
	parsedConfig.MinConns = 0
	pool, err := pgxpool.NewWithConfig(context.Background(), parsedConfig)
	if err != nil {
		return nil, fmt.Errorf("create proxy api key usage pool: %w", err)
	}
	return pool, nil
}

func newProxyAPIKeyUsageWriter(write func(context.Context, int, time.Time, string) error) *proxyAPIKeyUsageWriter {
	if write == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	writer := &proxyAPIKeyUsageWriter{
		write:           write,
		flushInterval:   defaultProxyKeyUsageFlushInterval,
		shutdownTimeout: defaultProxyKeyUsageShutdownTimeout,
		writeTimeout:    defaultProxyKeyUsageWriteTimeout,
		ctx:             ctx,
		cancel:          cancel,
		done:            make(chan struct{}),
		pending:         map[int]proxyAPIKeyUsageUpdate{},
	}
	go writer.run()
	return writer
}

func (w *proxyAPIKeyUsageWriter) Enqueue(keyID int, lastUsedAt time.Time, lastUsedIP string) error {
	if w == nil || keyID <= 0 {
		return nil
	}
	update := proxyAPIKeyUsageUpdate{KeyID: keyID, LastUsedAt: lastUsedAt.UTC(), LastUsedIP: lastUsedIP}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fmt.Errorf("proxy api key usage writer closed")
	}
	if existing, ok := w.pending[keyID]; ok {
		if existing.LastUsedAt.After(update.LastUsedAt) {
			return nil
		}
		if existing.LastUsedAt.Equal(update.LastUsedAt) && strings.TrimSpace(update.LastUsedIP) == "" {
			update.LastUsedIP = existing.LastUsedIP
		}
	}
	w.pending[keyID] = update
	return nil
}

func (w *proxyAPIKeyUsageWriter) Close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	w.cancel()
	select {
	case <-w.done:
	case <-time.After(w.shutdownTimeout + time.Second):
	}
}

func (w *proxyAPIKeyUsageWriter) run() {
	defer close(w.done)
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.ctx.Done():
			w.flushPending(w.shutdownTimeout)
			return
		case <-ticker.C:
			w.flushPending(w.writeTimeout)
		}
	}
}

func (w *proxyAPIKeyUsageWriter) flushPending(timeout time.Duration) {
	updates := w.drainPending()
	if len(updates) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for _, update := range updates {
		if err := w.write(ctx, update.KeyID, update.LastUsedAt, update.LastUsedIP); err != nil {
			w.requeue(updates)
			slog.Error("failed to flush proxy api key usage updates", "error", err, "pending_keys", len(updates))
			return
		}
	}
}

func (w *proxyAPIKeyUsageWriter) drainPending() []proxyAPIKeyUsageUpdate {
	w.mu.Lock()
	defer w.mu.Unlock()
	updates := make([]proxyAPIKeyUsageUpdate, 0, len(w.pending))
	for keyID, update := range w.pending {
		updates = append(updates, update)
		delete(w.pending, keyID)
	}
	return updates
}

func (w *proxyAPIKeyUsageWriter) requeue(updates []proxyAPIKeyUsageUpdate) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, update := range updates {
		existing, ok := w.pending[update.KeyID]
		if ok {
			if existing.LastUsedAt.After(update.LastUsedAt) {
				continue
			}
			if existing.LastUsedAt.Equal(update.LastUsedAt) && strings.TrimSpace(update.LastUsedIP) == "" {
				update.LastUsedIP = existing.LastUsedIP
			}
		}
		w.pending[update.KeyID] = update
	}
}

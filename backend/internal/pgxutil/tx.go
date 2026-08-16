package pgxutil

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type Beginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type beforeCommitHookContextKey struct{}

type BeforeCommitHook func(context.Context, pgx.Tx) error

func WithBeforeCommitHook(ctx context.Context, hook BeforeCommitHook) context.Context {
	if hook == nil {
		return ctx
	}
	return context.WithValue(ctx, beforeCommitHookContextKey{}, hook)
}

func InTx(ctx context.Context, beginner Beginner, label string, fn func(pgx.Tx) error) error {
	_, err := InTxValue(ctx, beginner, label, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, fn(tx)
	})
	return err
}

func InTxValue[T any](ctx context.Context, beginner Beginner, label string, fn func(pgx.Tx) (T, error)) (T, error) {
	return inTxValue(ctx, beginner, label, pgx.TxOptions{}, fn)
}

func InReadOnlyTxValue[T any](ctx context.Context, beginner Beginner, label string, fn func(pgx.Tx) (T, error)) (T, error) {
	return inTxValue(ctx, beginner, label, pgx.TxOptions{AccessMode: pgx.ReadOnly}, fn)
}

func inTxValue[T any](ctx context.Context, beginner Beginner, label string, options pgx.TxOptions, fn func(pgx.Tx) (T, error)) (value T, err error) {
	tx, err := beginner.BeginTx(ctx, options)
	if err != nil {
		return value, fmt.Errorf("begin %s transaction: %w", label, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	value, err = fn(tx)
	if err != nil {
		return value, err
	}
	// Runtime-cache generation bumps are writes and must land in the same
	// transaction as the primary state change (architecture.md:140). A
	// read-only transaction can never be that transaction, so the hook is
	// skipped there instead of failing the request or advancing generations
	// ahead of the write.
	if options.AccessMode != pgx.ReadOnly {
		if hook, ok := ctx.Value(beforeCommitHookContextKey{}).(BeforeCommitHook); ok && hook != nil {
			if err := hook(ctx, tx); err != nil {
				return value, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return value, fmt.Errorf("commit %s transaction: %w", label, err)
	}
	return value, nil
}

// InRepeatableReadTxValue runs fn inside a REPEATABLE READ read-only
// transaction: the same snapshot covers coverage resolution, preflight counts,
// and the row scan (Requests SPEC §6.8 export contract).
func InRepeatableReadTxValue[T any](ctx context.Context, beginner Beginner, label string, fn func(pgx.Tx) (T, error)) (T, error) {
	return inTxValue(ctx, beginner, label, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	}, fn)
}

// InRepeatableReadWriteTx runs fn in a bounded REPEATABLE READ transaction
// that may publish a durable result after all owner projections were read.
func InRepeatableReadWriteTx(ctx context.Context, beginner Beginner, label string, fn func(pgx.Tx) error) error {
	_, err := InRepeatableReadWriteTxValue(ctx, beginner, label, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, fn(tx)
	})
	return err
}

// InRepeatableReadWriteTxValue is the write-capable counterpart to
// InRepeatableReadTxValue.
func InRepeatableReadWriteTxValue[T any](ctx context.Context, beginner Beginner, label string, fn func(pgx.Tx) (T, error)) (T, error) {
	return inTxValue(ctx, beginner, label, pgx.TxOptions{IsoLevel: pgx.RepeatableRead}, fn)
}

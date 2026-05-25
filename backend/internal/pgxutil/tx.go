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

func inTxValue[T any](ctx context.Context, beginner Beginner, label string, options pgx.TxOptions, fn func(pgx.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := beginner.BeginTx(ctx, options)
	if err != nil {
		return zero, fmt.Errorf("begin %s transaction: %w", label, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	value, err := fn(tx)
	if err != nil {
		return zero, err
	}
	if hook, ok := ctx.Value(beforeCommitHookContextKey{}).(BeforeCommitHook); ok && hook != nil {
		if err := hook(ctx, tx); err != nil {
			return zero, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit %s transaction: %w", label, err)
	}
	return value, nil
}

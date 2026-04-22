package pgxutil

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type Beginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func InTx(ctx context.Context, beginner Beginner, label string, fn func(pgx.Tx) error) error {
	_, err := InTxValue(ctx, beginner, label, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, fn(tx)
	})
	return err
}

func InTxValue[T any](ctx context.Context, beginner Beginner, label string, fn func(pgx.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := beginner.BeginTx(ctx, pgx.TxOptions{})
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
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit %s transaction: %w", label, err)
	}
	return value, nil
}

package proxykeyusage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func RecordTx(ctx context.Context, tx pgx.Tx, keyID int, lastUsedAt time.Time, lastUsedIP string) error {
	return RecordWithExecutor(ctx, tx, keyID, lastUsedAt, lastUsedIP)
}

func RecordWithExecutor(ctx context.Context, exec Executor, keyID int, lastUsedAt time.Time, lastUsedIP string) error {
	if exec == nil {
		return fmt.Errorf("database executor unavailable")
	}
	if keyID <= 0 || lastUsedAt.IsZero() {
		return nil
	}
	trimmedIP := strings.TrimSpace(lastUsedIP)
	var nullableIP any
	if trimmedIP != "" {
		nullableIP = trimmedIP
	}
	if _, err := exec.Exec(
		ctx,
		`UPDATE proxy_api_keys
		SET last_used_at = CASE
				WHEN last_used_at IS NULL OR last_used_at < $2 THEN $2
				ELSE last_used_at
			END,
			last_used_ip = CASE
				WHEN last_used_at IS NULL OR last_used_at <= $2 THEN $3
				ELSE last_used_ip
			END,
			updated_at = GREATEST(updated_at, $2)
		WHERE id = $1`,
		keyID,
		lastUsedAt.UTC(),
		nullableIP,
	); err != nil {
		return err
	}
	return nil
}

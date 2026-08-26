package stats

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func loadReportCurrencyPreferences(ctx context.Context, exec queryExecutor, profileID int) (string, string, error) {
	var code string
	var symbol string
	err := exec.QueryRow(ctx, `SELECT report_currency_code, report_currency_symbol FROM user_settings WHERE profile_id = $1 ORDER BY id ASC LIMIT 1`, profileID).Scan(&code, &symbol)
	if err == nil {
		return code, symbol, nil
	}
	if err == pgx.ErrNoRows {
		return "USD", "$", nil
	}
	return "", "", fmt.Errorf("load report currency preferences for profile %d: %w", profileID, err)
}

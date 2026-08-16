package loadbalance

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// strategyImpactCursor is the opaque keyset cursor for the bounded strategy
// impact list. It binds profile, strategy, sort, limit and the profile's
// runtime-planning configuration generation so a stale cursor cannot be reused
// across a changed configuration scope.
type strategyImpactCursor struct {
	ProfileID          int    `json:"profile_id"`
	StrategyID         int    `json:"strategy_id"`
	Limit              int    `json:"limit"`
	PlanningGeneration int64  `json:"planning_generation"`
	AfterDisplayKey    string `json:"after_display_key,omitempty"`
	AfterModelConfigID int    `json:"after_model_config_id,omitempty"`
}

func encodeStrategyImpactCursor(cursor strategyImpactCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("marshal strategy impact cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeStrategyImpactCursor(raw string) (strategyImpactCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return strategyImpactCursor{}, fmt.Errorf("invalid strategy impact cursor")
	}
	var cursor strategyImpactCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return strategyImpactCursor{}, fmt.Errorf("invalid strategy impact cursor")
	}
	if cursor.ProfileID <= 0 || cursor.StrategyID <= 0 || cursor.Limit < 1 || cursor.Limit > 100 {
		return strategyImpactCursor{}, fmt.Errorf("invalid strategy impact cursor")
	}
	return cursor, nil
}

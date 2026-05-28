package loadbalance

type ImportedStrategyDocument struct {
	Name                   string
	LegacyStrategyType     *string
	FailureStatusCodes     []int
	BanMode                *string
	RetryBaseDelayMS       *int
	RetryBackoffMultiplier *float64
	RetryJitterRatio       *float64
	RetryMaxDelayMS        *int
	RetryMaxAttempts       *int
	BanDurationSeconds     *int
}

type CanonicalImportedStrategy struct {
	Name                   string
	LegacyStrategyType     *string
	FailureStatusCodes     []int
	BanMode                string
	RetryBaseDelayMS       int
	RetryBackoffMultiplier float64
	RetryJitterRatio       float64
	RetryMaxDelayMS        int
	RetryMaxAttempts       int
	BanDurationSeconds     int
	LegacyStrategyPtr      *string
}

func CanonicalizeImportedStrategyDocument(document ImportedStrategyDocument) (CanonicalImportedStrategy, error) {
	payload, err := canonicalizeStrategyRequest(loadbalanceStrategyRequest{
		Name:                   document.Name,
		LegacyStrategyType:     document.LegacyStrategyType,
		FailureStatusCodes:     document.FailureStatusCodes,
		BanMode:                document.BanMode,
		RetryBaseDelayMS:       document.RetryBaseDelayMS,
		RetryBackoffMultiplier: document.RetryBackoffMultiplier,
		RetryJitterRatio:       document.RetryJitterRatio,
		RetryMaxDelayMS:        document.RetryMaxDelayMS,
		RetryMaxAttempts:       document.RetryMaxAttempts,
		BanDurationSeconds:     document.BanDurationSeconds,
	})
	if err != nil {
		return CanonicalImportedStrategy{}, err
	}

	legacyStrategyType := payload.LegacyStrategyType
	return CanonicalImportedStrategy{
		Name:                   payload.Name,
		LegacyStrategyType:     &legacyStrategyType,
		FailureStatusCodes:     append([]int(nil), payload.FailureStatusCodes...),
		BanMode:                payload.BanMode,
		RetryBaseDelayMS:       payload.RetryBaseDelayMS,
		RetryBackoffMultiplier: payload.RetryBackoffMultiplier,
		RetryJitterRatio:       payload.RetryJitterRatio,
		RetryMaxDelayMS:        payload.RetryMaxDelayMS,
		RetryMaxAttempts:       payload.RetryMaxAttempts,
		BanDurationSeconds:     payload.BanDurationSeconds,
		LegacyStrategyPtr:      &legacyStrategyType,
	}, nil
}

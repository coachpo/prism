package contextcapability

import "fmt"

const (
	DefaultOutputTokenReserve    = 4096
	DefaultMaxContextUtilization = 0.90
)

type Settings struct {
	ContextWindowTokens       *int
	DefaultOutputTokenReserve int
	MaxContextUtilization     float64
}

func NormalizeModelSettings(contextWindowTokens *int, defaultOutputTokenReserve *int, maxContextUtilization *float64) (Settings, error) {
	resolvedContextWindowTokens, err := NormalizeContextWindowTokens(contextWindowTokens)
	if err != nil {
		return Settings{}, err
	}
	resolvedOutputTokenReserve, err := NormalizeOutputTokenReserve(defaultOutputTokenReserve)
	if err != nil {
		return Settings{}, err
	}
	resolvedMaxContextUtilization, err := NormalizeMaxContextUtilization(maxContextUtilization)
	if err != nil {
		return Settings{}, err
	}
	return Settings{ContextWindowTokens: resolvedContextWindowTokens,
		DefaultOutputTokenReserve: resolvedOutputTokenReserve,
		MaxContextUtilization:     resolvedMaxContextUtilization,
	}, nil
}

func NormalizeConnectionSettings(base Settings, contextWindowTokens *int, defaultOutputTokenReserve *int, maxContextUtilization *float64) (Settings, error) {
	resolvedContextWindowTokens := CopyIntPtr(base.ContextWindowTokens)
	if contextWindowTokens != nil {
		var err error
		resolvedContextWindowTokens, err = NormalizeContextWindowTokens(contextWindowTokens)
		if err != nil {
			return Settings{}, err
		}
	}
	resolvedOutputTokenReserve, err := NormalizeOutputTokenReserveWithFallback(defaultOutputTokenReserve, base.DefaultOutputTokenReserve)
	if err != nil {
		return Settings{}, err
	}
	resolvedMaxContextUtilization, err := NormalizeMaxContextUtilizationWithFallback(maxContextUtilization, base.MaxContextUtilization)
	if err != nil {
		return Settings{}, err
	}
	return Settings{ContextWindowTokens: resolvedContextWindowTokens,
		DefaultOutputTokenReserve: resolvedOutputTokenReserve,
		MaxContextUtilization:     resolvedMaxContextUtilization,
	}, nil
}

func NormalizeContextWindowTokens(value *int) (*int, error) {
	if value == nil {
		return nil, nil
	}
	if *value < 1 {
		return nil, fmt.Errorf("must be greater than or equal to 1 when provided")
	}
	resolved := *value
	return &resolved, nil
}

func NormalizeOutputTokenReserve(value *int) (int, error) {
	return NormalizeOutputTokenReserveWithFallback(value, DefaultOutputTokenReserve)
}

func NormalizeOutputTokenReserveWithFallback(value *int, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	if *value < 1 {
		return 0, fmt.Errorf("must be greater than or equal to 1 when provided")
	}
	return *value, nil
}

func NormalizeMaxContextUtilization(value *float64) (float64, error) {
	return NormalizeMaxContextUtilizationWithFallback(value, DefaultMaxContextUtilization)
}

func NormalizeMaxContextUtilizationWithFallback(value *float64, fallback float64) (float64, error) {
	if value == nil {
		return fallback, nil
	}
	if *value <= 0 || *value > 1 {
		return 0, fmt.Errorf("must be greater than 0 and less than or equal to 1 when provided")
	}
	return *value, nil
}

func CopyIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	resolved := *value
	return &resolved
}

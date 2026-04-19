package loadbalance

import (
	"bytes"
	"encoding/json"
)

type ImportedStrategyDocument struct {
	Name               string
	StrategyType       string
	LegacyStrategyType *string
	AutoRecovery       json.RawMessage
	RoutingPolicy      json.RawMessage
}

type CanonicalImportedStrategy struct {
	Name               string
	StrategyType       string
	LegacyStrategyType *string
	AutoRecoveryJSON   []byte
	RoutingPolicyJSON  []byte
}

func CanonicalizeImportedStrategyDocument(document ImportedStrategyDocument) (CanonicalImportedStrategy, error) {
	request := loadbalanceStrategyRequest{
		Name:               document.Name,
		StrategyType:       document.StrategyType,
		LegacyStrategyType: document.LegacyStrategyType,
	}

	if payload := bytes.TrimSpace(document.AutoRecovery); len(payload) > 0 && !bytes.Equal(payload, []byte("null")) {
		var input autoRecoveryInput
		if err := json.Unmarshal(payload, &input); err != nil {
			return CanonicalImportedStrategy{}, err
		}
		request.AutoRecovery = &input
	}
	if payload := bytes.TrimSpace(document.RoutingPolicy); len(payload) > 0 && !bytes.Equal(payload, []byte("null")) {
		var input routingPolicyInput
		if err := json.Unmarshal(payload, &input); err != nil {
			return CanonicalImportedStrategy{}, err
		}
		request.RoutingPolicy = &input
	}

	payload, err := canonicalizeStrategyRequest(request)
	if err != nil {
		return CanonicalImportedStrategy{}, err
	}

	return CanonicalImportedStrategy{
		Name:               payload.Name,
		StrategyType:       payload.StrategyType,
		LegacyStrategyType: payload.LegacyStrategyType,
		AutoRecoveryJSON:   cloneBytes(payload.AutoRecoveryJSON),
		RoutingPolicyJSON:  cloneBytes(payload.RoutingPolicyJSON),
	}, nil
}

package auth

import (
	"context"
	"strconv"
	"strings"
)

type RealtimeAuthState struct {
	AuthEnabled   bool
	Username      string
	Authenticated bool
}

type RuntimeProxyKeySnapshot struct {
	ID   int
	Name string
}

func (s *Service) ResolveRealtimeAuthState(ctx context.Context, rawToken string) (RealtimeAuthState, error) {
	settingsRow, err := s.loadOrCreateAppAuthSettings(ctx, s.pool)
	if err != nil {
		return RealtimeAuthState{}, err
	}

	state := RealtimeAuthState{
		AuthEnabled:   settingsRow.AuthEnabled,
		Username:      stringValue(settingsRow.Username),
		Authenticated: !settingsRow.AuthEnabled,
	}
	if !settingsRow.AuthEnabled {
		return state, nil
	}

	trimmedToken := strings.TrimSpace(rawToken)
	if trimmedToken == "" {
		return state, nil
	}
	claims, err := parseAccessToken(s.nowUTC(), s.authJWTSecret, trimmedToken)
	if err != nil {
		return state, nil
	}

	subjectID, err := strconv.Atoi(strings.TrimSpace(claims.Sub))
	if err != nil {
		return state, nil
	}
	if subjectID != settingsRow.ID || claims.TokenVersion != settingsRow.TokenVersion {
		return state, nil
	}

	state.Authenticated = true
	if claims.Username != "" {
		state.Username = claims.Username
	}
	return state, nil
}

func RuntimeProxyKeyFromContext(ctx context.Context) (*RuntimeProxyKeySnapshot, bool) {
	proxyKeyValue := ctx.Value(runtimeProxyKeyContextKey{})
	proxyKey, ok := proxyKeyValue.(runtimeProxyKey)
	if !ok {
		return nil, false
	}
	return &RuntimeProxyKeySnapshot{ID: proxyKey.ID, Name: proxyKey.Name}, true
}

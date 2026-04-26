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

func (s *Service) ResolveRealtimeAuthState(ctx context.Context, rawToken string) (RealtimeAuthState, error) {
	snapshot, err := s.loadAppAuthSettingsSnapshot(ctx)
	if err != nil {
		return RealtimeAuthState{}, err
	}

	state := RealtimeAuthState{
		AuthEnabled:   snapshot.AuthEnabled,
		Username:      snapshot.Username,
		Authenticated: !snapshot.AuthEnabled,
	}
	if !snapshot.AuthEnabled {
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
	if subjectID != snapshot.ID || claims.TokenVersion != snapshot.TokenVersion {
		return state, nil
	}

	state.Authenticated = true
	if claims.Username != "" {
		state.Username = claims.Username
	}
	return state, nil
}

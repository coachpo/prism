package auth

import (
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

type RuntimeAuthConfigSnapshot struct {
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	AccessCookieName  string
	RefreshCookieName string
	CookieSecure      bool
}

type RuntimeAuthConfigProvider interface {
	AuthRuntimeConfigSnapshot() RuntimeAuthConfigSnapshot
}

func runtimeAuthConfigSnapshotFromSettings(settings config.Settings) RuntimeAuthConfigSnapshot {
	return RuntimeAuthConfigSnapshot{
		AccessTokenTTL:    time.Duration(settings.AuthAccessTokenTTLSeconds) * time.Second,
		RefreshTokenTTL:   time.Duration(settings.AuthRefreshTokenTTLSeconds) * time.Second,
		AccessCookieName:  strings.TrimSpace(settings.AuthCookieName),
		RefreshCookieName: strings.TrimSpace(settings.AuthRefreshCookieName),
		CookieSecure:      settings.AuthCookieSecure,
	}
}

func (s *Service) runtimeAuthConfigSnapshot() RuntimeAuthConfigSnapshot {
	if s == nil {
		return RuntimeAuthConfigSnapshot{}
	}
	if s.authRuntimeConfigProvider != nil {
		return s.authRuntimeConfigProvider.AuthRuntimeConfigSnapshot()
	}
	return s.staticAuthRuntimeConfig
}

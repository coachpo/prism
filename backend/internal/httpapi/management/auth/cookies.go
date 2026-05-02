package auth

import (
	"net/http"
	"time"
)

func (s *Service) setAuthCookies(
	w http.ResponseWriter,
	authConfig RuntimeAuthConfigSnapshot,
	accessToken string,
	refreshToken string,
	refreshExpiresAt time.Time,
	duration sessionDuration,
) {
	now := s.nowUTC()
	accessCookie := &http.Cookie{
		Name:     authConfig.AccessCookieName,
		Value:    accessToken,
		HttpOnly: true,
		Secure:   authConfig.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	}
	if maxAge := duration.accessCookieMaxAge(authConfig.AccessTokenTTL); maxAge > 0 {
		accessCookie.MaxAge = maxAge
	}

	refreshCookie := &http.Cookie{
		Name:     authConfig.RefreshCookieName,
		Value:    refreshToken,
		HttpOnly: true,
		Secure:   authConfig.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	}
	if maxAge := duration.refreshCookieMaxAge(now, refreshExpiresAt); maxAge > 0 {
		refreshCookie.MaxAge = maxAge
	}

	http.SetCookie(w, accessCookie)
	http.SetCookie(w, refreshCookie)
}

func (s *Service) clearAuthCookies(w http.ResponseWriter, authConfig RuntimeAuthConfigSnapshot) {
	expiresAt := time.Unix(0, 0).UTC()
	http.SetCookie(w, &http.Cookie{
		Name:     authConfig.AccessCookieName,
		Value:    "",
		HttpOnly: true,
		Secure:   authConfig.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   -1,
		Expires:  expiresAt,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     authConfig.RefreshCookieName,
		Value:    "",
		HttpOnly: true,
		Secure:   authConfig.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   -1,
		Expires:  expiresAt,
	})
}

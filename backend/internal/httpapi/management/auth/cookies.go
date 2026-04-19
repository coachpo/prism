package auth

import (
	"net/http"
	"time"
)

func (s *Service) setAuthCookies(
	w http.ResponseWriter,
	accessToken string,
	refreshToken string,
	refreshExpiresAt time.Time,
	duration sessionDuration,
) {
	now := s.nowUTC()
	accessCookie := &http.Cookie{
		Name:     s.accessCookieName,
		Value:    accessToken,
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	}
	if maxAge := duration.accessCookieMaxAge(s.accessTokenTTL); maxAge > 0 {
		accessCookie.MaxAge = maxAge
	}

	refreshCookie := &http.Cookie{
		Name:     s.refreshCookieName,
		Value:    refreshToken,
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	}
	if maxAge := duration.refreshCookieMaxAge(now, refreshExpiresAt); maxAge > 0 {
		refreshCookie.MaxAge = maxAge
	}

	http.SetCookie(w, accessCookie)
	http.SetCookie(w, refreshCookie)
}

func (s *Service) clearAuthCookies(w http.ResponseWriter) {
	expiresAt := time.Unix(0, 0).UTC()
	http.SetCookie(w, &http.Cookie{
		Name:     s.accessCookieName,
		Value:    "",
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   -1,
		Expires:  expiresAt,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     s.refreshCookieName,
		Value:    "",
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   -1,
		Expires:  expiresAt,
	})
}

package auth

import (
	"net/http"
	"strings"
)

func (s *Service) AccessTokenFromRequest(request *http.Request) string {
	authConfig := s.runtimeAuthConfigSnapshot()
	cookie, err := request.Cookie(authConfig.AccessCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

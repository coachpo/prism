package models

import (
	"errors"
	"net/url"
	"strings"
)

func normalizeExportBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(trimmed, "#") || parsed.Opaque != "" {
		return "", errors.New("invalid Prism gateway origin")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("Prism gateway origin must not include a path")
	}
	parsed.Path, parsed.RawPath = "", ""
	parsed.ForceQuery = false
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

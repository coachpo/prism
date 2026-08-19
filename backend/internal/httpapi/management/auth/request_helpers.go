package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
)

func decodeJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	decoder := json.NewDecoder(request.Body)
	return decoder.Decode(target)
}

func decodeStrictJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return responseutil.SanitizeDecodeError(err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

func setNoStoreHeaders(w http.ResponseWriter) {
	// Create/rotate responses carry the one-time raw key: they must not be
	// cached by a reverse proxy, service worker or browser.
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
}

package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
)

func decodeJSONBody(request *http.Request, target any) error {
	body, err := readJSONBody(request)
	if err != nil {
		return err
	}
	return decodeJSONBytes(body, target)
}

func decodeModelCreateRequest(request *http.Request) (modelCreateRequest, error) {
	body, err := readJSONBody(request)
	if err != nil {
		return modelCreateRequest{}, err
	}
	var target modelCreateRequest
	if err := decodeJSONBytes(body, &target); err != nil {
		return modelCreateRequest{}, err
	}
	return target, nil
}

func decodeModelUpdateRequest(request *http.Request) (modelUpdateRequest, error) {
	body, err := readJSONBody(request)
	if err != nil {
		return modelUpdateRequest{}, err
	}
	var target modelUpdateRequest
	if err := decodeJSONBytes(body, &target); err != nil {
		return modelUpdateRequest{}, err
	}
	return target, nil
}

func decodeAccessTargetCreateRequest(request *http.Request) (modelAccessTargetCreateRequest, error) {
	body, err := readJSONBody(request)
	if err != nil {
		return modelAccessTargetCreateRequest{}, err
	}
	if err := rejectObsoleteAccessTargetFields(body, ""); err != nil {
		return modelAccessTargetCreateRequest{}, err
	}
	var target modelAccessTargetCreateRequest
	if err := decodeJSONBytes(body, &target); err != nil {
		return modelAccessTargetCreateRequest{}, err
	}
	return target, nil
}

func decodeAccessTargetUpdateRequest(request *http.Request) (modelAccessTargetUpdateRequest, error) {
	body, err := readJSONBody(request)
	if err != nil {
		return modelAccessTargetUpdateRequest{}, err
	}
	if err := rejectObsoleteAccessTargetFields(body, ""); err != nil {
		return modelAccessTargetUpdateRequest{}, err
	}
	var target modelAccessTargetUpdateRequest
	if err := decodeJSONBytes(body, &target); err != nil {
		return modelAccessTargetUpdateRequest{}, err
	}
	return target, nil
}

func readJSONBody(request *http.Request) ([]byte, error) {
	defer func() { _ = request.Body.Close() }()
	return io.ReadAll(request.Body)
}

func decodeJSONBytes(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return responseutil.SanitizeDecodeError(err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain exactly one JSON value")
		}
		return err
	}
	return nil
}

func rejectObsoleteAccessTargetFields(body []byte, pathPrefix string) error {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	for _, obsoleteField := range []string{"weight", "target_priority"} {
		if _, ok := payload[obsoleteField]; ok {
			path := pathPrefix + obsoleteField
			detail := fmt.Sprintf("%s is obsolete and must be omitted", path)
			return routingPlanValidationIssueError("obsolete_access_target_field", path, detail)
		}
	}
	return nil
}

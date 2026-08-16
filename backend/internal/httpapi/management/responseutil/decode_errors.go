package responseutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// unknownFieldPattern matches the encoding/json "unknown field" error text so
// the field name can be surfaced without leaking Go type names.
var unknownFieldPattern = regexp.MustCompile(`json: unknown field "([^"]+)"`)

// unknownFieldName extracts the offending field name from an encoding/json
// unknown-field error. encoding/json does not export a typed error for this
// case, so the extraction matches the stable error text.
func unknownFieldName(err error) (string, bool) {
	match := unknownFieldPattern.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}

// SanitizeDecodeError maps encoding/json failures to client-facing text that
// never leaks Go struct or type names.
func SanitizeDecodeError(err error) error {
	if err == nil {
		return nil
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return fmt.Errorf("invalid type for field %q", typeErr.Field)
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return errors.New("malformed JSON body")
	}
	if field, ok := unknownFieldName(err); ok {
		return fmt.Errorf("unknown field %q", field)
	}
	if errors.Is(err, io.EOF) {
		return errors.New("empty request body")
	}
	return errors.New("invalid request body")
}

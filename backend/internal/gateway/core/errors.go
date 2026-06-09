package core

import (
	"fmt"
	"sort"
	"strings"
)

type ErrorType string

const (
	ErrorTypeValidation     ErrorType = "validation"
	ErrorTypeClassification ErrorType = "classification"
	ErrorTypeConfig         ErrorType = "config"
	ErrorTypeRouting        ErrorType = "routing"
	ErrorTypeAdmission      ErrorType = "admission"
	ErrorTypeUpstream       ErrorType = "upstream"
	ErrorTypeInternal       ErrorType = "internal"
)

type FieldError struct {
	Field  string `json:"field"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type GatewayError struct {
	Type       ErrorType    `json:"type"`
	Code       string       `json:"code"`
	Detail     string       `json:"detail"`
	StatusCode int          `json:"status_code,omitempty"`
	Fields     []FieldError `json:"fields,omitempty"`
}

func NewGatewayError(errorType ErrorType, code string, detail string, statusCode int, fields ...FieldError) *GatewayError {
	return &GatewayError{
		Type:       errorType,
		Code:       strings.TrimSpace(code),
		Detail:     strings.TrimSpace(detail),
		StatusCode: statusCode,
		Fields:     sortedFieldErrors(fields),
	}
}

func NewConfigError(code string, detail string, fields ...FieldError) *GatewayError {
	return NewGatewayError(ErrorTypeConfig, code, detail, 0, fields...)
}

func (err *GatewayError) Error() string {
	if err == nil {
		return ""
	}
	if strings.TrimSpace(err.Code) == "" {
		return err.Detail
	}
	if strings.TrimSpace(err.Detail) == "" {
		return err.Code
	}
	return fmt.Sprintf("%s: %s", err.Code, err.Detail)
}

func sortedFieldErrors(fields []FieldError) []FieldError {
	if len(fields) == 0 {
		return nil
	}
	clone := append([]FieldError(nil), fields...)
	sort.Slice(clone, func(i, j int) bool {
		if clone[i].Field != clone[j].Field {
			return clone[i].Field < clone[j].Field
		}
		if clone[i].Code != clone[j].Code {
			return clone[i].Code < clone[j].Code
		}
		return clone[i].Detail < clone[j].Detail
	})
	return clone
}

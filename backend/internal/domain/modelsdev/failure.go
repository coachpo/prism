package modelsdev

import (
	"context"
	"errors"
	"net"
)

var ErrCatalogFormat = errors.New("invalid catalog format")
var ErrCatalogTooLarge = errors.New("catalog size limit exceeded")

// FailureCode publishes only a bounded category, never transport URLs, response
// bodies, revision headers or credentials from an untrusted catalog response.
func FailureCode(err error) string {
	if err == nil {
		return ""
	}
	var network net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.As(err, &network) && network.Timeout():
		return "timeout"
	case errors.Is(err, ErrCatalogFormat):
		return "format"
	case errors.Is(err, ErrCatalogTooLarge):
		return "too_large"
	default:
		return "unavailable"
	}
}

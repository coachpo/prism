package routing

import (
	"context"
	"errors"
	"net/http"
	"slices"
)

import gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"

type RetryFailureClass string

const (
	RetryFailureNone           RetryFailureClass = "none"
	RetryFailureProvider429    RetryFailureClass = "provider_429"
	RetryFailureProvider5xx    RetryFailureClass = "provider_5xx"
	RetryFailureConnectTimeout RetryFailureClass = "connect_timeout"
)

type RetryPolicy struct {
	FailoverStatusCodes []int
}

type RetryDecision struct {
	Class     RetryFailureClass
	Retryable bool
	Reason    gatewaycore.RouteReason
}

func (policy RetryPolicy) ClassifyHTTPStatus(statusCode int) RetryDecision {
	if statusCode == http.StatusTooManyRequests {
		return retryDecision(RetryFailureProvider429, gatewaycore.RouteReasonRetry429)
	}
	if statusCode >= http.StatusInternalServerError && statusCode <= 599 && slices.Contains(policy.FailoverStatusCodes, statusCode) {
		return retryDecision(RetryFailureProvider5xx, gatewaycore.RouteReasonRetry5xx)
	}
	return RetryDecision{Class: RetryFailureNone}
}

func (policy RetryPolicy) ClassifyTransportError(err error) RetryDecision {
	if isRetryableConnectTimeout(err) {
		return retryDecision(RetryFailureConnectTimeout, gatewaycore.RouteReasonRetryConnectTimeout)
	}
	return RetryDecision{Class: RetryFailureNone}
}

func retryDecision(class RetryFailureClass, reason gatewaycore.RouteReason) RetryDecision {
	return RetryDecision{Class: class, Retryable: true, Reason: reason}
}

type timeoutError interface {
	Timeout() bool
}

func isRetryableConnectTimeout(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var timeout timeoutError
	return errors.As(err, &timeout) && timeout.Timeout()
}

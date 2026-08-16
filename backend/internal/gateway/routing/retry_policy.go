package routing

import (
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
	RetryFailureProviderHTTP   RetryFailureClass = "provider_http"
	RetryFailureConnectTimeout RetryFailureClass = "connect_timeout"
	RetryFailureTransport      RetryFailureClass = "transport"
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
	if !slices.Contains(policy.FailoverStatusCodes, statusCode) {
		return RetryDecision{Class: RetryFailureNone}
	}
	if statusCode == http.StatusTooManyRequests {
		return retryDecision(RetryFailureProvider429, gatewaycore.RouteReasonRetry429)
	}
	if statusCode >= http.StatusInternalServerError && statusCode <= 599 {
		return retryDecision(RetryFailureProvider5xx, gatewaycore.RouteReasonRetry5xx)
	}
	return retryDecision(RetryFailureProviderHTTP, gatewaycore.RouteReasonRetryHTTP)
}

func (policy RetryPolicy) ClassifyTransportError(requestContextErr error, err error) RetryDecision {
	if err == nil || requestContextErr != nil {
		return RetryDecision{Class: RetryFailureNone}
	}
	if isRetryableConnectTimeout(err) {
		return retryDecision(RetryFailureConnectTimeout, gatewaycore.RouteReasonRetryConnectTimeout)
	}
	return retryDecision(RetryFailureTransport, gatewaycore.RouteReasonRetryTransport)
}

func retryDecision(class RetryFailureClass, reason gatewaycore.RouteReason) RetryDecision {
	return RetryDecision{Class: class, Retryable: true, Reason: reason}
}

type timeoutError interface {
	Timeout() bool
}

func isRetryableConnectTimeout(err error) bool {
	if err == nil {
		return false
	}
	var timeout timeoutError
	return errors.As(err, &timeout) && timeout.Timeout()
}

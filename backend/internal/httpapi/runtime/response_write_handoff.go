package runtime

import "net/http"

func (s *Service) runtimeResponseRequiresDurableHandoff(execution executionResult) bool {
	return s != nil && s.requireDurableSuccessHandoff && execution.Response != nil && execution.Response.StatusCode >= http.StatusOK && execution.Response.StatusCode <= 299
}

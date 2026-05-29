package platformhttp

import "testing"

func TestTelemetryRouteTemplatesAreBounded(t *testing.T) {
	testCases := []struct {
		name   string
		branch string
		method string
		path   string
		want   string
	}{
		{name: "management known template", branch: "management", method: "PATCH", path: "/api/settings/auth/proxy-keys/123", want: "/api/settings/auth/proxy-keys/{key_id}"},
		{name: "management unknown bucket", branch: "management", method: "GET", path: "/api/unplanned/123", want: "/api/*"},
		{name: "runtime known template", branch: "runtime", method: "POST", path: "/v1beta/models/gemini-pro:generateContent", want: "/v1beta/models/{model}:generateContent"},
		{name: "runtime wrong method template", branch: "runtime", method: "GET", path: "/v1/chat/completions", want: "/v1/chat/completions"},
		{name: "runtime unsupported bucket", branch: "runtime", method: "POST", path: "/v1/unsupported/123", want: "/v1/*"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var got string
			switch testCase.branch {
			case "management":
				got = managementTelemetryRouteTemplate(testCase.method, testCase.path)
			case "runtime":
				got = runtimeTelemetryRouteTemplate(testCase.method, testCase.path)
			default:
				t.Fatalf("unknown test branch %q", testCase.branch)
			}
			if got != testCase.want {
				t.Fatalf("route template = %q want %q", got, testCase.want)
			}
		})
	}
}

package startup

import "github.com/coachpo/prism/backend/internal/vendordomain"

type VendorDefinition = vendordomain.VendorDefinition

type HeaderBlocklistRuleDefinition struct {
	Name      string
	MatchType string
	Pattern   string
}

type UserAgentClientRuleDefinition struct {
	Name    string
	Pattern string
}

var DefaultVendors = append([]VendorDefinition(nil), vendordomain.SystemVendorDefinitions...)

var systemVendorByKey = func() map[string]VendorDefinition {
	items := make(map[string]VendorDefinition, len(DefaultVendors))
	for _, definition := range DefaultVendors {
		items[definition.Key] = definition
	}
	return items
}()

var SystemHeaderBlocklistDefaults = []HeaderBlocklistRuleDefinition{
	{Name: "Cloudflare headers", MatchType: "prefix", Pattern: "cf-"},
	{Name: "Cloudflare extended headers", MatchType: "prefix", Pattern: "x-cf-"},
	{Name: "Cloudflare Access headers", MatchType: "prefix", Pattern: "cf-access-"},
	{Name: "B3 tracing headers", MatchType: "prefix", Pattern: "x-b3-"},
	{Name: "Datadog tracing headers", MatchType: "prefix", Pattern: "x-datadog-"},
	{Name: "CDN loop detection", MatchType: "exact", Pattern: "cdn-loop"},
	{Name: "Forwarded header", MatchType: "exact", Pattern: "forwarded"},
	{Name: "Via header", MatchType: "exact", Pattern: "via"},
	{Name: "X-Forwarded-For", MatchType: "exact", Pattern: "x-forwarded-for"},
	{Name: "X-Forwarded-Host", MatchType: "exact", Pattern: "x-forwarded-host"},
	{Name: "X-Forwarded-Port", MatchType: "exact", Pattern: "x-forwarded-port"},
	{Name: "X-Forwarded-Proto", MatchType: "exact", Pattern: "x-forwarded-proto"},
	{Name: "X-Real-IP", MatchType: "exact", Pattern: "x-real-ip"},
	{Name: "True-Client-IP", MatchType: "exact", Pattern: "true-client-ip"},
	{Name: "W3C Traceparent", MatchType: "exact", Pattern: "traceparent"},
	{Name: "W3C Tracestate", MatchType: "exact", Pattern: "tracestate"},
	{Name: "W3C Baggage", MatchType: "exact", Pattern: "baggage"},
	{Name: "X-Request-ID", MatchType: "exact", Pattern: "x-request-id"},
	{Name: "X-Correlation-ID", MatchType: "exact", Pattern: "x-correlation-id"},
	{Name: "AWS X-Ray trace", MatchType: "exact", Pattern: "x-amzn-trace-id"},
	{Name: "GCP Cloud Trace", MatchType: "exact", Pattern: "x-cloud-trace-context"},
}

var SystemUserAgentClientRuleDefaults = []UserAgentClientRuleDefinition{
	{Name: "Opencode", Pattern: "opencode"},
	{Name: "Codex", Pattern: "codex"},
	{Name: "Claude Code", Pattern: "claude(?:\\s|-)?(?:code|cli)"},
	{Name: "Gemini", Pattern: "gemini"},
	{Name: "Python", Pattern: "python"},
	{Name: "Curl", Pattern: "curl"},
}

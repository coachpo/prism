// Package providercompat owns Prism's internal provider/API-family compatibility rules.
//
// Responsibilities:
// - keep the supported provider API-family and auth-type allowlists in one place;
// - normalize OpenAI probe endpoint variants for management and configbundle callers while preserving their distinct external error contracts;
// - derive the OpenAI upstream operation recorded by management responses, runtime plans, request logs, and usage events;
// - select provider auth/header profiles and their protected header names for runtime proxying and management health checks;
// - build provider-native health probe request paths and bodies for OpenAI, Anthropic, and Gemini;
// - provide same-family compatibility predicates used by management, configbundle import, and runtime planning.
package providercompat

// Package providercompat owns Prism's internal provider/API-family compatibility rules.
//
// Responsibilities:
// - keep the supported provider API-family and auth-type allowlists in one place;
// - derive the OpenAI upstream operation recorded by management responses, runtime plans, request logs, and usage events;
// - select provider auth/header profiles and their protected header names for runtime proxying;
// - provide same-family compatibility predicates used by management and runtime planning.
package providercompat

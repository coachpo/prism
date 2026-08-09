package runtimetest

// runtimeProxyAPIKeyRecord is the harness record for an issued proxy key
// (permissive attribution tests and cache-invalidation tests).
type runtimeProxyAPIKeyRecord struct {
	ID     int
	Name   string
	RawKey string
}

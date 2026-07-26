// Package registry holds registry TLS settings.
package registry

// Registry holds registry connection security settings.
type Registry struct {
	// TLSSkip disables TLS verification for registry connections.
	TLSSkip bool
	// TLSMinVersion is the minimum TLS version for registry connections.
	TLSMinVersion string
}

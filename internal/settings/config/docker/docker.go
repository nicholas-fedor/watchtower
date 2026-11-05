package docker

// Docker holds Docker daemon connection settings.
type Docker struct {
	// Host is the Docker daemon socket or host URL.
	Host string
	// APIVersion is the Docker API version, or empty for negotiation.
	APIVersion string
	// CertPath is the path to TLS certificates.
	CertPath string
	// TLSVerify enables TLS verification for remote daemons.
	TLSVerify bool
}

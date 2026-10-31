package meta

var (
	// Version is the version of Watchtower, set at link time. It is
	// v0.0.0-unknown when the build does not set it.
	Version = "v0.0.0-unknown"

	// UserAgent is the HTTP client identifier derived from Version.
	UserAgent string
)

func init() {
	UserAgent = "Watchtower/" + Version
}

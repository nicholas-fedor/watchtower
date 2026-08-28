package registry

// Persona is a fake public-registry dialect served inside DinD.
type Persona string

const (
	// PersonaNone disables hostname hijack.
	PersonaNone Persona = "none"
	// PersonaHub speaks Docker Hub challenge, token, and RateLimit headers.
	PersonaHub Persona = "hub"
	// PersonaGHCR speaks GHCR bearer challenge and body quota 429.
	PersonaGHCR Persona = "ghcr"
	// PersonaLSCR serves lscr.io so Watchtower's ghcr.io swap still hits a fake.
	PersonaLSCR Persona = "lscr"
	// PersonaPrivate is an arbitrary host with optional htpasswd.
	PersonaPrivate Persona = "private"
)

// HostsFor returns extra_hosts names that must resolve to the persona proxy.
//
// Parameters:
//   - persona: Selected dialect.
//
// Returns:
//   - []string: DNS names Watchtower and dockerd will use.
func HostsFor(persona Persona) []string {
	switch persona {
	case PersonaHub:
		return []string{"index.docker.io", "registry-1.docker.io", "auth.docker.io", "docker.io"}
	case PersonaGHCR:
		return []string{"ghcr.io"}
	case PersonaLSCR:
		return []string{"lscr.io", "ghcr.io"}
	case PersonaNone, PersonaPrivate:
		return nil
	default:
		return nil
	}
}

// HubRateLimitLimit is the Hub RateLimit-Limit header Watchtower parses.
const HubRateLimitLimit = "100;w=21600"

// HubRateLimitRemaining is a remaining-quota header for Hub 429 fixtures.
const HubRateLimitRemaining = "0"

// HubRetryAfterSeconds is the Retry-After delta used in Hub 429 fixtures.
const HubRetryAfterSeconds = "1"

// GHCRTooManyRequests is the GHCR 429 body Watchtower's ratelimit parser accepts.
const GHCRTooManyRequests = "toomanyrequests: retry-after: 200ms, allowed: 44000/minute"

// QuotaNo429Body is a throttle message with retry-after and allowed, but no 429 token.
const QuotaNo429Body = "error from registry: retry-after: 200ms, allowed: 44000/minute"

// HubWWWAuthenticate is a Bearer challenge for Docker Hub.
const HubWWWAuthenticate = `Bearer realm="http://auth.docker.io/token",service="registry.docker.io",scope="repository:e2e/app:pull"`

// GHCRWWWAuthenticate is a Bearer challenge for GHCR.
const GHCRWWWAuthenticate = `Bearer realm="http://ghcr.io/token",service="ghcr.io",scope="repository:e2e/app:pull"`

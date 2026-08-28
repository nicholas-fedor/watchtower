package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const (
	// slowHeadDelay is how long FaultSlowHead sleeps on HEAD.
	slowHeadDelay = 3 * time.Second
	// issuedAccess is the fixture bearer token returned by /token.
	issuedAccess = "e2e-registry-fixture"
	// controlPathPrefix is the in-band control API path.
	controlPathPrefix = "/e2e-control/"
	// maxControlBody is the max JSON size accepted by /e2e-control/fault.
	maxControlBody = 1 << 20
)

// Proxy is an HTTP handler that speaks persona dialects and proxies to a backend registry.
type Proxy struct {
	// persona is the Hub, GHCR, LSCR, or private dialect.
	persona Persona
	// backend is the distribution registry URL.
	backend *url.URL
	// control holds the live fault plan.
	control *Controller
	// director reverse-proxies blob and manifest traffic.
	director *httputil.ReverseProxy
}

// NewProxy constructs a persona reverse proxy.
//
// Parameters:
//   - persona: Dialect to speak on the public hostname.
//   - backend: Distribution registry base URL (for example http://127.0.0.1:5000).
//   - control: Fault controller. Must not be nil.
//
// Returns:
//   - *Proxy: Handler ready for http.Server or httptest.
//   - error: Backend URL parse error.
func NewProxy(persona Persona, backend string, control *Controller) (*Proxy, error) {
	parsed, err := url.Parse(backend)
	if err != nil {
		return nil, fmt.Errorf("persona backend url: %w", err)
	}

	if control == nil {
		control = NewController()
	}

	proxy := &Proxy{
		persona: persona,
		backend: parsed,
		control: control,
	}
	proxy.director = &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(parsed)
			req.Out.Host = parsed.Host
		},
	}

	return proxy, nil
}

// ServeHTTP implements http.Handler.
//
// Control paths, token, and /v2/ challenges are handled here. Everything else
// is reverse-proxied to the backend registry after optional fault injection.
//
// Parameters:
//   - writer: HTTP response.
//   - request: Incoming request.
func (p *Proxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if strings.HasPrefix(request.URL.Path, controlPathPrefix) {
		p.serveControl(writer, request)

		return
	}

	if request.URL.Path == "/token" || strings.HasPrefix(request.URL.Path, "/token") {
		p.serveToken(writer, request)

		return
	}

	if request.URL.Path == "/v2/" || request.URL.Path == "/v2" {
		p.serveChallenge(writer, request)

		return
	}

	fault := p.control.Trip()
	if p.applyFault(writer, request, fault) {
		return
	}

	p.director.ServeHTTP(writer, request)
}

// serveChallenge answers GET /v2/ with 401 and a persona WWW-Authenticate header.
//
// Parameters:
//   - writer: HTTP response.
//   - request: Incoming request, used for Bearer validation and Host.
func (p *Proxy) serveChallenge(writer http.ResponseWriter, request *http.Request) {
	auth := request.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
		token := after
		if p.control.TokenValid(token) {
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("{}"))

			return
		}
	}

	switch p.persona {
	case PersonaHub:
		writer.Header().Set("WWW-Authenticate", HubWWWAuthenticate)
	case PersonaGHCR, PersonaLSCR:
		writer.Header().Set("WWW-Authenticate", GHCRWWWAuthenticate)
	case PersonaNone, PersonaPrivate:
		writer.Header().Set("WWW-Authenticate", `Bearer realm="http://`+request.Host+`/token",service="e2e"`)
	default:
		writer.Header().Set("WWW-Authenticate", `Bearer realm="http://`+request.Host+`/token",service="e2e"`)
	}

	writer.WriteHeader(http.StatusUnauthorized)
}

// serveToken issues a fixture bearer token for Hub and GHCR token endpoints.
//
// Parameters:
//   - writer: HTTP response.
func (p *Proxy) serveToken(writer http.ResponseWriter, _ *http.Request) {
	p.control.IssueToken(issuedAccess)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte(`{"token":"` + issuedAccess + `","access_token":"` + issuedAccess + `"}`))
}

// applyFault writes a programmed registry failure, or returns false to proxy.
//
// Parameters:
//   - writer: HTTP response.
//   - request: Incoming request (used for slow HEAD).
//   - fault: Fault to apply on this request.
//
// Returns:
//   - bool: True when the handler already wrote a response.
func (p *Proxy) applyFault(writer http.ResponseWriter, request *http.Request, fault Fault) bool {
	switch fault {
	case FaultNone:
		return false
	case FaultHub429:
		writer.Header().Set("Retry-After", HubRetryAfterSeconds)
		writer.Header().Set("Ratelimit-Limit", HubRateLimitLimit)
		writer.Header().Set("Ratelimit-Remaining", HubRateLimitRemaining)
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte("toomanyrequests"))

		return true
	case FaultGHCR429:
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(GHCRTooManyRequests))

		return true
	case FaultUnauthorized:
		p.serveChallenge(writer, request)

		return true
	case FaultForbidden:
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte("denied"))

		return true
	case FaultSlowHead:
		if request.Method == http.MethodHead {
			time.Sleep(slowHeadDelay)
		}

		return false
	case FaultServerError:
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte("upstream reset"))

		return true
	case FaultQuotaNo429:
		writer.Header().Set("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(QuotaNo429Body))

		return true
	case FaultExpireToken:
		return false
	default:
		return false
	}
}

// controlBody is the JSON posted to /e2e-control/fault.
type controlBody struct {
	// Fault is the fault kind to arm.
	Fault string `json:"fault"`
	// After is how many requests to allow before the fault trips.
	After int `json:"after"`
}

// serveControl handles /e2e-control/fault and /e2e-control/health.
//
// Parameters:
//   - writer: HTTP response.
//   - request: Incoming control request.
func (p *Proxy) serveControl(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case controlPathPrefix + "fault":
		if request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusMethodNotAllowed)

			return
		}

		defer request.Body.Close()

		limited := io.LimitReader(request.Body, maxControlBody)

		raw, err := io.ReadAll(limited)
		if err != nil {
			writer.WriteHeader(http.StatusBadRequest)

			return
		}

		var body controlBody

		unmarshalErr := json.Unmarshal(raw, &body)
		if unmarshalErr != nil {
			writer.WriteHeader(http.StatusBadRequest)

			return
		}

		p.control.SetFault(Fault(body.Fault), body.After)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("armed"))
	case controlPathPrefix + "health":
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	default:
		writer.WriteHeader(http.StatusNotFound)
	}
}

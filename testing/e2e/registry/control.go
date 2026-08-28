package registry

import "sync"

// Fault is a programmable registry failure injected after N requests.
type Fault string

const (
	// FaultNone injects nothing.
	FaultNone Fault = "none"
	// FaultHub429 returns Hub rate-limit headers.
	FaultHub429 Fault = "429-hub"
	// FaultGHCR429 returns the GHCR quota body.
	FaultGHCR429 Fault = "429-ghcr"
	// FaultUnauthorized returns 401.
	FaultUnauthorized Fault = "401"
	// FaultForbidden returns 403.
	FaultForbidden Fault = "403"
	// FaultExpireToken rejects the next token after it was issued.
	FaultExpireToken Fault = "expire-token"
	// FaultSlowHead delays HEAD manifest requests.
	FaultSlowHead Fault = "slow-head"
	// FaultServerError returns 502 on blob/manifest GET.
	FaultServerError Fault = "5xx"
	// FaultQuotaNo429 returns a GHCR-style quota body without 429 or toomanyrequests.
	FaultQuotaNo429 Fault = "quota-no-429"
)

// Controller holds the live fault plan for one proxy.
type Controller struct {
	mu sync.Mutex

	// fault is the armed fault kind.
	fault Fault
	// after is how many requests pass before the fault trips.
	after int
	// seen is how many Trip calls have run since SetFault.
	seen int
	// tokens are bearer tokens issued by /token.
	tokens map[string]bool
}

// NewController returns an idle controller.
//
// Returns:
//   - *Controller: Zero-fault controller.
func NewController() *Controller {
	return &Controller{
		fault:  FaultNone,
		tokens: make(map[string]bool),
	}
}

// SetFault arms a fault after N matching requests.
//
// Parameters:
//   - fault: Fault kind.
//   - after: Requests to allow before the fault trips.
func (c *Controller) SetFault(fault Fault, after int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.fault = fault
	c.after = after
	c.seen = 0
}

// Trip increments the request counter and reports the active fault, if any.
//
// Returns:
//   - Fault: Fault to apply on this request, or FaultNone.
func (c *Controller) Trip() Fault {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.fault == FaultNone {
		return FaultNone
	}

	c.seen++
	if c.seen <= c.after {
		return FaultNone
	}

	return c.fault
}

// IssueToken records a bearer token as valid.
//
// Parameters:
//   - token: Opaque token string.
func (c *Controller) IssueToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tokens[token] = true
}

// TokenValid reports whether the bearer token is still accepted.
//
// Parameters:
//   - token: Presented token.
//
// Returns:
//   - bool: True when the token is known and not expired.
func (c *Controller) TokenValid(token string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.fault == FaultExpireToken && c.seen > c.after {
		return false
	}

	return c.tokens[token]
}

// Snapshot returns the current fault plan.
//
// Returns:
//   - Fault: Configured fault.
//   - int: Trip threshold.
func (c *Controller) Snapshot() (Fault, int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.fault, c.after
}

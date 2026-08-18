package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// hostState holds the cooldown and token-bucket budget for one registry host.
type hostState struct {
	// cooldownUntil is when the next request to this host may proceed.
	cooldownUntil time.Time
	// allowed is the advertised request budget. Zero when unknown.
	allowed int
	// window is the period for allowed. Zero when unknown.
	window time.Duration
	// tokens is the remaining budget in the current window.
	tokens float64
	// lastRefill is when tokens were last replenished.
	lastRefill time.Time
}

var (
	hostsMu sync.Mutex
	hosts   = map[string]*hostState{}
)

// ResetForTest clears per-host cooldown and quota state.
//
// Tests call this so one case cannot leak a cooldown into the next.
func ResetForTest() {
	hostsMu.Lock()
	defer hostsMu.Unlock()

	hosts = map[string]*hostState{}
}

// Observe records a 429 against host so later Wait calls honor it.
//
// Parameters:
//   - host: Registry host. Empty values are ignored.
//   - info: Parsed 429. Nil values are ignored.
func Observe(host string, info *Error) {
	if host == "" || info == nil {
		return
	}

	hostsMu.Lock()
	defer hostsMu.Unlock()

	state := hostLocked(host)

	cooldown := min(info.RetryAfter, maxHonorWait)

	if cooldown > 0 && cooldown < minHonorWait {
		cooldown = minHonorWait
	}

	if cooldown > 0 {
		until := time.Now().Add(cooldown)
		if until.After(state.cooldownUntil) {
			state.cooldownUntil = until
		}
	}

	if info.Allowed > 0 && info.AllowedWindow > 0 {
		state.allowed = info.Allowed
		state.window = info.AllowedWindow
		state.tokens = 0
		state.lastRefill = time.Now()
	}
}

// ObserveSuccess refreshes host's advertised budget after a successful request.
//
// Token reservation happens in Wait. This only refills so a success does not
// spend a second token.
//
// Parameters:
//   - host: Registry host. Empty values are ignored.
func ObserveSuccess(host string) {
	if host == "" {
		return
	}

	hostsMu.Lock()
	defer hostsMu.Unlock()

	state := hosts[host]
	if state == nil || state.allowed <= 0 || state.window <= 0 {
		return
	}

	state.refillLocked(time.Now())
}

// Wait blocks until host is outside its 429 cooldown and has budget.
//
// Parameters:
//   - ctx: Context that can cancel the wait.
//   - host: Registry host. Empty hosts return immediately.
//
// Returns:
//   - error: ctx.Err() when canceled. Nil when the caller may proceed.
func Wait(ctx context.Context, host string) error {
	if host == "" {
		return nil
	}

	for {
		wait := nextWait(host)
		if wait <= 0 {
			return nil
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()

			return fmt.Errorf("registry rate-limit wait canceled: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// WaitCooldown blocks until host is outside its 429 cooldown.
//
// Unlike Wait, this does not reserve a quota token. Callers that already
// passed Wait use it to recheck cooldown after a lock or slot handoff.
//
// Parameters:
//   - ctx: Context that can cancel the wait.
//   - host: Registry host. Empty hosts return immediately.
//
// Returns:
//   - error: ctx.Err() when canceled. Nil when the caller may proceed.
func WaitCooldown(ctx context.Context, host string) error {
	if host == "" {
		return nil
	}

	for {
		wait := nextCooldownWait(host)
		if wait <= 0 {
			return nil
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()

			return fmt.Errorf("registry rate-limit wait canceled: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// nextWait computes how long the caller must wait before contacting host.
//
// Parameters:
//   - host: Registry host whose cooldown and quota should be inspected.
//
// Returns:
//   - time.Duration: Sleep before the next request. Zero when the caller may proceed.
func nextWait(host string) time.Duration {
	hostsMu.Lock()
	defer hostsMu.Unlock()

	state := hostLocked(host)
	now := time.Now()

	if now.Before(state.cooldownUntil) {
		return time.Until(state.cooldownUntil)
	}

	if state.allowed <= 0 || state.window <= 0 {
		return 0
	}

	state.refillLocked(now)

	if state.tokens >= 1 {
		state.tokens--

		return 0
	}

	perToken := max(state.window/time.Duration(state.allowed), time.Millisecond)

	return perToken
}

// nextCooldownWait returns remaining 429 cooldown for host without taking a token.
//
// Parameters:
//   - host: Registry host whose cooldown should be inspected.
//
// Returns:
//   - time.Duration: Sleep before the next request. Zero when there is no cooldown.
func nextCooldownWait(host string) time.Duration {
	hostsMu.Lock()
	defer hostsMu.Unlock()

	state := hosts[host]
	if state == nil || !time.Now().Before(state.cooldownUntil) {
		return 0
	}

	return time.Until(state.cooldownUntil)
}

// hostLocked returns the per-host limiter state, creating it when missing.
//
// The caller must hold hostsMu.
//
// Parameters:
//   - host: Registry host key.
//
// Returns:
//   - *hostState: Existing or newly created state for host.
func hostLocked(host string) *hostState {
	state := hosts[host]
	if state == nil {
		state = &hostState{lastRefill: time.Now()}
		hosts[host] = state
	}

	return state
}

// refillLocked adds tokens earned since the last refill.
//
// The caller must hold hostsMu.
//
// Parameters:
//   - now: Current time used to compute earned tokens.
func (s *hostState) refillLocked(now time.Time) {
	if s.allowed <= 0 || s.window <= 0 {
		return
	}

	if s.lastRefill.IsZero() {
		s.lastRefill = now
		s.tokens = float64(s.allowed)

		return
	}

	elapsed := now.Sub(s.lastRefill)
	if elapsed <= 0 {
		return
	}

	s.tokens += float64(elapsed) / float64(s.window) * float64(s.allowed)
	if s.tokens > float64(s.allowed) {
		s.tokens = float64(s.allowed)
	}

	s.lastRefill = now
}

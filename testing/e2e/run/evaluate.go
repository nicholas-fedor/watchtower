package run

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nicholas-fedor/watchtower/testing/e2e/assert"
	"github.com/nicholas-fedor/watchtower/testing/e2e/docker"
	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

// evaluate checks Watchtower behavior against Expect after a session.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Assertion failure.
func (s *session) evaluate(ctx context.Context) error {
	secretErr := assert.ForbiddenSecrets(s.logs, s.item.Watchtower.SecretValues())
	if secretErr != nil {
		return fmt.Errorf("secret scan: %w", secretErr)
	}

	outcomeErr := s.assertOutcome()
	if outcomeErr != nil {
		return outcomeErr
	}

	cleanupErr := s.assertCleanup(ctx)
	if cleanupErr != nil {
		return cleanupErr
	}

	macErr := s.assertMACAddress(ctx)
	if macErr != nil {
		return macErr
	}

	apiErr := s.assertHTTPDetails()
	if apiErr != nil {
		return apiErr
	}

	return s.assertPorcelain()
}

// assertOutcome checks image-id change, fidelity, extra env, depends-on, and rate-limit logs.
//
// Returns:
//   - error: Outcome mismatch.
func (s *session) assertOutcome() error {
	imageChanged := s.before.ImageID != "" && s.after.ImageID != s.before.ImageID

	switch s.item.Expect.Outcome {
	case engine.OutcomeUpdated:
		if !imageChanged {
			if watchtowerScannedNone(s.logs) {
				return fmt.Errorf("%w (subject image %q)", ErrWatchtowerSawNoContainers, s.before.ImageRef)
			}

			if pullDenied(s.logs) {
				return fmt.Errorf("%w for %s", errRegistryPullDenied, s.before.ImageRef)
			}

			return ErrImageIDUnchanged
		}

		fidErr := assert.DiffFidelity(s.before, s.after)
		if fidErr != nil {
			return fmt.Errorf("fidelity: %w", fidErr)
		}

		joined := strings.Join(s.after.Env, "\n")
		for _, extra := range s.item.Topology.ExtraEnv {
			if !strings.Contains(joined, extra) {
				return fmt.Errorf("%w: %q", errExtraEnvMissing, extra)
			}
		}

		if s.item.Topology.Graph == engine.GraphChain4 || s.item.Topology.Graph == engine.GraphComposeDepends {
			orderErr := assert.AssertDependencyOrder(assert.ParseSession(s.logs), s.subjects.DependencyOrder)
			if orderErr != nil {
				return fmt.Errorf("depends-on: %w", orderErr)
			}
		}
	case engine.OutcomeNoUpdate, engine.OutcomeRateLimited, engine.OutcomeAuthFail, engine.OutcomeBlocked:
		if imageChanged {
			return assert.ErrUnexpectedUpdate
		}

		if s.item.Expect.Outcome == engine.OutcomeRateLimited && !rateLimitLogged(s.logs) {
			return errNoRateLimitLog
		}
	case engine.OutcomeRejectConfig, engine.OutcomeTimeout, engine.OutcomeOOM, engine.OutcomeKilled, engine.OutcomeCrash, engine.OutcomeLeftover:
	}

	return nil
}

// rateLimitLogged reports whether logs show a pull or quota failure.
//
// Parameters:
//   - logs: Combined stdout and stderr.
//
// Returns:
//   - bool: True when a rate-limit or pull-fail phrase is present.
func watchtowerScannedNone(logs string) bool {
	return strings.Contains(logs, `"scanned":0`) || strings.Contains(logs, `"scanned": 0`)
}

func pullDenied(logs string) bool {
	return strings.Contains(logs, "Failed to pull image") ||
		strings.Contains(logs, "connect: connection refused") ||
		strings.Contains(logs, `Head "http://127.0.0.1:5000`)
}

func rateLimitLogged(logs string) bool {
	lower := strings.ToLower(logs)

	return strings.Contains(lower, "rate limited") ||
		strings.Contains(lower, "retry-after") ||
		strings.Contains(lower, "toomanyrequests") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "failed to pull")
}

// assertCleanup fails when --cleanup left the previous image ID.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Leftover image.
func (s *session) assertCleanup(ctx context.Context) error {
	if s.item.Watchtower.Cleanup == nil || !*s.item.Watchtower.Cleanup || s.item.Expect.Outcome != engine.OutcomeUpdated {
		return nil
	}

	return assertImageGone(ctx, s.opts.Daemon.Client(), s.before.ImageID)
}

// assertMACAddress fails when a 02:42 engine MAC was copied onto a recreated subject.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Preserved MAC.
func (s *session) assertMACAddress(ctx context.Context) error {
	if s.item.Topology.SubjectCount <= 1 {
		return nil
	}

	for _, name := range s.subjects.DependencyOrder {
		_, snap, macErr := docker.InspectSnapshot(ctx, s.opts.Daemon.Client(), name)
		if macErr != nil {
			return fmt.Errorf("inspect %s: %w", name, macErr)
		}

		if strings.HasPrefix(snap.ConfigMAC, "02:42:") {
			return errPreservedMAC
		}
	}

	return nil
}

// assertHTTPDetails checks /v1/containers/details when that endpoint is enabled.
//
// Returns:
//   - error: Contract failure.
func (s *session) assertHTTPDetails() error {
	if s.item.Watchtower.HTTPAPIContainers == nil || !*s.item.Watchtower.HTTPAPIContainers {
		return nil
	}

	enabledErr := docker.DetailsEnabledTrue(s.details, s.subjects.PrimaryName)
	if enabledErr != nil {
		return fmt.Errorf("containers/details: %w", enabledErr)
	}

	return nil
}

// assertPorcelain checks porcelain JSON when the case requested it.
//
// Returns:
//   - error: Missing updated record.
func (s *session) assertPorcelain() error {
	raw := s.stdoutBuf.Bytes()
	if len(raw) == 0 && s.caseDir != "" {
		raw, _ = os.ReadFile(filepath.Join(s.caseDir, "watchtower.stdout.jsonl"))
	}

	if len(raw) > 0 && s.item.Watchtower.Porcelain != nil && *s.item.Watchtower.Porcelain == "json" {
		if s.caseDir != "" {
			_ = os.WriteFile(filepath.Join(s.caseDir, "porcelain.json"), raw, permFile) //nolint:gosec // G703: caseDir is created by the harness.
		}

		parsed, parseErr := assert.ParsePorcelain(raw)
		if parseErr == nil && s.item.Expect.Outcome == engine.OutcomeUpdated {
			updErr := assert.RequireUpdated(parsed)
			if updErr != nil {
				return fmt.Errorf("porcelain: %w", updErr)
			}
		}
	}

	return nil
}

package engine

import "strings"

// DeriveExpect infers the session outcome from the vector when File does not override it.
//
// Parameters:
//   - item: Fully applied case.
//
// Returns:
//   - Expect: Predicted outcome and leak secrets.
func DeriveExpect(item Case) Expect {
	expect := Expect{
		Outcome: OutcomeUpdated,
		Secrets: item.Watchtower.SecretValues(),
	}

	if item.Shape == ShapeIntervalSchedule ||
		(item.Watchtower.Interval != nil && item.Watchtower.Schedule != nil && *item.Watchtower.Schedule != "") {
		expect.Outcome = OutcomeRejectConfig
		expect.RejectReason = "interval and schedule cannot both be set"

		return expect
	}

	if item.Watchtower.DiskSpaceWarn != nil && strings.Contains(*item.Watchtower.DiskSpaceWarn, "%") &&
		item.Watchtower.DiskSpaceMax == nil {
		expect.Outcome = OutcomeRejectConfig
		expect.RejectReason = "disk-space-warn percent requires disk-space-max"

		return expect
	}

	if item.Watchtower.MonitorOnly != nil && *item.Watchtower.MonitorOnly {
		expect.Outcome = OutcomeNoUpdate

		return expect
	}

	if item.Topology.DigestPinned {
		expect.Outcome = OutcomeNoUpdate

		return expect
	}

	if item.Watchtower.CooldownDelay != nil && *item.Watchtower.CooldownDelay != "" &&
		*item.Watchtower.CooldownDelay != "0" && item.Topology.ImageCreatedAge == 0 {
		expect.Outcome = OutcomeNoUpdate

		return expect
	}

	switch item.Topology.RegistryFault {
	case "401", "403":
		expect.Outcome = OutcomeAuthFail
	case "429-hub", "429-ghcr":
		expect.Outcome = OutcomeRateLimited
	case "slow-head":
		expect.Outcome = OutcomeTimeout
	}

	if item.Topology.SubjectKind == "slow-term" || item.Topology.SubjectKind == "deaf-term" {
		if item.Watchtower.StopTimeout != nil {
			expect.TimeoutAtLeast = *item.Watchtower.StopTimeout
		}
	}

	if item.Topology.SubjectKind == "deaf-term" {
		expect.Outcome = OutcomeKilled
	}

	if item.Topology.WatchtowerEnvelope.MemoryBytes > 0 && item.Topology.WatchtowerEnvelope.MemoryBytes < 32<<20 {
		expect.Outcome = OutcomeOOM
	}

	if item.Topology.HTTPQuery.Image != "" || item.Topology.HTTPQuery.Container != "" {
		if item.Shape == ShapeHTTPUpdate || item.Shape == ShapeHTTPUpdatePeriodic {
			expect.HTTPStatus = []int{200, 202}
		}
	}

	return expect
}

// Unrealizable is true when the topology cannot be built (not a count budget).
//
// Registry personas require Watchtower inside DinD so extra_hosts apply.
// Binary packaging plus a public-registry persona would resolve live DNS.
//
// Parameters:
//   - item: Fully applied case.
//
// Returns:
//   - bool: True when the scheduler must skip the case.
func Unrealizable(item Case) bool {
	persona := item.Topology.RegistryPersona
	if persona != "" && persona != "none" && item.Packaging == PackagingBinary {
		return true
	}

	if item.Topology.DockerTransport == "remote" && item.Packaging == PackagingBinary {
		return true
	}

	return false
}

package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// LevelUnset is the factor level that leaves a field at the Watchtower default.
const LevelUnset = "unset"

// Outcome is the expected session result for a case.
type Outcome string

const (
	// OutcomeUpdated means subjects were recreated onto a newer image.
	OutcomeUpdated Outcome = "updated"
	// OutcomeNoUpdate means Watchtower ran and left subjects in place.
	OutcomeNoUpdate Outcome = "no-update"
	// OutcomeRejectConfig means Watchtower exits because the vector is illegal.
	OutcomeRejectConfig Outcome = "reject-config"
	// OutcomeBlocked means disk-space-max or a similar gate refused the session.
	OutcomeBlocked Outcome = "blocked"
	// OutcomeAuthFail means registry or HTTP API authentication failed.
	OutcomeAuthFail Outcome = "auth-fail"
	// OutcomeTimeout means a stop, lifecycle, or HTTP deadline fired.
	OutcomeTimeout Outcome = "timeout"
	// OutcomeOOM means a memory envelope killed a process.
	OutcomeOOM Outcome = "oom"
	// OutcomeKilled means SIGKILL after a deaf stop-timeout.
	OutcomeKilled Outcome = "killed"
	// OutcomeCrash means Watchtower exited unexpectedly.
	OutcomeCrash Outcome = "crash"
	// OutcomeLeftover means a documented leftover (orphan orchestrator) remains.
	OutcomeLeftover Outcome = "leftover"
	// OutcomeRateLimited means the persona returned 429 and Watchtower honored it.
	OutcomeRateLimited Outcome = "rate-limited"
)

// Packaging selects how Watchtower is launched against the inner daemon.
type Packaging string

const (
	// PackagingContainer runs Watchtower as a container inside DinD.
	PackagingContainer Packaging = "container"
	// PackagingBinary runs the host-built binary with DOCKER_HOST at the inner daemon.
	PackagingBinary Packaging = "binary"
)

// ConfigChannel selects how WatchtowerConfig is delivered to the process.
type ConfigChannel string

const (
	// ChannelFlags renders CLI arguments only.
	ChannelFlags ConfigChannel = "flags"
	// ChannelEnv renders WATCHTOWER_* / DOCKER_* environment only.
	ChannelEnv ConfigChannel = "env"
	// ChannelMixed splits fields across argv and env using a stable partition.
	ChannelMixed ConfigChannel = "mixed"
	// ChannelSecretFile places tokens and notification URLs in secret files.
	ChannelSecretFile ConfigChannel = "secret-file"
)

// ProcessShape is the mutex over Watchtower's process entry shape.
type ProcessShape string

const (
	// ShapeRunOnce is --run-once.
	ShapeRunOnce ProcessShape = "run-once"
	// ShapeInterval is a short --interval poll loop.
	ShapeInterval ProcessShape = "interval"
	// ShapeSchedule is --schedule cron / @every.
	ShapeSchedule ProcessShape = "schedule"
	// ShapeIntervalSchedule sets both interval and schedule (illegal).
	ShapeIntervalSchedule ProcessShape = "interval+schedule"
	// ShapeHTTPUpdate is HTTP API update without periodic polls.
	ShapeHTTPUpdate ProcessShape = "http-update"
	// ShapeHTTPUpdatePeriodic is HTTP API update plus periodic polls.
	ShapeHTTPUpdatePeriodic ProcessShape = "http-update+periodic"
)

// GraphKind is a dependency-graph topology class.
type GraphKind string

const (
	// GraphNone is a single subject with no depends-on edges.
	GraphNone GraphKind = "none"
	// GraphChain4 is a four-node depends-on chain A<-B<-C<-D.
	GraphChain4 GraphKind = "chain-4"
	// GraphCycle is a depends-on cycle that must not hang.
	GraphCycle GraphKind = "cycle"
	// GraphComposeDepends uses Compose depends_on labels plus Watchtower labels.
	GraphComposeDepends GraphKind = "compose-depends"
)

// Case is one full configuration vector: Watchtower settings plus topology.
type Case struct {
	// Factors records the assigned level for every Model() factor.
	Factors map[string]string `json:"factors" yaml:"factors"`
	// Watchtower is the typed flag/env vector. Unset pointer fields mean default.
	Watchtower WatchtowerConfig `json:"watchtower" yaml:"watchtower"`
	// Topology is the inner daemon fixture.
	Topology Topology `json:"topology" yaml:"topology"`
	// Expect is the predicted outcome.
	Expect Expect `json:"expect" yaml:"expect"`
	// Packaging is container or binary.
	Packaging Packaging `json:"packaging" yaml:"packaging"`
	// Channel is how the config is delivered.
	Channel ConfigChannel `json:"channel" yaml:"channel"`
	// Shape is the process-shape mutex.
	Shape ProcessShape `json:"shape" yaml:"shape"`
	// Names are positional container name filters.
	Names []string `json:"names,omitempty" yaml:"names,omitempty"`
	// ImageSource is thin or self-local for container packaging.
	ImageSource string `json:"image_source,omitempty" yaml:"image_source,omitempty"`

	id string
}

// ID returns a stable hash of the factor vector so shards and resume are deterministic.
//
// Returns:
//   - string: Canonical case identifier.
func (c *Case) ID() string {
	if c.id != "" {
		return c.id
	}

	return c.computeID()
}

// AssignID stores the computed identifier on the case.
func (c *Case) AssignID() {
	c.id = c.computeID()
}

// computeID hashes the sorted factor assignments.
//
// Returns:
//   - string: Hex prefix of SHA-256 over name=level lines.
func (c *Case) computeID() string {
	keys := make([]string, 0, len(c.Factors))
	for name := range c.Factors {
		keys = append(keys, name)
	}

	sort.Strings(keys)

	var builder strings.Builder
	for _, name := range keys {
		builder.WriteString(name)
		builder.WriteByte('=')
		builder.WriteString(c.Factors[name])
		builder.WriteByte('\n')
	}

	sum := sha256.Sum256([]byte(builder.String()))
	short := hex.EncodeToString(sum[:8])

	shape := string(c.Shape)
	if shape == "" {
		shape = "na"
	}

	pack := string(c.Packaging)
	if pack == "" {
		pack = "na"
	}

	kind := c.Topology.SubjectKind
	if kind == "" {
		kind = "na"
	}

	return fmt.Sprintf("%s_%s_%s_%s", shape, pack, kind, short)
}

// Factor is one cartesian axis: a name, discrete levels, and an apply function.
type Factor struct {
	// Name is the stable factor key used in IDs and --dump-factors.
	Name string
	// Levels are discrete values, including unset where the default matters.
	Levels []string
	// Apply writes the level onto the case.
	Apply func(c *Case, level string)
	// Flag is the Watchtower long flag this factor covers, if any.
	Flag string
}

// BoolPtr returns a pointer to v.
//
// Parameters:
//   - value: Boolean to address.
//
// Returns:
//   - *bool: Pointer to value.
//
//go:fix inline
func BoolPtr(value bool) *bool {
	return new(value)
}

// StringPtr returns a pointer to v.
//
// Parameters:
//   - value: String to address.
//
// Returns:
//   - *string: Pointer to value.
//
//go:fix inline
func StringPtr(value string) *string {
	return new(value)
}

// IntPtr returns a pointer to v.
//
// Parameters:
//   - value: Integer to address.
//
// Returns:
//   - *int: Pointer to value.
//
//go:fix inline
func IntPtr(value int) *int {
	return new(value)
}

// DurationPtr returns a pointer to v.
//
// Parameters:
//   - value: Duration to address.
//
// Returns:
//   - *time.Duration: Pointer to value.
//
//go:fix inline
func DurationPtr(value time.Duration) *time.Duration {
	return new(value)
}

// StringsPtr returns a pointer to a string slice copy.
//
// Parameters:
//   - values: Slice to address.
//
// Returns:
//   - *[]string: Pointer to a copy of values.
func StringsPtr(values ...string) *[]string {
	copied := append([]string{}, values...)

	return &copied
}

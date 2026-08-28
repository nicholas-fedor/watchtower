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

// Envelope is a calibrated CPU/memory/pids budget for a container.
type Envelope struct {
	// MemoryBytes is HostConfig.Memory. Zero means unset.
	MemoryBytes int64 `json:"memory_bytes,omitempty"`
	// NanoCPUs is HostConfig.NanoCPUs. Zero means unset.
	NanoCPUs int64 `json:"nano_cpus,omitempty"`
	// PidsLimit is HostConfig.PidsLimit. Zero means unset.
	PidsLimit int64 `json:"pids_limit,omitempty"`
}

// LifecycleTopo is per-container lifecycle hook labels and timeouts.
type LifecycleTopo struct {
	// PreCheck is the pre-check command label value.
	PreCheck string `json:"pre_check,omitempty"`
	// PostCheck is the post-check command label value.
	PostCheck string `json:"post_check,omitempty"`
	// PreUpdate is the pre-update command label value.
	PreUpdate string `json:"pre_update,omitempty"`
	// PostUpdate is the post-update command label value.
	PostUpdate string `json:"post_update,omitempty"`
	// PreTimeout is the pre-update-timeout label (minutes, or 0 unlimited).
	PreTimeout string `json:"pre_timeout,omitempty"`
	// UID is the per-container lifecycle UID label.
	UID string `json:"uid,omitempty"`
	// GID is the per-container lifecycle GID label.
	GID string `json:"gid,omitempty"`
}

// HTTPQuery is HTTP API ?image= / ?container= intersection with instance filters.
type HTTPQuery struct {
	// Image is the ?image= regex or literal.
	Image string `json:"image,omitempty"`
	// Container is the ?container= regex or literal.
	Container string `json:"container,omitempty"`
}

// Topology is the inner-daemon fixture around Watchtower for one case.
type Topology struct {
	// SubjectKind selects the subject family member (echo, slow-term, ...).
	SubjectKind string `json:"subject_kind,omitempty"`
	// SubjectState is the Docker runtime state before Watchtower starts.
	SubjectState string `json:"subject_state,omitempty"`
	// SubjectCount is how many primary subjects to create. Zero means one.
	SubjectCount int `json:"subject_count,omitempty"`
	// Graph is the depends-on topology.
	Graph GraphKind `json:"graph,omitempty"`
	// Networks are extra inner networks attached to subjects (class A multi-net).
	Networks []string `json:"networks,omitempty"`
	// NetworkMode is Docker network_mode (bridge, host, none, container:, service:).
	NetworkMode string `json:"network_mode,omitempty"`
	// DigestPinned tags the subject with repo@sha256:... so it must not update.
	DigestPinned bool `json:"digest_pinned,omitempty"`
	// RegistryPersona is hub, ghcr, lscr, private, or none.
	RegistryPersona string `json:"registry_persona,omitempty"`
	// RegistryFault is none, 429-hub, 429-ghcr, 401, 403, expire-token, slow-head, 5xx.
	RegistryFault string `json:"registry_fault,omitempty"`
	// DockerTransport is unix, tcp, tcp-tls, or remote.
	DockerTransport string `json:"docker_transport,omitempty"`
	// HostEnvelope applies to the outer DinD container.
	HostEnvelope Envelope `json:"host_envelope"`
	// WatchtowerEnvelope applies to the Watchtower container.
	WatchtowerEnvelope Envelope `json:"watchtower_envelope"`
	// SubjectEnvelope applies to subject containers.
	SubjectEnvelope Envelope `json:"subject_envelope"`
	// Labels are extra container labels on the primary subject.
	Labels map[string]string `json:"labels,omitempty"`
	// StopSignal is com.centurylinklabs.watchtower.stop-signal.
	StopSignal string `json:"stop_signal,omitempty"`
	// Lifecycle is hook labels on the primary subject.
	Lifecycle LifecycleTopo `json:"lifecycle"`
	// HTTPQuery is API filter intersection.
	HTTPQuery HTTPQuery `json:"http_query"`
	// NotifySink is none, webhook, or webhook-5xx.
	NotifySink string `json:"notify_sink,omitempty"`
	// RemoteDocker is true when Watchtower in worker A talks to worker B.
	RemoteDocker bool `json:"remote_docker,omitempty"`
	// ImageCreatedAge is the fake registry Image Created offset for cooldown.
	ImageCreatedAge time.Duration `json:"image_created_age,omitempty"`
	// Mirror points the inner daemon at a second in-DinD persona.
	Mirror bool `json:"mirror,omitempty"`
	// SharedImage shares one image ID across N containers for --cleanup.
	SharedImage bool `json:"shared_image,omitempty"`
	// Decoy starts an unenlisted container that must not move.
	Decoy bool `json:"decoy,omitempty"`
	// EnableLabel is the com.centurylinklabs.watchtower.enable value.
	EnableLabel string `json:"enable_label,omitempty"`
	// ScopeLabel is the com.centurylinklabs.watchtower.scope value.
	ScopeLabel string `json:"scope_label,omitempty"`
	// MonitorOnlyLabel is the per-container monitor-only label.
	MonitorOnlyLabel string `json:"monitor_only_label,omitempty"`
	// NoPullLabel is the per-container no-pull label.
	NoPullLabel string `json:"no_pull_label,omitempty"`
	// CooldownLabel is the per-container cooldown-delay label.
	CooldownLabel string `json:"cooldown_label,omitempty"`
	// NetDelay is tc netem / toxiproxy latency in front of the persona.
	NetDelay time.Duration `json:"net_delay,omitempty"`
	// RestartPolicy is the subject's Docker restart policy.
	RestartPolicy string `json:"restart_policy,omitempty"`
	// ExtraEnv is extra environment on the subject (fidelity).
	ExtraEnv []string `json:"extra_env,omitempty"`
}

// Expect is the derived or File-overridden outcome for a case.
type Expect struct {
	// Outcome is the primary expected result.
	Outcome Outcome `json:"outcome"`
	// RejectReason documents why OutcomeRejectConfig was chosen.
	RejectReason string `json:"reject_reason,omitempty"`
	// HTTPStatus are acceptable HTTP API statuses (for example 202 and 429).
	HTTPStatus []int `json:"http_status,omitempty"`
	// Secrets are values that must never appear in logs, porcelain, or /v1/config.
	Secrets []string `json:"secrets,omitempty"`
	// TimeoutAtLeast is the minimum observed stop duration for timeout cases.
	TimeoutAtLeast time.Duration `json:"timeout_at_least,omitempty"`
}

// Case is one full configuration vector: Watchtower settings plus topology.
type Case struct {
	// Factors records the assigned level for every Model() factor.
	Factors map[string]string `json:"factors"`
	// Watchtower is the typed flag/env vector. Unset pointer fields mean default.
	Watchtower WatchtowerConfig `json:"watchtower"`
	// Topology is the inner daemon fixture.
	Topology Topology `json:"topology"`
	// Expect is the predicted outcome.
	Expect Expect `json:"expect"`
	// Packaging is container or binary.
	Packaging Packaging `json:"packaging"`
	// Channel is how the config is delivered.
	Channel ConfigChannel `json:"channel"`
	// Shape is the process-shape mutex.
	Shape ProcessShape `json:"shape"`
	// Names are positional container name filters.
	Names []string `json:"names,omitempty"`
	// ImageSource is thin or self-local for container packaging.
	ImageSource string `json:"image_source,omitempty"`

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

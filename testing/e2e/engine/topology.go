package engine

import "time"

// Envelope is a calibrated CPU/memory/pids budget for a container.
type Envelope struct {
	// MemoryBytes is HostConfig.Memory. Zero means unset.
	MemoryBytes int64 `json:"memory_bytes,omitempty" yaml:"memory_bytes,omitempty"`
	// NanoCPUs is HostConfig.NanoCPUs. Zero means unset.
	NanoCPUs int64 `json:"nano_cpus,omitempty" yaml:"nano_cpus,omitempty"`
	// PidsLimit is HostConfig.PidsLimit. Zero means unset.
	PidsLimit int64 `json:"pids_limit,omitempty" yaml:"pids_limit,omitempty"`
}

// LifecycleTopo is per-container lifecycle hook labels and timeouts.
type LifecycleTopo struct {
	// PreCheck is the pre-check command label value.
	PreCheck string `json:"pre_check,omitempty" yaml:"pre_check,omitempty"`
	// PostCheck is the post-check command label value.
	PostCheck string `json:"post_check,omitempty" yaml:"post_check,omitempty"`
	// PreUpdate is the pre-update command label value.
	PreUpdate string `json:"pre_update,omitempty" yaml:"pre_update,omitempty"`
	// PostUpdate is the post-update command label value.
	PostUpdate string `json:"post_update,omitempty" yaml:"post_update,omitempty"`
	// PreTimeout is the pre-update-timeout label (minutes, or 0 unlimited).
	PreTimeout string `json:"pre_timeout,omitempty" yaml:"pre_timeout,omitempty"`
	// UID is the per-container lifecycle UID label.
	UID string `json:"uid,omitempty" yaml:"uid,omitempty"`
	// GID is the per-container lifecycle GID label.
	GID string `json:"gid,omitempty" yaml:"gid,omitempty"`
}

// HTTPQuery is HTTP API ?image= / ?container= intersection with instance filters.
type HTTPQuery struct {
	// Image is the ?image= regex or literal.
	Image string `json:"image,omitempty" yaml:"image,omitempty"`
	// Container is the ?container= regex or literal.
	Container string `json:"container,omitempty" yaml:"container,omitempty"`
}

// Topology is the inner-daemon fixture around Watchtower for one case.
type Topology struct {
	// SubjectKind selects the subject family member (echo, slow-term, ...).
	SubjectKind string `json:"subject_kind,omitempty" yaml:"subject_kind,omitempty"`
	// SubjectState is the Docker runtime state before Watchtower starts.
	SubjectState string `json:"subject_state,omitempty" yaml:"subject_state,omitempty"`
	// SubjectCount is how many primary subjects to create. Zero means one.
	SubjectCount int `json:"subject_count,omitempty" yaml:"subject_count,omitempty"`
	// Graph is the depends-on topology.
	Graph GraphKind `json:"graph,omitempty" yaml:"graph,omitempty"`
	// Networks are extra inner networks attached to subjects (class A multi-net).
	Networks []string `json:"networks,omitempty" yaml:"networks,omitempty"`
	// NetworkMode is Docker network_mode (bridge, host, none, container:, service:).
	NetworkMode string `json:"network_mode,omitempty" yaml:"network_mode,omitempty"`
	// DigestPinned tags the subject with repo@sha256:... so it must not update.
	DigestPinned bool `json:"digest_pinned,omitempty" yaml:"digest_pinned,omitempty"`
	// RegistryPersona is hub, ghcr, lscr, private, or none.
	RegistryPersona string `json:"registry_persona,omitempty" yaml:"registry_persona,omitempty"`
	// RegistryFault is none, 429-hub, 429-ghcr, 401, 403, expire-token, slow-head, 5xx.
	RegistryFault string `json:"registry_fault,omitempty" yaml:"registry_fault,omitempty"`
	// DockerTransport is unix, tcp, tcp-tls, or remote.
	DockerTransport string `json:"docker_transport,omitempty" yaml:"docker_transport,omitempty"`
	// HostEnvelope applies to the outer DinD container.
	HostEnvelope Envelope `json:"host_envelope" yaml:"host_envelope"`
	// WatchtowerEnvelope applies to the Watchtower container.
	WatchtowerEnvelope Envelope `json:"watchtower_envelope" yaml:"watchtower_envelope"`
	// SubjectEnvelope applies to subject containers.
	SubjectEnvelope Envelope `json:"subject_envelope" yaml:"subject_envelope"`
	// Labels are extra container labels on the primary subject.
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	// StopSignal is com.centurylinklabs.watchtower.stop-signal.
	StopSignal string `json:"stop_signal,omitempty" yaml:"stop_signal,omitempty"`
	// Lifecycle is hook labels on the primary subject.
	Lifecycle LifecycleTopo `json:"lifecycle" yaml:"lifecycle"`
	// HTTPQuery is API filter intersection.
	HTTPQuery HTTPQuery `json:"http_query" yaml:"http_query"`
	// NotifySink is none, webhook, or webhook-5xx.
	NotifySink string `json:"notify_sink,omitempty" yaml:"notify_sink,omitempty"`
	// RemoteDocker is true when Watchtower in worker A talks to worker B.
	RemoteDocker bool `json:"remote_docker,omitempty" yaml:"remote_docker,omitempty"`
	// ImageCreatedAge is the fake registry Image Created offset for cooldown.
	ImageCreatedAge time.Duration `json:"image_created_age,omitempty" yaml:"-"`
	// Mirror points the inner daemon at a second in-DinD persona.
	Mirror bool `json:"mirror,omitempty" yaml:"mirror,omitempty"`
	// SharedImage shares one image ID across N containers for --cleanup.
	SharedImage bool `json:"shared_image,omitempty" yaml:"shared_image,omitempty"`
	// Decoy starts an unenlisted container that must not move.
	Decoy bool `json:"decoy,omitempty" yaml:"decoy,omitempty"`
	// EnableLabel is the com.centurylinklabs.watchtower.enable value.
	EnableLabel string `json:"enable_label,omitempty" yaml:"enable_label,omitempty"`
	// ScopeLabel is the com.centurylinklabs.watchtower.scope value.
	ScopeLabel string `json:"scope_label,omitempty" yaml:"scope_label,omitempty"`
	// MonitorOnlyLabel is the per-container monitor-only label.
	MonitorOnlyLabel string `json:"monitor_only_label,omitempty" yaml:"monitor_only_label,omitempty"`
	// NoPullLabel is the per-container no-pull label.
	NoPullLabel string `json:"no_pull_label,omitempty" yaml:"no_pull_label,omitempty"`
	// CooldownLabel is the per-container cooldown-delay label.
	CooldownLabel string `json:"cooldown_label,omitempty" yaml:"cooldown_label,omitempty"`
	// NetDelay is tc netem / toxiproxy latency in front of the persona.
	NetDelay time.Duration `json:"net_delay,omitempty" yaml:"-"`
	// RestartPolicy is the subject's Docker restart policy.
	RestartPolicy string `json:"restart_policy,omitempty" yaml:"restart_policy,omitempty"`
	// ExtraEnv is extra environment on the subject (fidelity).
	ExtraEnv []string `json:"extra_env,omitempty" yaml:"extra_env,omitempty"`
}

// Expect is the derived or File-overridden outcome for a case.
type Expect struct {
	// Outcome is the primary expected result.
	Outcome Outcome `json:"outcome" yaml:"outcome"`
	// RejectReason documents why OutcomeRejectConfig was chosen.
	RejectReason string `json:"reject_reason,omitempty" yaml:"reject_reason,omitempty"`
	// HTTPStatus are acceptable HTTP API statuses (for example 202 and 429).
	HTTPStatus []int `json:"http_status,omitempty" yaml:"http_status,omitempty"`
	// Secrets are values that must never appear in logs, porcelain, or /v1/config.
	Secrets []string `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	// TimeoutAtLeast is the minimum observed stop duration for timeout cases.
	TimeoutAtLeast time.Duration `json:"timeout_at_least,omitempty" yaml:"-"`
}

package engine

import (
	"fmt"
	"os"
	"time"

	"go.yaml.in/yaml/v3"
)

// fileCase is the YAML shape for named regressions under testdata/cases/.
type fileCase struct {
	ID          string            `yaml:"id"`
	Packaging   string            `yaml:"packaging"`
	Channel     string            `yaml:"channel"`
	Shape       string            `yaml:"shape"`
	ImageSource string            `yaml:"image_source"`
	Watchtower  fileWatchtower    `yaml:"watchtower"`
	Topology    fileTopology      `yaml:"topology"`
	Expect      fileExpect        `yaml:"expect"`
	Factors     map[string]string `yaml:"factors"`
	Names       []string          `yaml:"names"`
}

// fileWatchtower is the YAML watchtower: block.
type fileWatchtower struct {
	Host       *string `yaml:"host"`
	TLSVerify  *bool   `yaml:"tlsverify"`
	APIVersion *string `yaml:"api_version"`
	CertPath   *string `yaml:"cert_path"`

	IncludeStopped    *bool   `yaml:"include_stopped"`
	IncludeRestarting *bool   `yaml:"include_restarting"`
	ReviveStopped     *bool   `yaml:"revive_stopped"`
	RemoveVolumes     *bool   `yaml:"remove_volumes"`
	WarnOnHeadFailure *string `yaml:"warn_on_head_failure"`

	Interval      *int    `yaml:"interval"`
	Schedule      *string `yaml:"schedule"`
	UpdateOnStart *bool   `yaml:"update_on_start"`

	RunOnce                *bool   `yaml:"run_once"`
	HealthCheck            *bool   `yaml:"health_check"`
	Porcelain              *string `yaml:"porcelain"`
	NoStartupMessage       *bool   `yaml:"no_startup_message"`
	SelfUpdateOrchestrator *bool   `yaml:"self_update_orchestrator"`

	Cleanup             *bool   `yaml:"cleanup"`
	NoPull              *bool   `yaml:"no_pull"`
	NoRestart           *bool   `yaml:"no_restart"`
	MonitorOnly         *bool   `yaml:"monitor_only"`
	RollingRestart      *bool   `yaml:"rolling_restart"`
	StopTimeout         *string `yaml:"stop_timeout"`
	CooldownDelay       *string `yaml:"cooldown_delay"`
	UseComposeDependsOn *bool   `yaml:"use_compose_depends_on"`
	LabelTakePrecedence *bool   `yaml:"label_take_precedence"`
	EphemeralSelfUpdate *bool   `yaml:"ephemeral_self_update"`
	DiskSpaceMax        *string `yaml:"disk_space_max"`
	DiskSpaceWarn       *string `yaml:"disk_space_warn"`

	EnableLifecycleHooks *bool `yaml:"enable_lifecycle_hooks"`
	LifecycleUID         *int  `yaml:"lifecycle_uid"`
	LifecycleGID         *int  `yaml:"lifecycle_gid"`

	LabelEnable              *bool    `yaml:"label_enable"`
	DisableContainers        []string `yaml:"disable_containers"`
	MonitorImageNames        []string `yaml:"monitor_image_names"`
	SkipImageNames           []string `yaml:"skip_image_names"`
	EnableContainersByLabel  []string `yaml:"enable_containers_by_label"`
	DisableContainersByLabel []string `yaml:"disable_containers_by_label"`
	Scope                    *string  `yaml:"scope"`

	RegistryTLSSkip       *bool   `yaml:"registry_tls_skip"`
	RegistryTLSMinVersion *string `yaml:"registry_tls_min_version"`

	DisableMemorySwappiness *bool   `yaml:"disable_memory_swappiness"`
	CPUCopyMode             *string `yaml:"cpu_copy_mode"`

	HTTPAPIEndpoints      []string `yaml:"http_api_endpoints"`
	HTTPAPIUpdate         *bool    `yaml:"http_api_update"`
	HTTPAPIMetrics        *bool    `yaml:"http_api_metrics"`
	HTTPAPIContainers     *bool    `yaml:"http_api_containers"`
	HTTPAPIHost           *string  `yaml:"http_api_host"`
	HTTPAPIPort           *string  `yaml:"http_api_port"`
	HTTPAPIToken          *string  `yaml:"http_api_token"`
	HTTPAPIEventsToken    *string  `yaml:"http_api_events_token"`
	HTTPAPIPeriodicPolls  *bool    `yaml:"http_api_periodic_polls"`
	HTTPAPIRateLimit      *int     `yaml:"http_api_rate_limit"`
	HTTPAPITLSCert        *string  `yaml:"http_api_tls_cert"`
	HTTPAPITLSKey         *string  `yaml:"http_api_tls_key"`
	HTTPAPITrustedProxies []string `yaml:"http_api_trusted_proxies"`
	HTTPAPIProxyHeader    *string  `yaml:"http_api_proxy_header"`
	HTTPAPICORSOrigins    []string `yaml:"http_api_cors_origins"`
	HTTPAPICheckTimeout   *string  `yaml:"http_api_check_timeout"`
	HTTPAPIUpdateTimeout  *string  `yaml:"http_api_update_timeout"`

	NotificationURL              []string `yaml:"notification_url"`
	NotificationsLevel           *string  `yaml:"notifications_level"`
	NotificationsDelay           *int     `yaml:"notifications_delay"`
	NotificationsHostname        *string  `yaml:"notifications_hostname"`
	NotificationTemplate         *string  `yaml:"notification_template"`
	NotificationTemplateFile     *string  `yaml:"notification_template_file"`
	NotificationReport           *bool    `yaml:"notification_report"`
	NotificationTitleTag         *string  `yaml:"notification_title_tag"`
	NotificationSkipTitle        *bool    `yaml:"notification_skip_title"`
	NotificationLogStdout        *bool    `yaml:"notification_log_stdout"`
	NotificationSplitByContainer *bool    `yaml:"notification_split_by_container"`
	Notifications                []string `yaml:"notifications"`
	NotificationEmailFrom        *string  `yaml:"notification_email_from"`
	NotificationEmailTo          *string  `yaml:"notification_email_to"`
	NotificationEmailDelay       *int     `yaml:"notification_email_delay"`
	NotificationEmailServer      *string  `yaml:"notification_email_server"`
	NotificationEmailServerPort  *int     `yaml:"notification_email_server_port"`
	NotificationEmailTLSSkip     *bool    `yaml:"notification_email_tls_skip"`
	NotificationEmailUser        *string  `yaml:"notification_email_user"`
	NotificationEmailPassword    *string  `yaml:"notification_email_password"`
	NotificationEmailSubjectTag  *string  `yaml:"notification_email_subjecttag"`
	NotificationSlackHookURL     *string  `yaml:"notification_slack_hook_url"`
	NotificationSlackIdentifier  *string  `yaml:"notification_slack_identifier"`
	NotificationSlackChannel     *string  `yaml:"notification_slack_channel"`
	NotificationSlackIconEmoji   *string  `yaml:"notification_slack_icon_emoji"`
	NotificationSlackIconURL     *string  `yaml:"notification_slack_icon_url"`
	NotificationMSTeamsHook      *string  `yaml:"notification_msteams_hook"`
	NotificationGotifyURL        *string  `yaml:"notification_gotify_url"`
	NotificationGotifyToken      *string  `yaml:"notification_gotify_token"`
	NotificationGotifyTLSSkip    *bool    `yaml:"notification_gotify_tls_skip"`

	LogFormat *string `yaml:"log_format"`
	LogLevel  *string `yaml:"log_level"`
	Debug     *bool   `yaml:"debug"`
	Trace     *bool   `yaml:"trace"`
	NoColor   *bool   `yaml:"no_color"`
}

// fileEnvelope is the YAML CPU and memory budget block.
type fileEnvelope struct {
	MemoryBytes int64 `yaml:"memory_bytes"`
	NanoCPUs    int64 `yaml:"nano_cpus"`
	PidsLimit   int64 `yaml:"pids_limit"`
}

// fileLifecycle is the YAML lifecycle hook label block.
type fileLifecycle struct {
	PreCheck   string `yaml:"pre_check"`
	PostCheck  string `yaml:"post_check"`
	PreUpdate  string `yaml:"pre_update"`
	PostUpdate string `yaml:"post_update"`
	PreTimeout string `yaml:"pre_timeout"`
	UID        string `yaml:"uid"`
	GID        string `yaml:"gid"`
}

// fileHTTPQuery is the YAML HTTP API query filter block.
type fileHTTPQuery struct {
	Image     string `yaml:"image"`
	Container string `yaml:"container"`
}

// fileTopology is the YAML topology: block.
type fileTopology struct {
	SubjectKind        string            `yaml:"subject_kind"`
	SubjectState       string            `yaml:"subject_state"`
	SubjectCount       int               `yaml:"subject_count"`
	Graph              string            `yaml:"graph"`
	Networks           []string          `yaml:"networks"`
	NetworkMode        string            `yaml:"network_mode"`
	DigestPinned       bool              `yaml:"digest_pinned"`
	RegistryPersona    string            `yaml:"registry_persona"`
	RegistryFault      string            `yaml:"registry_fault"`
	DockerTransport    string            `yaml:"docker_transport"`
	HostEnvelope       fileEnvelope      `yaml:"host_envelope"`
	WatchtowerEnvelope fileEnvelope      `yaml:"watchtower_envelope"`
	SubjectEnvelope    fileEnvelope      `yaml:"subject_envelope"`
	Labels             map[string]string `yaml:"labels"`
	StopSignal         string            `yaml:"stop_signal"`
	Lifecycle          fileLifecycle     `yaml:"lifecycle"`
	HTTPQuery          fileHTTPQuery     `yaml:"http_query"`
	NotifySink         string            `yaml:"notify_sink"`
	RemoteDocker       bool              `yaml:"remote_docker"`
	ImageCreatedAge    string            `yaml:"image_created_age"`
	Mirror             bool              `yaml:"mirror"`
	SharedImage        bool              `yaml:"shared_image"`
	Decoy              bool              `yaml:"decoy"`
	EnableLabel        string            `yaml:"enable_label"`
	ScopeLabel         string            `yaml:"scope_label"`
	MonitorOnlyLabel   string            `yaml:"monitor_only_label"`
	NoPullLabel        string            `yaml:"no_pull_label"`
	CooldownLabel      string            `yaml:"cooldown_label"`
	NetDelay           string            `yaml:"net_delay"`
	RestartPolicy      string            `yaml:"restart_policy"`
	ExtraEnv           []string          `yaml:"extra_env"`
}

// fileExpect is the YAML expect: block.
type fileExpect struct {
	Outcome        string   `yaml:"outcome"`
	RejectReason   string   `yaml:"reject_reason"`
	HTTPStatus     []int    `yaml:"http_status"`
	Secrets        []string `yaml:"secrets"`
	TimeoutAtLeast string   `yaml:"timeout_at_least"`
}

// LoadFile reads named YAML cases. It does not replace Product.
//
// Parameters:
//   - path: Filesystem path to a YAML document or array of documents.
//
// Returns:
//   - []Case: Parsed cases with IDs assigned.
//   - error: Filesystem or YAML failure.
func LoadFile(path string) ([]Case, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read case file: %w", err)
	}

	var many []fileCase

	unmarshalErr := yaml.Unmarshal(raw, &many)
	if unmarshalErr != nil {
		var one fileCase

		oneErr := yaml.Unmarshal(raw, &one)
		if oneErr != nil {
			return nil, fmt.Errorf("parse case file: %w", unmarshalErr)
		}

		many = []fileCase{one}
	}

	out := make([]Case, 0, len(many))
	for _, parsed := range many {
		out = append(out, parsed.toCase())
	}

	return out, nil
}

// toCase converts YAML into a runtime Case.
//
// Returns:
//   - Case: Runtime vector.
func (parsed fileCase) toCase() Case {
	watchtowerCfg := parsed.Watchtower
	parsedTopology := parsed.Topology
	item := Case{
		Factors:     parsed.Factors,
		Packaging:   Packaging(parsed.Packaging),
		Channel:     ConfigChannel(parsed.Channel),
		Shape:       ProcessShape(parsed.Shape),
		ImageSource: parsed.ImageSource,
		Names:       parsed.Names,
		Watchtower: WatchtowerConfig{
			Host:                         watchtowerCfg.Host,
			TLSVerify:                    watchtowerCfg.TLSVerify,
			APIVersion:                   watchtowerCfg.APIVersion,
			CertPath:                     watchtowerCfg.CertPath,
			IncludeStopped:               watchtowerCfg.IncludeStopped,
			IncludeRestarting:            watchtowerCfg.IncludeRestarting,
			ReviveStopped:                watchtowerCfg.ReviveStopped,
			RemoveVolumes:                watchtowerCfg.RemoveVolumes,
			WarnOnHeadFailure:            watchtowerCfg.WarnOnHeadFailure,
			Interval:                     watchtowerCfg.Interval,
			Schedule:                     watchtowerCfg.Schedule,
			UpdateOnStart:                watchtowerCfg.UpdateOnStart,
			RunOnce:                      watchtowerCfg.RunOnce,
			HealthCheck:                  watchtowerCfg.HealthCheck,
			Porcelain:                    watchtowerCfg.Porcelain,
			NoStartupMessage:             watchtowerCfg.NoStartupMessage,
			SelfUpdateOrchestrator:       watchtowerCfg.SelfUpdateOrchestrator,
			Cleanup:                      watchtowerCfg.Cleanup,
			NoPull:                       watchtowerCfg.NoPull,
			NoRestart:                    watchtowerCfg.NoRestart,
			MonitorOnly:                  watchtowerCfg.MonitorOnly,
			RollingRestart:               watchtowerCfg.RollingRestart,
			StopTimeout:                  parseOptionalDuration(watchtowerCfg.StopTimeout),
			CooldownDelay:                watchtowerCfg.CooldownDelay,
			UseComposeDependsOn:          watchtowerCfg.UseComposeDependsOn,
			LabelTakePrecedence:          watchtowerCfg.LabelTakePrecedence,
			EphemeralSelfUpdate:          watchtowerCfg.EphemeralSelfUpdate,
			DiskSpaceMax:                 watchtowerCfg.DiskSpaceMax,
			DiskSpaceWarn:                watchtowerCfg.DiskSpaceWarn,
			EnableLifecycleHooks:         watchtowerCfg.EnableLifecycleHooks,
			LifecycleUID:                 watchtowerCfg.LifecycleUID,
			LifecycleGID:                 watchtowerCfg.LifecycleGID,
			LabelEnable:                  watchtowerCfg.LabelEnable,
			DisableContainers:            optionalStrings(watchtowerCfg.DisableContainers),
			MonitorImageNames:            optionalStrings(watchtowerCfg.MonitorImageNames),
			SkipImageNames:               optionalStrings(watchtowerCfg.SkipImageNames),
			EnableContainersByLabel:      optionalStrings(watchtowerCfg.EnableContainersByLabel),
			DisableContainersByLabel:     optionalStrings(watchtowerCfg.DisableContainersByLabel),
			Scope:                        watchtowerCfg.Scope,
			RegistryTLSSkip:              watchtowerCfg.RegistryTLSSkip,
			RegistryTLSMinVersion:        watchtowerCfg.RegistryTLSMinVersion,
			DisableMemorySwappiness:      watchtowerCfg.DisableMemorySwappiness,
			CPUCopyMode:                  watchtowerCfg.CPUCopyMode,
			HTTPAPIEndpoints:             optionalStrings(watchtowerCfg.HTTPAPIEndpoints),
			HTTPAPIUpdate:                watchtowerCfg.HTTPAPIUpdate,
			HTTPAPIMetrics:               watchtowerCfg.HTTPAPIMetrics,
			HTTPAPIContainers:            watchtowerCfg.HTTPAPIContainers,
			HTTPAPIHost:                  watchtowerCfg.HTTPAPIHost,
			HTTPAPIPort:                  watchtowerCfg.HTTPAPIPort,
			HTTPAPIToken:                 watchtowerCfg.HTTPAPIToken,
			HTTPAPIEventsToken:           watchtowerCfg.HTTPAPIEventsToken,
			HTTPAPIPeriodicPolls:         watchtowerCfg.HTTPAPIPeriodicPolls,
			HTTPAPIRateLimit:             watchtowerCfg.HTTPAPIRateLimit,
			HTTPAPITLSCert:               watchtowerCfg.HTTPAPITLSCert,
			HTTPAPITLSKey:                watchtowerCfg.HTTPAPITLSKey,
			HTTPAPITrustedProxies:        optionalStrings(watchtowerCfg.HTTPAPITrustedProxies),
			HTTPAPIProxyHeader:           watchtowerCfg.HTTPAPIProxyHeader,
			HTTPAPICORSOrigins:           optionalStrings(watchtowerCfg.HTTPAPICORSOrigins),
			HTTPAPICheckTimeout:          parseOptionalDuration(watchtowerCfg.HTTPAPICheckTimeout),
			HTTPAPIUpdateTimeout:         parseOptionalDuration(watchtowerCfg.HTTPAPIUpdateTimeout),
			NotificationURL:              optionalStrings(watchtowerCfg.NotificationURL),
			NotificationsLevel:           watchtowerCfg.NotificationsLevel,
			NotificationsDelay:           watchtowerCfg.NotificationsDelay,
			NotificationsHostname:        watchtowerCfg.NotificationsHostname,
			NotificationTemplate:         watchtowerCfg.NotificationTemplate,
			NotificationTemplateFile:     watchtowerCfg.NotificationTemplateFile,
			NotificationReport:           watchtowerCfg.NotificationReport,
			NotificationTitleTag:         watchtowerCfg.NotificationTitleTag,
			NotificationSkipTitle:        watchtowerCfg.NotificationSkipTitle,
			NotificationLogStdout:        watchtowerCfg.NotificationLogStdout,
			NotificationSplitByContainer: watchtowerCfg.NotificationSplitByContainer,
			Notifications:                optionalStrings(watchtowerCfg.Notifications),
			NotificationEmailFrom:        watchtowerCfg.NotificationEmailFrom,
			NotificationEmailTo:          watchtowerCfg.NotificationEmailTo,
			NotificationEmailDelay:       watchtowerCfg.NotificationEmailDelay,
			NotificationEmailServer:      watchtowerCfg.NotificationEmailServer,
			NotificationEmailServerPort:  watchtowerCfg.NotificationEmailServerPort,
			NotificationEmailTLSSkip:     watchtowerCfg.NotificationEmailTLSSkip,
			NotificationEmailUser:        watchtowerCfg.NotificationEmailUser,
			NotificationEmailPassword:    watchtowerCfg.NotificationEmailPassword,
			NotificationEmailSubjectTag:  watchtowerCfg.NotificationEmailSubjectTag,
			NotificationSlackHookURL:     watchtowerCfg.NotificationSlackHookURL,
			NotificationSlackIdentifier:  watchtowerCfg.NotificationSlackIdentifier,
			NotificationSlackChannel:     watchtowerCfg.NotificationSlackChannel,
			NotificationSlackIconEmoji:   watchtowerCfg.NotificationSlackIconEmoji,
			NotificationSlackIconURL:     watchtowerCfg.NotificationSlackIconURL,
			NotificationMSTeamsHook:      watchtowerCfg.NotificationMSTeamsHook,
			NotificationGotifyURL:        watchtowerCfg.NotificationGotifyURL,
			NotificationGotifyToken:      watchtowerCfg.NotificationGotifyToken,
			NotificationGotifyTLSSkip:    watchtowerCfg.NotificationGotifyTLSSkip,
			LogFormat:                    watchtowerCfg.LogFormat,
			LogLevel:                     watchtowerCfg.LogLevel,
			Debug:                        watchtowerCfg.Debug,
			Trace:                        watchtowerCfg.Trace,
			NoColor:                      watchtowerCfg.NoColor,
		},
		Topology: Topology{
			SubjectKind:        parsedTopology.SubjectKind,
			SubjectState:       parsedTopology.SubjectState,
			SubjectCount:       parsedTopology.SubjectCount,
			Graph:              GraphKind(parsedTopology.Graph),
			Networks:           parsedTopology.Networks,
			NetworkMode:        parsedTopology.NetworkMode,
			DigestPinned:       parsedTopology.DigestPinned,
			RegistryPersona:    parsedTopology.RegistryPersona,
			RegistryFault:      parsedTopology.RegistryFault,
			DockerTransport:    parsedTopology.DockerTransport,
			HostEnvelope:       envelopeFromFile(parsedTopology.HostEnvelope),
			WatchtowerEnvelope: envelopeFromFile(parsedTopology.WatchtowerEnvelope),
			SubjectEnvelope:    envelopeFromFile(parsedTopology.SubjectEnvelope),
			Labels:             parsedTopology.Labels,
			StopSignal:         parsedTopology.StopSignal,
			Lifecycle: LifecycleTopo{
				PreCheck:   parsedTopology.Lifecycle.PreCheck,
				PostCheck:  parsedTopology.Lifecycle.PostCheck,
				PreUpdate:  parsedTopology.Lifecycle.PreUpdate,
				PostUpdate: parsedTopology.Lifecycle.PostUpdate,
				PreTimeout: parsedTopology.Lifecycle.PreTimeout,
				UID:        parsedTopology.Lifecycle.UID,
				GID:        parsedTopology.Lifecycle.GID,
			},
			HTTPQuery: HTTPQuery{
				Image:     parsedTopology.HTTPQuery.Image,
				Container: parsedTopology.HTTPQuery.Container,
			},
			NotifySink:       parsedTopology.NotifySink,
			RemoteDocker:     parsedTopology.RemoteDocker,
			ImageCreatedAge:  parseDurationValue(parsedTopology.ImageCreatedAge),
			Mirror:           parsedTopology.Mirror,
			SharedImage:      parsedTopology.SharedImage,
			Decoy:            parsedTopology.Decoy,
			EnableLabel:      parsedTopology.EnableLabel,
			ScopeLabel:       parsedTopology.ScopeLabel,
			MonitorOnlyLabel: parsedTopology.MonitorOnlyLabel,
			NoPullLabel:      parsedTopology.NoPullLabel,
			CooldownLabel:    parsedTopology.CooldownLabel,
			NetDelay:         parseDurationValue(parsedTopology.NetDelay),
			RestartPolicy:    parsedTopology.RestartPolicy,
			ExtraEnv:         parsedTopology.ExtraEnv,
		},
	}

	if item.Factors == nil {
		item.Factors = map[string]string{}
	}

	if item.Packaging == "" {
		item.Packaging = PackagingContainer
	}

	if item.Channel == "" {
		item.Channel = ChannelEnv
	}

	if item.Shape == "" {
		item.Shape = ShapeRunOnce
	}

	item.Expect = DeriveExpect(item)
	if parsed.Expect.Outcome != "" {
		item.Expect.Outcome = Outcome(parsed.Expect.Outcome)
		item.Expect.RejectReason = parsed.Expect.RejectReason
		item.Expect.HTTPStatus = parsed.Expect.HTTPStatus
		item.Expect.Secrets = parsed.Expect.Secrets
		item.Expect.TimeoutAtLeast = parseDurationValue(parsed.Expect.TimeoutAtLeast)
	}

	if parsed.ID != "" {
		item.id = parsed.ID
	} else {
		item.AssignID()
	}

	return item
}

// optionalStrings returns a pointer to values, or nil when the slice is empty.
//
// Parameters:
//   - values: YAML list.
//
// Returns:
//   - *[]string: Pointer for WatchtowerConfig, or nil.
func optionalStrings(values []string) *[]string {
	if len(values) == 0 {
		return nil
	}

	return StringsPtr(values...)
}

// parseOptionalDuration parses a Go duration string pointer.
//
// Parameters:
//   - raw: Duration such as 30s, or nil.
//
// Returns:
//   - *time.Duration: Parsed duration, or nil.
func parseOptionalDuration(raw *string) *time.Duration {
	if raw == nil {
		return nil
	}

	parsed, err := time.ParseDuration(*raw)
	if err != nil {
		return nil
	}

	return new(parsed)
}

// parseDurationValue parses a Go duration string, or returns zero.
//
// Parameters:
//   - raw: Duration such as 24h, or empty.
//
// Returns:
//   - time.Duration: Parsed duration, or 0.
func parseDurationValue(raw string) time.Duration {
	if raw == "" {
		return 0
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0
	}

	return parsed
}

// envelopeFromFile copies YAML envelope numbers onto Topology.
//
// Parameters:
//   - parsed: YAML envelope block.
//
// Returns:
//   - Envelope: Runtime envelope.
func envelopeFromFile(parsed fileEnvelope) Envelope {
	return Envelope(parsed)
}

package engine

import "time"

// WatchtowerConfig is every Watchtower flag domain. Nil / unset means default.
type WatchtowerConfig struct {
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

	Cleanup             *bool          `yaml:"cleanup"`
	NoPull              *bool          `yaml:"no_pull"`
	NoRestart           *bool          `yaml:"no_restart"`
	MonitorOnly         *bool          `yaml:"monitor_only"`
	RollingRestart      *bool          `yaml:"rolling_restart"`
	StopTimeout         *time.Duration `yaml:"-"`
	CooldownDelay       *string        `yaml:"cooldown_delay"`
	UseComposeDependsOn *bool          `yaml:"use_compose_depends_on"`
	LabelTakePrecedence *bool          `yaml:"label_take_precedence"`
	EphemeralSelfUpdate *bool          `yaml:"ephemeral_self_update"`
	DiskSpaceMax        *string        `yaml:"disk_space_max"`
	DiskSpaceWarn       *string        `yaml:"disk_space_warn"`

	EnableLifecycleHooks *bool `yaml:"enable_lifecycle_hooks"`
	LifecycleUID         *int  `yaml:"lifecycle_uid"`
	LifecycleGID         *int  `yaml:"lifecycle_gid"`

	LabelEnable              *bool     `yaml:"label_enable"`
	DisableContainers        *[]string `yaml:"disable_containers"`
	MonitorImageNames        *[]string `yaml:"monitor_image_names"`
	SkipImageNames           *[]string `yaml:"skip_image_names"`
	EnableContainersByLabel  *[]string `yaml:"enable_containers_by_label"`
	DisableContainersByLabel *[]string `yaml:"disable_containers_by_label"`
	Scope                    *string   `yaml:"scope"`

	RegistryTLSSkip       *bool   `yaml:"registry_tls_skip"`
	RegistryTLSMinVersion *string `yaml:"registry_tls_min_version"`

	DisableMemorySwappiness *bool   `yaml:"disable_memory_swappiness"`
	CPUCopyMode             *string `yaml:"cpu_copy_mode"`

	HTTPAPIEndpoints      *[]string      `yaml:"http_api_endpoints"`
	HTTPAPIUpdate         *bool          `yaml:"http_api_update"`
	HTTPAPIMetrics        *bool          `yaml:"http_api_metrics"`
	HTTPAPIContainers     *bool          `yaml:"http_api_containers"`
	HTTPAPIHost           *string        `yaml:"http_api_host"`
	HTTPAPIPort           *string        `yaml:"http_api_port"`
	HTTPAPIToken          *string        `yaml:"http_api_token"`
	HTTPAPIEventsToken    *string        `yaml:"http_api_events_token"`
	HTTPAPIPeriodicPolls  *bool          `yaml:"http_api_periodic_polls"`
	HTTPAPIRateLimit      *int           `yaml:"http_api_rate_limit"`
	HTTPAPITLSCert        *string        `yaml:"http_api_tls_cert"`
	HTTPAPITLSKey         *string        `yaml:"http_api_tls_key"`
	HTTPAPITrustedProxies *[]string      `yaml:"http_api_trusted_proxies"`
	HTTPAPIProxyHeader    *string        `yaml:"http_api_proxy_header"`
	HTTPAPICORSOrigins    *[]string      `yaml:"http_api_cors_origins"`
	HTTPAPICheckTimeout   *time.Duration `yaml:"-"`
	HTTPAPIUpdateTimeout  *time.Duration `yaml:"-"`

	NotificationURL              *[]string `yaml:"notification_url"`
	NotificationsLevel           *string   `yaml:"notifications_level"`
	NotificationsDelay           *int      `yaml:"notifications_delay"`
	NotificationsHostname        *string   `yaml:"notifications_hostname"`
	NotificationTemplate         *string   `yaml:"notification_template"`
	NotificationTemplateFile     *string   `yaml:"notification_template_file"`
	NotificationReport           *bool     `yaml:"notification_report"`
	NotificationTitleTag         *string   `yaml:"notification_title_tag"`
	NotificationSkipTitle        *bool     `yaml:"notification_skip_title"`
	NotificationLogStdout        *bool     `yaml:"notification_log_stdout"`
	NotificationSplitByContainer *bool     `yaml:"notification_split_by_container"`
	Notifications                *[]string `yaml:"notifications"`
	NotificationEmailFrom        *string   `yaml:"notification_email_from"`
	NotificationEmailTo          *string   `yaml:"notification_email_to"`
	NotificationEmailDelay       *int      `yaml:"notification_email_delay"`
	NotificationEmailServer      *string   `yaml:"notification_email_server"`
	NotificationEmailServerPort  *int      `yaml:"notification_email_server_port"`
	NotificationEmailTLSSkip     *bool     `yaml:"notification_email_tls_skip"`
	NotificationEmailUser        *string   `yaml:"notification_email_user"`
	NotificationEmailPassword    *string   `yaml:"notification_email_password"`
	NotificationEmailSubjectTag  *string   `yaml:"notification_email_subjecttag"`
	NotificationSlackHookURL     *string   `yaml:"notification_slack_hook_url"`
	NotificationSlackIdentifier  *string   `yaml:"notification_slack_identifier"`
	NotificationSlackChannel     *string   `yaml:"notification_slack_channel"`
	NotificationSlackIconEmoji   *string   `yaml:"notification_slack_icon_emoji"`
	NotificationSlackIconURL     *string   `yaml:"notification_slack_icon_url"`
	NotificationMSTeamsHook      *string   `yaml:"notification_msteams_hook"`
	NotificationGotifyURL        *string   `yaml:"notification_gotify_url"`
	NotificationGotifyToken      *string   `yaml:"notification_gotify_token"`
	NotificationGotifyTLSSkip    *bool     `yaml:"notification_gotify_tls_skip"`

	LogFormat *string `yaml:"log_format"`
	LogLevel  *string `yaml:"log_level"`
	Debug     *bool   `yaml:"debug"`
	Trace     *bool   `yaml:"trace"`
	NoColor   *bool   `yaml:"no_color"`
}

// flagSpec maps one WatchtowerConfig field onto CLI and environment names.
type flagSpec struct {
	// flag is the long CLI name without dashes.
	flag string
	// env is the environment key, or empty when the flag has no env binding.
	env string
	// kind selects argv rendering.
	kind renderKind
	// get reads the field. set is false when the pointer is nil.
	get func(WatchtowerConfig) (set bool, value string)
}

// renderKind selects how a field is written to argv and env.
type renderKind int

const (
	// renderBool is a boolean flag.
	renderBool renderKind = iota
	// renderScalar is a string, int, or duration.
	renderScalar
	// renderSlice is a comma-joined list.
	renderSlice
)

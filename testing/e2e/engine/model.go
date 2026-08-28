package engine

import (
	"strconv"
	"strings"
	"time"
)

const (
	// ShortPollSeconds is the interval used when process.shape is interval.
	ShortPollSeconds = 2
	// ShortStopTimeout is a tight stop timeout for slow-term / deaf-term cases.
	ShortStopTimeout = 2 * time.Second
	// HarnessAPIAuth is a fixture HTTP API bearer value, not a real credential.
	HarnessAPIAuth = "e2e-api-auth"
	// HarnessEventsAuth is a fixture events SSE bearer value, not a real credential.
	HarnessEventsAuth = "e2e-events-auth"
	// smallHostMemoryBytes is the "small" DinD memory envelope.
	smallHostMemoryBytes = 512 << 20
	// smallHostNanoCPUs is the "small" DinD CPU envelope.
	smallHostNanoCPUs = 500_000_000
	// smallHostPids is the "small" DinD pids envelope.
	smallHostPids = 256
	// smallWatchtowerMemoryBytes is the "small" Watchtower memory envelope.
	smallWatchtowerMemoryBytes = 64 << 20
	// smallWatchtowerNanoCPUs is the "small" Watchtower CPU envelope.
	smallWatchtowerNanoCPUs = 250_000_000
	// smallWatchtowerPids is the "small" Watchtower pids envelope.
	smallWatchtowerPids = 64
	// factorCap is the Model() slice capacity hint.
	factorCap = 80
	// SmokeIntervalSeconds is unused by run-once smoke but kept for interval shape.
	SmokeIntervalSeconds = 2
)

// Model returns every cartesian factor: process mutexes, taxonomy A–R, and flags.
//
// Every RegisterAll flag has at least unset plus documented interesting values.
// Process shape and packaging are mutexes. Remaining flags combine freely.
// Constraints in DeriveExpect / Unrealizable are Watchtower-illegal or
// unrealizable topology, not a test-count budget.
//
// Returns:
//   - []Factor: Ordered factors for Product and Random.
func Model() []Factor {
	factors := make([]Factor, 0, factorCap)
	factors = append(factors, []Factor{
		processShapeFactor(),
		packagingFactor(),
		channelFactor(),
		imageSourceFactor(),
		subjectKindFactor(),
		subjectStateFactor(),
		graphFactor(),
		networksFactor(),
		digestPinnedFactor(),
		personaFactor(),
		faultFactor(),
		transportFactor(),
		hostEnvelopeFactor(),
		watchtowerEnvelopeFactor(),
		decoyFactor(),
		notifySinkFactor(),
		httpEndpointsFactor(),
	}...)

	factors = append(factors, flagFactors()...)

	return factors
}

// processShapeFactor is the mutex over run-once, interval, schedule, and HTTP API shapes.
//
// Returns:
//   - Factor: Process-shape axis.
func processShapeFactor() Factor {
	return Factor{
		Name:   "process.shape",
		Levels: []string{string(ShapeRunOnce), string(ShapeInterval), string(ShapeSchedule), string(ShapeIntervalSchedule), string(ShapeHTTPUpdate), string(ShapeHTTPUpdatePeriodic)},
		Apply: func(c *Case, level string) {
			c.Shape = ProcessShape(level)
			applyProcessShape(&c.Watchtower, c.Shape)
		},
	}
}

// applyProcessShape sets the mutually exclusive process-shape flags.
//
// Parameters:
//   - cfg: Watchtower config to mutate.
//   - shape: Selected process shape.
func applyProcessShape(cfg *WatchtowerConfig, shape ProcessShape) {
	switch shape {
	case ShapeRunOnce:
		cfg.RunOnce = new(true)
		cfg.Porcelain = new("json")
	case ShapeInterval:
		cfg.Interval = new(ShortPollSeconds)
		cfg.UpdateOnStart = new(true)
	case ShapeSchedule:
		cfg.Schedule = new("@every 2s")
		cfg.UpdateOnStart = new(true)
	case ShapeIntervalSchedule:
		cfg.Interval = new(ShortPollSeconds)
		cfg.Schedule = new("@every 2s")
	case ShapeHTTPUpdate:
		cfg.HTTPAPIUpdate = new(true)
		cfg.HTTPAPIEndpoints = StringsPtr("all")
		cfg.HTTPAPIToken = new(HarnessAPIAuth)
		cfg.HTTPAPIEventsToken = new(HarnessEventsAuth)
	case ShapeHTTPUpdatePeriodic:
		cfg.HTTPAPIUpdate = new(true)
		cfg.HTTPAPIPeriodicPolls = new(true)
		cfg.HTTPAPIEndpoints = StringsPtr("all")
		cfg.HTTPAPIToken = new(HarnessAPIAuth)
		cfg.HTTPAPIEventsToken = new(HarnessEventsAuth)
		cfg.Interval = new(ShortPollSeconds)
	}
}

// packagingFactor selects Watchtower as a DinD container or a host binary.
//
// Returns:
//   - Factor: Packaging axis.
func packagingFactor() Factor {
	return Factor{
		Name:   "packaging",
		Levels: []string{string(PackagingContainer), string(PackagingBinary)},
		Apply: func(c *Case, level string) {
			c.Packaging = Packaging(level)
		},
	}
}

// channelFactor selects flags, environment, mixed, or secret-file delivery.
//
// Returns:
//   - Factor: Config-channel axis.
func channelFactor() Factor {
	return Factor{
		Name:   "config.channel",
		Levels: []string{string(ChannelFlags), string(ChannelEnv), string(ChannelMixed), string(ChannelSecretFile)},
		Apply: func(c *Case, level string) {
			c.Channel = ConfigChannel(level)
		},
	}
}

// imageSourceFactor selects a thin scratch wrap or Dockerfile.self-local.
//
// Returns:
//   - Factor: Image-source axis.
func imageSourceFactor() Factor {
	return Factor{
		Name:   "image.source",
		Levels: []string{"thin", "self-local"},
		Apply: func(c *Case, level string) {
			c.ImageSource = level
		},
	}
}

// subjectKindFactor selects the subject family member under test.
//
// Returns:
//   - Factor: Subject-kind axis.
func subjectKindFactor() Factor {
	return Factor{
		Name:   "subject.kind",
		Levels: []string{"echo", "slow-term", "deaf-term", "custom-signal", "healthcheck", "nonroot", "volume-writer", "self"},
		Apply: func(c *Case, level string) {
			c.Topology.SubjectKind = level
			if level == "custom-signal" {
				c.Topology.StopSignal = "SIGHUP"
			}
		},
	}
}

// subjectStateFactor selects the subject's Docker runtime state before Watchtower starts.
//
// Returns:
//   - Factor: Subject-state axis.
func subjectStateFactor() Factor {
	return Factor{
		Name:   "subject.state",
		Levels: []string{"running", "exited", "created", "paused", "restarting"},
		Apply: func(c *Case, level string) {
			c.Topology.SubjectState = level
			if level == "exited" || level == "created" {
				c.Watchtower.IncludeStopped = new(true)
			}

			if level == "restarting" {
				c.Watchtower.IncludeRestarting = new(true)
			}
		},
	}
}

// graphFactor selects depends-on topology.
//
// Returns:
//   - Factor: Graph axis.
func graphFactor() Factor {
	return Factor{
		Name:   "graph",
		Levels: []string{string(GraphNone), string(GraphChain4), string(GraphCycle), string(GraphComposeDepends)},
		Apply: func(c *Case, level string) {
			c.Topology.Graph = GraphKind(level)
		},
	}
}

// networksFactor selects one or two extra inner networks on the subject.
//
// Returns:
//   - Factor: Networks axis.
func networksFactor() Factor {
	return Factor{
		Name:   "networks",
		Levels: []string{"one", "two"},
		Apply: func(c *Case, level string) {
			if level == "two" {
				c.Topology.Networks = []string{"e2e-a", "e2e-b"}
			}
		},
	}
}

// digestPinnedFactor pins the subject to repo@sha256 so it must not update.
//
// Returns:
//   - Factor: Digest-pin axis.
func digestPinnedFactor() Factor {
	return Factor{
		Name:   "image.digest_pinned",
		Levels: []string{"false", "true"},
		Apply: func(c *Case, level string) {
			c.Topology.DigestPinned = level == "true"
		},
	}
}

// personaFactor selects Hub, GHCR, LSCR, private, or no fake registry dialect.
//
// Returns:
//   - Factor: Registry-persona axis.
func personaFactor() Factor {
	return Factor{
		Name:   "registry.persona",
		Levels: []string{"none", "hub", "ghcr", "lscr", "private"},
		Apply: func(c *Case, level string) {
			c.Topology.RegistryPersona = level
			if level != "none" {
				c.Watchtower.RegistryTLSSkip = new(true)
				c.Packaging = PackagingContainer
			}
		},
	}
}

// faultFactor selects a programmable registry failure injected by the persona proxy.
//
// Returns:
//   - Factor: Registry-fault axis.
func faultFactor() Factor {
	return Factor{
		Name:   "registry.fault",
		Levels: []string{"none", "429-hub", "429-ghcr", "401", "403", "expire-token", "slow-head", "5xx"},
		Apply: func(c *Case, level string) {
			c.Topology.RegistryFault = level
		},
	}
}

// transportFactor selects unix, TCP, TLS, or remote Docker API transport.
//
// Returns:
//   - Factor: Docker-transport axis.
func transportFactor() Factor {
	return Factor{
		Name:   "docker.transport",
		Levels: []string{"unix", "tcp", "tcp-tls", "remote"},
		Apply: func(c *Case, level string) {
			c.Topology.DockerTransport = level
			c.Topology.RemoteDocker = level == "remote"
		},
	}
}

// hostEnvelopeFactor optionally applies a small CPU and memory budget to DinD.
//
// Returns:
//   - Factor: Host-envelope axis.
func hostEnvelopeFactor() Factor {
	return Factor{
		Name:   "host.envelope",
		Levels: []string{"none", "small"},
		Apply: func(c *Case, level string) {
			if level == "small" {
				c.Topology.HostEnvelope = Envelope{
					MemoryBytes: smallHostMemoryBytes,
					NanoCPUs:    smallHostNanoCPUs,
					PidsLimit:   smallHostPids,
				}
			}
		},
	}
}

// watchtowerEnvelopeFactor optionally applies a small CPU and memory budget to Watchtower.
//
// Returns:
//   - Factor: Watchtower-envelope axis.
func watchtowerEnvelopeFactor() Factor {
	return Factor{
		Name:   "watchtower.envelope",
		Levels: []string{"none", "small"},
		Apply: func(c *Case, level string) {
			if level == "small" {
				c.Topology.WatchtowerEnvelope = Envelope{
					MemoryBytes: smallWatchtowerMemoryBytes,
					NanoCPUs:    smallWatchtowerNanoCPUs,
					PidsLimit:   smallWatchtowerPids,
				}
			}
		},
	}
}

// decoyFactor starts an extra container that must not be updated.
//
// Returns:
//   - Factor: Decoy axis.
func decoyFactor() Factor {
	return Factor{
		Name:   "decoy",
		Levels: []string{"false", "true"},
		Apply: func(c *Case, level string) {
			c.Topology.Decoy = level == "true"
		},
	}
}

// notifySinkFactor selects no notifications, a webhook, or a failing webhook.
//
// Returns:
//   - Factor: Notify-sink axis.
func notifySinkFactor() Factor {
	return Factor{
		Name:   "notify.sink",
		Levels: []string{"none", "webhook", "webhook-5xx"},
		Apply: func(c *Case, level string) {
			c.Topology.NotifySink = level
		},
	}
}

// httpEndpointsFactor selects HTTP API endpoint lists.
//
// Returns:
//   - Factor: API-endpoints axis.
func httpEndpointsFactor() Factor {
	return Factor{
		Name:   "api.endpoints",
		Flag:   "http-api-endpoints",
		Levels: []string{LevelUnset, "all", "health", "update", "health,update,metrics,check,containers,history,images,config,events,swagger"},
		Apply: func(c *Case, level string) {
			if level == LevelUnset {
				return
			}

			c.Watchtower.HTTPAPIEndpoints = StringsPtr(splitCSV(level)...)
			if c.Watchtower.HTTPAPIToken == nil {
				c.Watchtower.HTTPAPIToken = new(HarnessAPIAuth)
			}
		},
	}
}

// flagFactors returns per-flag axes not already covered by process shape.
//
// Returns:
//   - []Factor: Flag axes appended to Model().
func flagFactors() []Factor {
	return []Factor{
		boolFlag("include-stopped", "WATCHTOWER_INCLUDE_STOPPED", func(c *Case, ptr *bool) { c.Watchtower.IncludeStopped = ptr }),
		boolFlag("include-restarting", "WATCHTOWER_INCLUDE_RESTARTING", func(c *Case, ptr *bool) { c.Watchtower.IncludeRestarting = ptr }),
		boolFlag("revive-stopped", "WATCHTOWER_REVIVE_STOPPED", func(c *Case, ptr *bool) { c.Watchtower.ReviveStopped = ptr }),
		boolFlag("remove-volumes", "WATCHTOWER_REMOVE_VOLUMES", func(c *Case, ptr *bool) { c.Watchtower.RemoveVolumes = ptr }),
		enumFlag("warn-on-head-failure", "warn-on-head-failure", []string{LevelUnset, "always", "auto", "never"}, func(c *Case, level string) {
			c.Watchtower.WarnOnHeadFailure = new(level)
		}),
		boolFlag("update-on-start", "WATCHTOWER_UPDATE_ON_START", func(c *Case, ptr *bool) { c.Watchtower.UpdateOnStart = ptr }),
		enumFlag("porcelain", "porcelain", []string{LevelUnset, "json", "v1"}, func(c *Case, level string) {
			c.Watchtower.Porcelain = new(level)
		}),
		boolFlag("no-startup-message", "WATCHTOWER_NO_STARTUP_MESSAGE", func(c *Case, ptr *bool) { c.Watchtower.NoStartupMessage = ptr }),
		boolFlag("cleanup", "WATCHTOWER_CLEANUP", func(c *Case, ptr *bool) { c.Watchtower.Cleanup = ptr }),
		boolFlag("no-pull", "WATCHTOWER_NO_PULL", func(c *Case, ptr *bool) { c.Watchtower.NoPull = ptr }),
		boolFlag("no-restart", "WATCHTOWER_NO_RESTART", func(c *Case, ptr *bool) { c.Watchtower.NoRestart = ptr }),
		boolFlag("monitor-only", "WATCHTOWER_MONITOR_ONLY", func(c *Case, ptr *bool) { c.Watchtower.MonitorOnly = ptr }),
		boolFlag("rolling-restart", "WATCHTOWER_ROLLING_RESTART", func(c *Case, ptr *bool) { c.Watchtower.RollingRestart = ptr }),
		enumFlag("stop-timeout", "stop-timeout", []string{LevelUnset, "2s", "30s"}, func(c *Case, level string) {
			parsed, err := time.ParseDuration(level)
			if err != nil {
				return
			}

			c.Watchtower.StopTimeout = new(parsed)
		}),
		enumFlag("cooldown-delay", "cooldown-delay", []string{LevelUnset, "24h", "0"}, func(c *Case, level string) {
			c.Watchtower.CooldownDelay = new(level)
		}),
		boolFlag("use-compose-depends-on", "WATCHTOWER_USE_COMPOSE_DEPENDS_ON", func(c *Case, ptr *bool) { c.Watchtower.UseComposeDependsOn = ptr }),
		boolFlag("label-take-precedence", "WATCHTOWER_LABEL_TAKE_PRECEDENCE", func(c *Case, ptr *bool) { c.Watchtower.LabelTakePrecedence = ptr }),
		boolFlag("ephemeral-self-update", "WATCHTOWER_EPHEMERAL_SELF_UPDATE", func(c *Case, ptr *bool) { c.Watchtower.EphemeralSelfUpdate = ptr }),
		enumFlag("disk-space-max", "disk-space-max", []string{LevelUnset, "40GB"}, func(c *Case, level string) {
			c.Watchtower.DiskSpaceMax = new(level)
		}),
		enumFlag("disk-space-warn", "disk-space-warn", []string{LevelUnset, "80%", "30GB"}, func(c *Case, level string) {
			c.Watchtower.DiskSpaceWarn = new(level)
		}),
		boolFlag("enable-lifecycle-hooks", "WATCHTOWER_LIFECYCLE_HOOKS", func(c *Case, ptr *bool) { c.Watchtower.EnableLifecycleHooks = ptr }),
		enumFlag("lifecycle-uid", "lifecycle-uid", []string{LevelUnset, "0", "1000"}, func(c *Case, level string) {
			c.Watchtower.LifecycleUID = new(atoi(level))
		}),
		enumFlag("lifecycle-gid", "lifecycle-gid", []string{LevelUnset, "0", "1000"}, func(c *Case, level string) {
			c.Watchtower.LifecycleGID = new(atoi(level))
		}),
		boolFlag("label-enable", "WATCHTOWER_LABEL_ENABLE", func(c *Case, ptr *bool) { c.Watchtower.LabelEnable = ptr }),
		enumFlag("disable-containers", "disable-containers", []string{LevelUnset, "decoy"}, func(c *Case, level string) {
			c.Watchtower.DisableContainers = StringsPtr(level)
		}),
		enumFlag("monitor-image-names", "monitor-image-names", []string{LevelUnset, "e2e/app:latest"}, func(c *Case, level string) {
			c.Watchtower.MonitorImageNames = StringsPtr(level)
		}),
		enumFlag("skip-image-names", "skip-image-names", []string{LevelUnset, "e2e/skip:latest"}, func(c *Case, level string) {
			c.Watchtower.SkipImageNames = StringsPtr(level)
		}),
		enumFlag("enable-containers-by-label", "enable-containers-by-label", []string{LevelUnset, "e2e=true"}, func(c *Case, level string) {
			c.Watchtower.EnableContainersByLabel = StringsPtr(level)
		}),
		enumFlag("disable-containers-by-label", "disable-containers-by-label", []string{LevelUnset, "e2e=skip"}, func(c *Case, level string) {
			c.Watchtower.DisableContainersByLabel = StringsPtr(level)
		}),
		enumFlag("scope", "scope", []string{LevelUnset, "alpha", "none"}, func(c *Case, level string) {
			c.Watchtower.Scope = new(level)
		}),
		boolFlag("registry-tls-skip", "WATCHTOWER_REGISTRY_TLS_SKIP", func(c *Case, ptr *bool) { c.Watchtower.RegistryTLSSkip = ptr }),
		enumFlag("registry-tls-min-version", "registry-tls-min-version", []string{LevelUnset, "TLS1.2", "TLS1.3"}, func(c *Case, level string) {
			c.Watchtower.RegistryTLSMinVersion = new(level)
		}),
		boolFlag("disable-memory-swappiness", "WATCHTOWER_DISABLE_MEMORY_SWAPPINESS", func(c *Case, ptr *bool) { c.Watchtower.DisableMemorySwappiness = ptr }),
		enumFlag("cpu-copy-mode", "cpu-copy-mode", []string{LevelUnset, "auto", "full", "none"}, func(c *Case, level string) {
			c.Watchtower.CPUCopyMode = new(level)
		}),
		boolFlag("http-api-metrics", "WATCHTOWER_HTTP_API_METRICS", func(c *Case, ptr *bool) { c.Watchtower.HTTPAPIMetrics = ptr }),
		boolFlag("http-api-containers", "WATCHTOWER_HTTP_API_CONTAINERS", func(c *Case, ptr *bool) { c.Watchtower.HTTPAPIContainers = ptr }),
		enumFlag("http-api-port", "http-api-port", []string{LevelUnset, "8080", "9090"}, func(c *Case, level string) {
			c.Watchtower.HTTPAPIPort = new(level)
		}),
		boolFlag("http-api-periodic-polls", "WATCHTOWER_HTTP_API_PERIODIC_POLLS", func(c *Case, ptr *bool) { c.Watchtower.HTTPAPIPeriodicPolls = ptr }),
		enumFlag("http-api-rate-limit", "http-api-rate-limit", []string{LevelUnset, "60", "5"}, func(c *Case, level string) {
			c.Watchtower.HTTPAPIRateLimit = new(atoi(level))
		}),
		enumFlag("http-api-check-timeout", "http-api-check-timeout", []string{LevelUnset, "30s"}, func(c *Case, level string) {
			parsed, err := time.ParseDuration(level)
			if err != nil {
				return
			}

			c.Watchtower.HTTPAPICheckTimeout = new(parsed)
		}),
		enumFlag("http-api-update-timeout", "http-api-update-timeout", []string{LevelUnset, "1m"}, func(c *Case, level string) {
			parsed, err := time.ParseDuration(level)
			if err != nil {
				return
			}

			c.Watchtower.HTTPAPIUpdateTimeout = new(parsed)
		}),
		boolFlag("notification-report", "WATCHTOWER_NOTIFICATION_REPORT", func(c *Case, ptr *bool) { c.Watchtower.NotificationReport = ptr }),
		boolFlag("notification-skip-title", "WATCHTOWER_NOTIFICATION_SKIP_TITLE", func(c *Case, ptr *bool) { c.Watchtower.NotificationSkipTitle = ptr }),
		boolFlag("notification-log-stdout", "WATCHTOWER_NOTIFICATION_LOG_STDOUT", func(c *Case, ptr *bool) { c.Watchtower.NotificationLogStdout = ptr }),
		boolFlag("notification-split-by-container", "WATCHTOWER_NOTIFICATION_SPLIT_BY_CONTAINER", func(c *Case, ptr *bool) { c.Watchtower.NotificationSplitByContainer = ptr }),
		enumFlag("notifications", "notifications", []string{LevelUnset, "gotify"}, func(c *Case, level string) {
			c.Watchtower.Notifications = StringsPtr(level)
		}),
		enumFlag("log-format", "log-format", []string{LevelUnset, "json", "pretty", "logfmt", "auto"}, func(c *Case, level string) {
			c.Watchtower.LogFormat = new(level)
		}),
		enumFlag("log-level", "log-level", []string{LevelUnset, "trace", "debug", "info"}, func(c *Case, level string) {
			c.Watchtower.LogLevel = new(level)
		}),
		boolFlag("debug", "WATCHTOWER_DEBUG", func(c *Case, ptr *bool) { c.Watchtower.Debug = ptr }),
		boolFlag("trace", "WATCHTOWER_TRACE", func(c *Case, ptr *bool) { c.Watchtower.Trace = ptr }),
		boolFlag("no-color", "NO_COLOR", func(c *Case, ptr *bool) { c.Watchtower.NoColor = ptr }),
		enumFlag("host", "host", []string{LevelUnset, "unix:///var/run/docker.sock"}, func(c *Case, level string) {
			c.Watchtower.Host = new(level)
		}),
		boolFlag("tlsverify", "DOCKER_TLS_VERIFY", func(c *Case, ptr *bool) { c.Watchtower.TLSVerify = ptr }),
		enumFlag("api-version", "api-version", []string{LevelUnset, "1.44", "bogus"}, func(c *Case, level string) {
			c.Watchtower.APIVersion = new(level)
		}),
		lifecyclePhaseFactor(),
		filterStackFactor(),
		labelPrecedenceKnobFactor(),
	}
}

// boolFlag builds unset/true/false levels for a boolean Watchtower flag.
//
// Parameters:
//   - name: Long flag name.
//   - set: Assigns the pointer onto the case.
//
// Returns:
//   - Factor: Boolean flag axis.
func boolFlag(name, _ string, set func(*Case, *bool)) Factor {
	return Factor{
		Name:   "flag." + name,
		Flag:   name,
		Levels: []string{LevelUnset, "true", "false"},
		Apply: func(c *Case, level string) {
			switch level {
			case "true":
				set(c, new(true))
			case "false":
				set(c, new(false))
			}
		},
	}
}

// enumFlag builds discrete levels for a string, int, or duration flag.
//
// Parameters:
//   - name: Factor suffix after flag.
//   - flag: Watchtower long flag name.
//   - levels: Discrete values including unset.
//   - apply: Writes a non-unset level onto the case.
//
// Returns:
//   - Factor: Enumerated flag axis.
func enumFlag(name, flag string, levels []string, apply func(*Case, string)) Factor {
	return Factor{
		Name:   "flag." + name,
		Flag:   flag,
		Levels: levels,
		Apply: func(c *Case, level string) {
			if level == LevelUnset {
				return
			}

			apply(c, level)
		},
	}
}

// lifecyclePhaseFactor selects which lifecycle hook label is set on the subject.
//
// Returns:
//   - Factor: Lifecycle-phase axis.
func lifecyclePhaseFactor() Factor {
	return Factor{
		Name:   "lifecycle.phase",
		Levels: []string{LevelUnset, "pre-check", "post-check", "pre-update", "post-update", "pre-update-hang", "pre-update-fail"},
		Apply: func(c *Case, level string) {
			if level == LevelUnset {
				return
			}

			c.Watchtower.EnableLifecycleHooks = new(true)

			switch level {
			case "pre-check":
				c.Topology.Lifecycle.PreCheck = "/subject -hook pre-check"
			case "post-check":
				c.Topology.Lifecycle.PostCheck = "/subject -hook post-check"
			case "pre-update":
				c.Topology.Lifecycle.PreUpdate = "/subject -hook pre-update"
			case "post-update":
				c.Topology.Lifecycle.PostUpdate = "/subject -hook post-update"
			case "pre-update-hang":
				c.Topology.Lifecycle.PreUpdate = "/subject -hook hang"
				c.Topology.Lifecycle.PreTimeout = "1"
			case "pre-update-fail":
				c.Topology.Lifecycle.PreUpdate = "/subject -hook fail"
			}
		},
	}
}

// filterStackFactor selects stacked container-selection knobs.
//
// Returns:
//   - Factor: Filter-stack axis.
func filterStackFactor() Factor {
	return Factor{
		Name:   "filter.stack",
		Levels: []string{LevelUnset, "label-enable", "names+disable", "monitor-skip", "enable-disable-label", "http-intersect"},
		Apply: func(c *Case, level string) {
			switch level {
			case "label-enable":
				c.Watchtower.LabelEnable = new(true)
				c.Topology.EnableLabel = "true"
			case "names+disable":
				c.Names = []string{"e2e-subject"}
				c.Watchtower.DisableContainers = StringsPtr("e2e-decoy")
			case "monitor-skip":
				c.Watchtower.MonitorImageNames = StringsPtr("e2e/app:latest")
				c.Watchtower.SkipImageNames = StringsPtr("e2e/skip:latest")
			case "enable-disable-label":
				c.Watchtower.EnableContainersByLabel = StringsPtr("e2e=keep")
				c.Watchtower.DisableContainersByLabel = StringsPtr("e2e=drop")
			case "http-intersect":
				c.Topology.HTTPQuery.Image = "e2e/app"
				c.Topology.HTTPQuery.Container = "e2e-subject"
			}
		},
	}
}

// labelPrecedenceKnobFactor selects which per-container label take-precedence knob is set.
//
// Returns:
//   - Factor: Label-precedence axis.
func labelPrecedenceKnobFactor() Factor {
	return Factor{
		Name:   "label.take_precedence.knob",
		Levels: []string{LevelUnset, "monitor-only", "no-pull", "enable", "cooldown-delay"},
		Apply: func(c *Case, level string) {
			if level == LevelUnset {
				return
			}

			c.Watchtower.LabelTakePrecedence = new(true)

			switch level {
			case "monitor-only":
				c.Topology.MonitorOnlyLabel = "true"
			case "no-pull":
				c.Topology.NoPullLabel = "true"
			case "enable":
				c.Topology.EnableLabel = "false"
			case "cooldown-delay":
				c.Topology.CooldownLabel = "0"
			}
		},
	}
}

// splitCSV splits a comma-separated level into endpoint or image names.
//
// Parameters:
//   - raw: Comma-separated string.
//
// Returns:
//   - []string: Pieces.
func splitCSV(raw string) []string {
	return strings.Split(raw, ",")
}

// atoi parses a decimal integer level, or returns 0.
//
// Parameters:
//   - raw: Decimal digits.
//
// Returns:
//   - int: Parsed value, or 0 on error.
func atoi(raw string) int {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}

	return value
}

// CoveredFlags returns Watchtower long flags represented in Model().
//
// Returns:
//   - map[string]string: Flag name to factor name.
func CoveredFlags() map[string]string {
	covered := make(map[string]string)

	for _, factor := range Model() {
		if factor.Flag != "" {
			covered[factor.Flag] = factor.Name
		}
	}

	covered["run-once"] = "process.shape"
	covered["interval"] = "process.shape"
	covered["schedule"] = "process.shape"
	covered["http-api-update"] = "process.shape"
	covered["health-check"] = "process.shape"
	covered["self-update-orchestrator"] = "process.shape"
	covered["http-api-token"] = "process.shape"
	covered["http-api-events-token"] = "process.shape"
	covered["http-api-host"] = "api.endpoints"
	covered["http-api-tls-cert"] = "docker.transport"
	covered["http-api-tls-key"] = "docker.transport"
	covered["http-api-trusted-proxies"] = "api.endpoints"
	covered["http-api-proxy-header"] = "api.endpoints"
	covered["http-api-cors-origins"] = "api.endpoints"
	covered["cert-path"] = "docker.transport"
	covered["notification-url"] = "notify.sink"
	covered["notifications-level"] = "notify.sink"
	covered["notifications-delay"] = "notify.sink"
	covered["notifications-hostname"] = "notify.sink"
	covered["notification-template"] = "notify.sink"
	covered["notification-template-file"] = "notify.sink"
	covered["notification-title-tag"] = "notify.sink"
	covered["notification-email-from"] = "flag.notifications"
	covered["notification-email-to"] = "flag.notifications"
	covered["notification-email-delay"] = "flag.notifications"
	covered["notification-email-server"] = "flag.notifications"
	covered["notification-email-server-port"] = "flag.notifications"
	covered["notification-email-server-tls-skip-verify"] = "flag.notifications"
	covered["notification-email-server-user"] = "flag.notifications"
	covered["notification-email-server-password"] = "flag.notifications"
	covered["notification-email-subjecttag"] = "flag.notifications"
	covered["notification-slack-hook-url"] = "flag.notifications"
	covered["notification-slack-identifier"] = "flag.notifications"
	covered["notification-slack-channel"] = "flag.notifications"
	covered["notification-slack-icon-emoji"] = "flag.notifications"
	covered["notification-slack-icon-url"] = "flag.notifications"
	covered["notification-msteams-hook"] = "flag.notifications"
	covered["notification-gotify-url"] = "flag.notifications"
	covered["notification-gotify-token"] = "flag.notifications"
	covered["notification-gotify-tls-skip-verify"] = "flag.notifications"

	return covered
}

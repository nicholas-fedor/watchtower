package engine

import (
	"strconv"
	"time"
)

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

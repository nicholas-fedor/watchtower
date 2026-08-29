package engine

import (
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

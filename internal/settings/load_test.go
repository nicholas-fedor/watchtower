package settings

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/git/semver"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/docker"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/git"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/registry"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/update"
	"github.com/nicholas-fedor/watchtower/internal/settings/flags/spec"
)

// newNopLogger returns a discarded logger for Load and validate tests.
//
// Returns:
//   - *zerolog.Logger: Nop logger.
func newNopLogger() *zerolog.Logger {
	log := zerolog.Nop()

	return &log
}

// parsedValues returns a reader of a command's flags parsed from args, with
// every flag registered as in production.
//
// Parameters:
//   - t: Test handle.
//   - args: CLI arguments after the command name.
//
// Returns:
//   - *flagValues: Reader of the parsed persistent flags.
func parsedValues(t *testing.T, args ...string) *flagValues {
	t.Helper()

	return &flagValues{flags: newParsedCommand(t, args...).PersistentFlags()}
}

func TestLoad_DefaultsAndAlignment(t *testing.T) {
	cmd := newParsedCommand(t)

	cfg, _, err := Load(newNopLogger(), cmd, []string{"/Foo", "Bar"})
	require.NoError(t, err)

	assert.Equal(t, "unix:///var/run/docker.sock", cfg.Docker.Host)
	assert.False(t, cfg.Docker.TLSVerify)
	assert.Equal(t, "auto", cfg.Compatibility.CPUCopyMode)
	assert.False(t, cfg.Compatibility.DisableMemorySwappiness)
	assert.Equal(t, 30*time.Second, cfg.Update.StopTimeout)
	assert.Equal(t, []string{"Foo", "Bar"}, cfg.Filter.Names)
	assert.NotNil(t, cfg.Filter.Predicate)
	assert.Equal(t, "auto", cfg.Logging.Format)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.False(t, cfg.Git.Enable)
	assert.Equal(t, git.DefaultRef, cfg.Git.DefaultRef)
	assert.Equal(t, git.DefaultPolicy, cfg.Git.SemverPolicy)
}

// TestLoad_PreparesFlags verifies that Load prepares the flags itself, so a
// caller that only parses them still gets the environment, the alias flags
// and the secret files applied.
func TestLoad_PreparesFlags(t *testing.T) {
	tokenFile := writeSecretFile(t, "s3cret\n")

	cfg := newLoadedCommand(t,
		map[string]string{"WATCHTOWER_CLEANUP": "true"},
		"--interval", "30", "--http-api-token", tokenFile)

	assert.True(t, cfg.Update.Cleanup, "the environment sets an unset flag")
	assert.Equal(t, "@every 30s", cfg.Schedule.Spec, "the interval alias sets the schedule")
	assert.Equal(t, "s3cret", cfg.API.Token, "the secret file is read into its flag")
}

func TestLoad_APIVersionQuotesStripped(t *testing.T) {
	cmd := newParsedCommand(t, "--api-version", `"1.44"`)

	cfg, _, err := Load(newNopLogger(), cmd, nil)
	require.NoError(t, err)
	assert.Equal(t, "1.44", cfg.Docker.APIVersion)
}

// TestLoad_CompatReachesClientOptions verifies the compatibility flags reach
// the Docker client options through their single home in Compatibility.
func TestLoad_CompatReachesClientOptions(t *testing.T) {
	cmd := newParsedCommand(t, "--cpu-copy-mode", "full", "--disable-memory-swappiness")

	cfg, _, err := Load(newNopLogger(), cmd, nil)
	require.NoError(t, err)
	assert.Equal(t, "full", cfg.Compatibility.CPUCopyMode)
	assert.True(t, cfg.Compatibility.DisableMemorySwappiness)

	opts := cfg.ClientOptions()
	assert.Equal(t, "full", opts.CPUCopyMode)
	assert.True(t, opts.DisableMemorySwappiness)
}

func TestLoad_RollingRestartWithMonitorOnly(t *testing.T) {
	cmd := newParsedCommand(t, "--rolling-restart", "--monitor-only")

	_, _, err := Load(newNopLogger(), cmd, nil)
	require.ErrorIs(t, err, errRollingRestartWithMonitorOnly)
}

func TestLoad_MonitorOnlyWithNoPullSucceeds(t *testing.T) {
	cmd := newParsedCommand(t, "--monitor-only", "--no-pull")

	cfg, _, err := Load(newNopLogger(), cmd, nil)
	require.NoError(t, err)
	assert.True(t, cfg.Update.MonitorOnly)
	assert.True(t, cfg.Update.NoPull)
}

func TestLoad_GitAndComposeErrors(t *testing.T) {
	t.Run("invalid git-image", func(t *testing.T) {
		cmd := newParsedCommand(t, "--git-image", "not-a-mapping")
		_, _, err := Load(newNopLogger(), cmd, nil)
		require.ErrorIs(t, err, git.ErrInvalidGitImage)
	})

	t.Run("invalid compose-project", func(t *testing.T) {
		cmd := newParsedCommand(t, "--compose-project", "nopair")
		_, _, err := Load(newNopLogger(), cmd, nil)
		require.ErrorIs(t, err, git.ErrInvalidComposeProject)
	})

	t.Run("missing ca bundle", func(t *testing.T) {
		cmd := newParsedCommand(t, "--git-ca-bundle", filepath.Join(t.TempDir(), "missing.pem"))
		_, _, err := Load(newNopLogger(), cmd, nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "git-ca-bundle:")
	})
}

func TestLoad_InvalidFilterLabel(t *testing.T) {
	cmd := newParsedCommand(t, "--enable-containers-by-label", "not-a-pair")

	_, _, err := Load(newNopLogger(), cmd, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "build filter:")
}

// TestLoad_NegativeDurations verifies that a negative value for any duration
// setting stops Load with an error naming the setting. The value comes from a
// flag or an environment variable. Environment values cover both a unit
// suffix and a bare number of seconds. The test runs sequentially because the
// environment cases set variables.
func TestLoad_NegativeDurations(t *testing.T) {
	tests := []struct {
		name    string
		setting string
		args    []string
		env     map[string]string
	}{
		{name: "stop-timeout flag", setting: "stop-timeout", args: []string{"--stop-timeout", "-1s"}},
		{
			name:    "stop-timeout env in bare seconds",
			setting: "stop-timeout",
			env:     map[string]string{"WATCHTOWER_TIMEOUT": "-10"},
		},
		{name: "git-timeout flag", setting: "git-timeout", args: []string{"--git-timeout", "-1m"}},
		{
			name:    "git-timeout env",
			setting: "git-timeout",
			env:     map[string]string{"WATCHTOWER_GIT_TIMEOUT": "-30s"},
		},
		{
			name:    "http-api-check-timeout flag",
			setting: "http-api-check-timeout",
			args:    []string{"--http-api-check-timeout", "-5m"},
		},
		{
			name:    "http-api-update-timeout env in bare seconds",
			setting: "http-api-update-timeout",
			env:     map[string]string{"WATCHTOWER_HTTP_API_UPDATE_TIMEOUT": "-600"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnv(t, tt.env, tt.args...)
			require.ErrorContains(t, err, tt.setting+" must be non-negative, got -")
		})
	}
}

// TestLoad_SettingRanges verifies that every integer and duration setting is
// range-checked when the configuration loads. Each setting is set just below
// its minimum and, when it has one, just above its maximum. The minimum itself
// loads. A new setting is covered without a test of its own. The spec package
// tests the messages.
func TestLoad_SettingRanges(t *testing.T) {
	for _, flagSpec := range AllSpecs() {
		var accepted, rejected []string

		switch flagSpec.Kind {
		case spec.KindInt:
			accepted = []string{strconv.Itoa(flagSpec.Min)}
			rejected = []string{strconv.Itoa(flagSpec.Min - 1)}

			if flagSpec.Max != 0 {
				accepted = append(accepted, strconv.Itoa(flagSpec.Max))
				rejected = append(rejected, strconv.Itoa(flagSpec.Max+1))
			}
		case spec.KindDuration:
			accepted = []string{"0s"}
			rejected = []string{"-1s"}
		case spec.KindBool, spec.KindString, spec.KindStringSlice, spec.KindStringArray:
			continue
		default:
			t.Fatalf("flag %q has unknown kind %d", flagSpec.Name, flagSpec.Kind)
		}

		t.Run(flagSpec.Name, func(t *testing.T) {
			for _, value := range accepted {
				_, err := loadWithEnv(t, nil, "--"+flagSpec.Name+"="+value)
				require.NoError(t, err, "value %s", value)
			}

			for _, value := range rejected {
				_, err := loadWithEnv(t, nil, "--"+flagSpec.Name+"="+value)
				require.ErrorContains(t, err, flagSpec.Name+" must be", "value %s", value)
				require.ErrorContains(t, err, "got "+value)
			}
		})
	}
}

// TestLoad_ZeroRateLimit verifies that an HTTP API rate limit of zero stops
// Load. Zero has no defined meaning. The rate limiter would replace it with
// its own default of 5 requests per minute.
func TestLoad_ZeroRateLimit(t *testing.T) {
	_, err := loadWithEnv(t, nil, "--http-api-rate-limit", "0")
	require.ErrorContains(t, err, "http-api-rate-limit must be at least 1, got 0")
}

func TestLoad_InvalidAndNegativeCooldown(t *testing.T) {
	t.Run("invalid duration", func(t *testing.T) {
		cmd := newParsedCommand(t, "--cooldown-delay", "not-a-duration")
		_, _, err := Load(newNopLogger(), cmd, nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "cooldown-delay:")
	})

	t.Run("negative duration", func(t *testing.T) {
		cmd := newParsedCommand(t, "--cooldown-delay", "-1h")
		_, _, err := Load(newNopLogger(), cmd, nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "cooldown-delay:")
	})
}

// TestLoadDocker checks the Docker connection settings loadDocker reads from the
// flags and the environment. An https:// host becomes tcp://. With TLS
// verification on, a host that cannot use TLS logs a warning.
func TestLoadDocker(t *testing.T) {
	const defaultHost = "unix:///var/run/docker.sock"

	testCases := []struct {
		name        string
		env         map[string]string
		flags       []string
		register    func(*cobra.Command)
		want        docker.Docker
		wantErr     string
		wantWarning string
	}{
		{
			name: "defaults",
			want: docker.Docker{Host: defaultHost},
		},
		{
			name: "flags",
			flags: []string{
				"-H", "tcp://127.0.0.1:2375", "--tlsverify", "--api-version", `"1.43"`, "--cert-path", "/certs",
			},
			want: docker.Docker{Host: "tcp://127.0.0.1:2375", TLSVerify: true, APIVersion: "1.43", CertPath: "/certs"},
		},
		{
			name: "environment",
			env: map[string]string{
				"DOCKER_HOST":        "tcp://example.com:2376",
				"DOCKER_TLS_VERIFY":  "1",
				"DOCKER_API_VERSION": "1.41",
				"DOCKER_CERT_PATH":   "/env/certs",
			},
			want: docker.Docker{Host: "tcp://example.com:2376", TLSVerify: true, APIVersion: "1.41", CertPath: "/env/certs"},
		},
		{
			name:  "host without a scheme keeps its form",
			flags: []string{"--host", "some-custom-docker-host", "--tlsverify", "--cert-path", "/certs"},
			want:  docker.Docker{Host: "some-custom-docker-host", TLSVerify: true, CertPath: "/certs"},
		},
		{
			name: "tcp host without tls verification keeps its scheme",
			env:  map[string]string{"DOCKER_HOST": "tcp://example.com:2375"},
			want: docker.Docker{Host: "tcp://example.com:2375"},
		},
		{
			name: "https host with tls verification",
			env: map[string]string{
				"DOCKER_HOST":       "https://example.com:2376",
				"DOCKER_TLS_VERIFY": "1",
				"DOCKER_CERT_PATH":  "/env/certs",
			},
			want: docker.Docker{Host: "tcp://example.com:2376", TLSVerify: true, CertPath: "/env/certs"},
		},
		{
			name: "https host without tls verification",
			env:  map[string]string{"DOCKER_HOST": "https://example.com:2376"},
			want: docker.Docker{Host: "tcp://example.com:2376"},
		},
		{
			name: "false tls verify env var",
			env:  map[string]string{"DOCKER_TLS_VERIFY": "0"},
			want: docker.Docker{Host: defaultHost},
		},
		{
			name:  "tlsverify flag false overrides env var",
			env:   map[string]string{"DOCKER_TLS_VERIFY": "1"},
			flags: []string{"--tlsverify=false"},
			want:  docker.Docker{Host: defaultHost},
		},
		{
			name: "empty flags override env vars",
			env: map[string]string{
				"DOCKER_HOST":        "tcp://example.com:2375",
				"DOCKER_API_VERSION": "1.41",
				"DOCKER_CERT_PATH":   "/env/certs",
			},
			flags: []string{"--host=", "--api-version=", "--cert-path="},
			want:  docker.Docker{},
		},
		{
			// The exit path reads the Docker settings after a failed Load, so
			// other settings, such as an invalid log format, must not stop it.
			name: "invalid log format env var is not read",
			env:  map[string]string{"DOCKER_HOST": "tcp://socket-proxy:2375", "WATCHTOWER_LOG_FORMAT": "bogus"},
			want: docker.Docker{Host: "tcp://socket-proxy:2375"},
		},
		{
			name: "empty api version env var",
			env:  map[string]string{"DOCKER_API_VERSION": ""},
			want: docker.Docker{Host: defaultHost},
		},
		{
			name:        "http host with tls verification warns",
			env:         map[string]string{"DOCKER_TLS_VERIFY": "1", "DOCKER_CERT_PATH": "/env/certs"},
			flags:       []string{"--host", "http://example.com"},
			want:        docker.Docker{Host: "http://example.com", TLSVerify: true, CertPath: "/env/certs"},
			wantWarning: "TLS verification is enabled but DOCKER_HOST uses insecure scheme 'http://'. Consider using 'tcp://' or disable TLS verification.",
		},
		{
			name:        "unix host with tls verification warns",
			env:         map[string]string{"DOCKER_TLS_VERIFY": "1"},
			want:        docker.Docker{Host: defaultHost, TLSVerify: true},
			wantWarning: "TLS verification is enabled but DOCKER_HOST uses local socket 'unix://'. TLS is not applicable for local sockets. Consider disabling TLS verification.",
		},
		{
			// Without a certificate path the client builds no TLS
			// configuration and would send plain HTTP to the daemon.
			name:    "tls verification without a certificate path fails",
			env:     map[string]string{"DOCKER_HOST": "tcp://example.com:2376", "DOCKER_TLS_VERIFY": "1"},
			wantErr: "tlsverify requires cert-path for a remote Docker host",
		},
		{
			name:    "https host with tls verification and no certificate path fails",
			env:     map[string]string{"DOCKER_HOST": "https://example.com:2376", "DOCKER_TLS_VERIFY": "1"},
			wantErr: "tlsverify requires cert-path for a remote Docker host",
		},
		{
			// Startup rejects this value, so the exit path must not read it as false.
			name:    "unparsable tls verify env var fails",
			env:     map[string]string{"DOCKER_TLS_VERIFY": "yes"},
			wantErr: `tlsverify: parse bool: strconv.ParseBool: parsing "yes": invalid syntax`,
		},
		{
			name:     "unregistered flags fail",
			register: func(*cobra.Command) {},
			wantErr:  "read host:",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"DOCKER_HOST", "DOCKER_TLS_VERIFY", "DOCKER_API_VERSION", "DOCKER_CERT_PATH"} {
				t.Setenv(key, "")
				require.NoError(t, os.Unsetenv(key))
			}

			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			cmd := new(cobra.Command)
			if tc.register != nil {
				tc.register(cmd)
			} else {
				RegisterAll(cmd)
			}

			require.NoError(t, cmd.ParseFlags(tc.flags))

			var logOutput bytes.Buffer

			got, err := LoadDocker(testLoggerAt(&logOutput, logging.InfoLevel), cmd)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)

			if tc.wantWarning != "" {
				assert.Contains(t, logOutput.String(), tc.wantWarning)
			} else {
				assert.Empty(t, logOutput.String())
			}
		})
	}
}

func TestLoadClient(t *testing.T) {
	values := parsedValues(t, "--include-stopped", "--include-restarting", "--revive-stopped", "--remove-volumes", "--warn-on-head-failure", "always")

	got := loadClient(values)
	assert.True(t, got.IncludeStopped)
	assert.True(t, got.IncludeRestarting)
	assert.True(t, got.ReviveStopped)
	assert.True(t, got.RemoveVolumes)
	assert.Equal(t, "always", got.WarnOnHeadFailure)
}

func TestLoadCompat(t *testing.T) {
	values := parsedValues(t, "--disable-memory-swappiness", "--cpu-copy-mode", "none")

	got := loadCompat(values)
	assert.True(t, got.DisableMemorySwappiness)
	assert.Equal(t, "none", got.CPUCopyMode)
}

func TestLoadSchedule(t *testing.T) {
	values := parsedValues(t, "--interval", "120", "--schedule", "0 0 * * *", "--update-on-start")

	got := loadSchedule(values)
	assert.Equal(t, 120, got.IntervalSeconds)
	assert.Equal(t, "0 0 * * *", got.Spec)
	assert.True(t, got.UpdateOnStart)
}

func TestLoadMode(t *testing.T) {
	values := parsedValues(t, "--run-once", "--health-check", "--porcelain", "v1", "--self-update-orchestrator", "--no-startup-message")

	got := loadMode(values)
	assert.True(t, got.RunOnce)
	assert.True(t, got.HealthCheck)
	assert.Equal(t, "v1", got.Porcelain)
	assert.True(t, got.SelfUpdateOrchestrator)
	assert.True(t, got.NoStartupMessage)
}

func TestLoadUpdate(t *testing.T) {
	t.Run("flags", func(t *testing.T) {
		values := parsedValues(t,
			"--cleanup", "--no-pull", "--no-restart", "--monitor-only", "--rolling-restart",
			"--stop-timeout", "45s", "--cooldown-delay", "2h",
			"--use-compose-depends-on", "--label-take-precedence", "--ephemeral-self-update",
		)

		got, err := loadUpdate(newNopLogger(), values)
		require.NoError(t, err)
		assert.True(t, got.Cleanup)
		assert.True(t, got.NoPull)
		assert.True(t, got.NoRestart)
		assert.True(t, got.MonitorOnly)
		assert.True(t, got.RollingRestart)
		assert.Equal(t, 45*time.Second, got.StopTimeout)
		assert.Equal(t, 2*time.Hour, got.CooldownDelay)
		assert.True(t, got.UseComposeDependsOn)
		assert.True(t, got.LabelPrecedence)
		assert.True(t, got.EphemeralSelfUpdate)
	})

	t.Run("empty cooldown", func(t *testing.T) {
		values := parsedValues(t)
		got, err := loadUpdate(newNopLogger(), values)
		require.NoError(t, err)
		assert.Equal(t, time.Duration(0), got.CooldownDelay)
	})

	t.Run("subsecond timeout", func(t *testing.T) {
		values := parsedValues(t, "--stop-timeout", "500ms")
		got, err := loadUpdate(newNopLogger(), values)
		require.NoError(t, err)
		assert.Equal(t, 500*time.Millisecond, got.StopTimeout)
	})

	t.Run("invalid cooldown", func(t *testing.T) {
		values := parsedValues(t, "--cooldown-delay", "nope")
		_, err := loadUpdate(newNopLogger(), values)
		require.Error(t, err)
		assert.ErrorContains(t, err, "cooldown-delay:")
	})

	t.Run("negative cooldown", func(t *testing.T) {
		// units.ParseDuration rejects a leading '-'.
		values := parsedValues(t, "--cooldown-delay", "-2d")
		_, err := loadUpdate(newNopLogger(), values)
		require.Error(t, err)
		assert.ErrorContains(t, err, "cooldown-delay:")
	})
}

func TestLoadLifecycle(t *testing.T) {
	values := parsedValues(t, "--enable-lifecycle-hooks", "--lifecycle-uid", "1000", "--lifecycle-gid", "1001")

	got := loadLifecycle(values)
	assert.True(t, got.Enabled)
	assert.Equal(t, 1000, got.UID)
	assert.Equal(t, 1001, got.GID)
}

// TestNormalizedStringSlice verifies that normalizedStringSlice applies the
// transform to each element of a list flag and reads an unset list as empty.
func TestNormalizedStringSlice(t *testing.T) {
	t.Parallel()

	t.Run("lowercases values", func(t *testing.T) {
		t.Parallel()

		values := parsedValues(t, "--disable-containers", "/Foo,Bar")

		got := normalizedStringSlice(values, "disable-containers", strings.ToLower)
		assert.Equal(t, []string{"/foo", "bar"}, got)
	})

	t.Run("empty when unset", func(t *testing.T) {
		t.Parallel()

		values := parsedValues(t)

		got := normalizedStringSlice(values, "disable-containers", strings.ToLower)
		assert.Empty(t, got)
	})
}

func TestLoadFilter(t *testing.T) {
	t.Run("names and lists", func(t *testing.T) {
		values := parsedValues(t,
			"--label-enable",
			"--disable-containers", "/SkipMe",
			"--monitor-image-names", "nginx*",
			"--skip-image-names", "redis*",
			"--enable-containers-by-label", "env=prod",
			"--disable-containers-by-label", "skip=true",
			"--scope", "prod",
		)

		got, err := loadFilter(newNopLogger(), values, []string{"/App"})
		require.NoError(t, err)
		assert.True(t, got.LabelEnable)
		assert.Equal(t, []string{"SkipMe"}, got.DisableContainers)
		assert.Equal(t, []string{"nginx*"}, got.MonitorImageNames)
		assert.Equal(t, []string{"redis*"}, got.SkipImageNames)
		assert.Equal(t, []string{"env=prod"}, got.EnableContainersByLabel)
		assert.Equal(t, []string{"skip=true"}, got.DisableContainersByLabel)
		assert.Equal(t, "prod", got.Scope)
		assert.Equal(t, []string{"App"}, got.Names)
		assert.NotNil(t, got.Predicate)
		assert.NotEmpty(t, got.Desc)
	})

	t.Run("invalid label pair", func(t *testing.T) {
		values := parsedValues(t, "--disable-containers-by-label", "broken")
		_, err := loadFilter(newNopLogger(), values, nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "build filter:")
	})
}

// TestLoadRegistry verifies that loadRegistry reads the TLS settings from
// their flags and the cooldown platform from its environment variables, and
// leaves an unset platform empty so the registry client uses the runtime
// platform.
func TestLoadRegistry(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		t.Setenv("WATCHTOWER_COOLDOWN_PLATFORM_OS", "windows")
		t.Setenv("WATCHTOWER_COOLDOWN_PLATFORM_ARCH", "arm")
		t.Setenv("WATCHTOWER_COOLDOWN_PLATFORM_VARIANT", "v7")

		values := parsedValues(t, "--registry-tls-skip", "--registry-tls-min-version", "1.3")

		assert.Equal(t, registry.Registry{
			CooldownPlatform: registry.Platform{OS: "windows", Architecture: "arm", Variant: "v7"},
			TLSMinVersion:    "1.3",
			TLSSkip:          true,
		}, loadRegistry(values))
	})

	t.Run("unset", func(t *testing.T) {
		for _, key := range []string{
			"WATCHTOWER_COOLDOWN_PLATFORM_OS",
			"WATCHTOWER_COOLDOWN_PLATFORM_ARCH",
			"WATCHTOWER_COOLDOWN_PLATFORM_VARIANT",
		} {
			t.Setenv(key, "")
			require.NoError(t, os.Unsetenv(key))
		}

		values := parsedValues(t)

		assert.Equal(t, registry.Registry{TLSMinVersion: "TLS1.2"}, loadRegistry(values))
	})
}

func TestLoadAPI(t *testing.T) {
	t.Run("all fields", func(t *testing.T) {
		values := parsedValues(t,
			"--http-api-endpoints", "health,update",
			"--http-api-update", "--http-api-metrics", "--http-api-containers",
			"--http-api-host", "127.0.0.1", "--http-api-port", "8080",
			"--http-api-token", "tok", "--http-api-events-token", "ev",
			"--http-api-periodic-polls", "--http-api-rate-limit", "10",
			"--http-api-tls-cert", "cert.pem", "--http-api-tls-key", "key.pem",
			"--http-api-trusted-proxies", "10.0.0.0/8,172.16.0.0/12",
			"--http-api-proxy-header", "X-Real-IP",
			"--http-api-cors-origins", "https://app.example.com",
			"--http-api-check-timeout", "5s", "--http-api-update-timeout", "10s",
		)

		got := loadAPI(values)
		assert.Equal(t, []string{"health", "update"}, got.Endpoints)
		assert.True(t, got.LegacyUpdate)
		assert.True(t, got.LegacyMetrics)
		assert.True(t, got.LegacyContainers)
		assert.Equal(t, "127.0.0.1", got.Host)
		assert.True(t, got.HostChanged)
		assert.Equal(t, "8080", got.Port)
		assert.True(t, got.PortChanged)
		assert.Equal(t, "tok", got.Token)
		assert.Equal(t, "ev", got.EventsToken)
		assert.True(t, got.PeriodicPolls)
		assert.Equal(t, 10, got.RateLimit)
		assert.True(t, got.RateLimitChanged)
		assert.Equal(t, "cert.pem", got.TLSCert)
		assert.Equal(t, "key.pem", got.TLSKey)
		assert.Equal(t, []string{"10.0.0.0/8", "172.16.0.0/12"}, got.TrustedProxies)
		assert.Equal(t, "X-Real-IP", got.ProxyHeader)
		assert.Equal(t, []string{"https://app.example.com"}, got.CORSOrigins)
		assert.Equal(t, 5*time.Second, got.CheckTimeout)
		assert.True(t, got.CheckTimeoutChanged)
		assert.Equal(t, 10*time.Second, got.UpdateTimeout)
		assert.True(t, got.UpdateTimeoutChanged)
	})

	t.Run("unchanged flags", func(t *testing.T) {
		values := parsedValues(t)

		got := loadAPI(values)
		assert.False(t, got.HostChanged)
		assert.False(t, got.PortChanged)
		assert.False(t, got.RateLimitChanged)
		assert.False(t, got.CheckTimeoutChanged)
		assert.False(t, got.UpdateTimeoutChanged)
		assert.Empty(t, got.Endpoints)
		assert.Empty(t, got.TrustedProxies)
		assert.Empty(t, got.CORSOrigins)
	})
}

func TestLoadNotify(t *testing.T) {
	values := parsedValues(t,
		"--notification-url", "gotify://example/token",
		"--notifications", "email,slack",
		"--notifications-level", "debug",
		"--notification-template", "{{.Title}}",
		"--notification-template-file", "/tpl.tmpl",
		"--notification-report",
		"--notification-split-by-container",
		"--notification-skip-title",
		"--notification-log-stdout",
		"--notifications-delay", "7",
		"--notifications-hostname", "box",
		"--notification-title-tag", "prod",
		"--notification-email-subjecttag", "mail",
		"--notification-email-from", "from@example.com",
		"--notification-email-to", "to@example.com",
		"--notification-email-server", "smtp.example.com",
		"--notification-email-server-user", "user",
		"--notification-email-server-password", "pw",
		"--notification-email-server-port", "587",
		"--notification-email-server-tls-skip-verify",
		"--notification-email-delay", "3",
		"--notification-slack-hook-url", "https://hooks.slack.example/x",
		"--notification-slack-identifier", "wt",
		"--notification-slack-channel", "#ops",
		"--notification-slack-icon-emoji", ":bell:",
		"--notification-slack-icon-url", "https://example.com/icon.png",
		"--notification-msteams-hook", "https://teams.example/hook",
		"--notification-gotify-url", "https://gotify.example",
		"--notification-gotify-token", "g-tok",
		"--notification-gotify-tls-skip-verify",
	)

	got := loadNotify(values)
	assert.Equal(t, []string{"gotify://example/token"}, got.URLs)
	assert.Equal(t, []string{"email", "slack"}, got.LegacyTypes)
	assert.Equal(t, "debug", got.Level)
	assert.Equal(t, "{{.Title}}", got.Template)
	assert.Equal(t, "/tpl.tmpl", got.TemplateFile)
	assert.True(t, got.Report)
	assert.True(t, got.SplitByContainer)
	assert.True(t, got.SkipTitle)
	assert.True(t, got.LogStdout)
	assert.Equal(t, 7, got.DelaySeconds)
	assert.Equal(t, "box", got.Hostname)
	assert.Equal(t, "prod", got.TitleTag)
	assert.Equal(t, "mail", got.EmailSubjectTag)
	assert.Equal(t, "from@example.com", got.Legacy.EmailFrom)
	assert.Equal(t, "to@example.com", got.Legacy.EmailTo)
	assert.Equal(t, "smtp.example.com", got.Legacy.EmailServer)
	assert.Equal(t, "user", got.Legacy.EmailUser)
	assert.Equal(t, "pw", got.Legacy.EmailPassword)
	assert.Equal(t, 587, got.Legacy.EmailPort)
	assert.True(t, got.Legacy.EmailTLSSkipVerify)
	assert.Equal(t, 3, got.Legacy.EmailDelay)
	assert.Equal(t, "https://hooks.slack.example/x", got.Legacy.SlackHookURL)
	assert.Equal(t, "wt", got.Legacy.SlackIdentifier)
	assert.Equal(t, "#ops", got.Legacy.SlackChannel)
	assert.Equal(t, ":bell:", got.Legacy.SlackIconEmoji)
	assert.Equal(t, "https://example.com/icon.png", got.Legacy.SlackIconURL)
	assert.Equal(t, "https://teams.example/hook", got.Legacy.MSTeamsHook)
	assert.Equal(t, "https://gotify.example", got.Legacy.GotifyURL)
	assert.Equal(t, "g-tok", got.Legacy.GotifyToken)
	assert.True(t, got.Legacy.GotifyTLSSkipVerify)
}

func TestLoadLogging(t *testing.T) {
	t.Run("explicit format", func(t *testing.T) {
		values := parsedValues(t, "--log-format", "json", "--log-level", "debug", "--debug", "--trace", "--no-color")
		got := loadLogging(values)
		assert.Equal(t, "json", got.Format)
		assert.Equal(t, "debug", got.Level)
		assert.True(t, got.Debug)
		assert.True(t, got.Trace)
		assert.True(t, got.NoColor)
	})

	t.Run("empty format becomes auto", func(t *testing.T) {
		values := parsedValues(t, "--log-format=", "--log-level", "warn")
		got := loadLogging(values)
		assert.Equal(t, "auto", got.Format)
		assert.Equal(t, "warn", got.Level)
		assert.False(t, got.NoColor)
	})
}

// TestLoad_NoColorFromEnvPresence verifies that Config.Logging.NoColor follows
// the presence rule for NO_COLOR (https://no-color.org/), where any value,
// including empty, "0", and "false", disables color, and that a CLI value wins.
func TestLoad_NoColorFromEnvPresence(t *testing.T) {
	for _, value := range []string{"", "0", "false", "1"} {
		t.Run("NO_COLOR="+value, func(t *testing.T) {
			cfg := newLoadedCommand(t, map[string]string{"NO_COLOR": value})
			assert.True(t, cfg.Logging.NoColor)
		})
	}

	t.Run("unset", func(t *testing.T) {
		// t.Setenv restores the original NO_COLOR state at cleanup, and
		// Unsetenv removes it, since even an empty value counts as present.
		t.Setenv("NO_COLOR", "")
		require.NoError(t, os.Unsetenv("NO_COLOR"))

		cfg := newLoadedCommand(t, nil)
		assert.False(t, cfg.Logging.NoColor)
	})

	t.Run("flag overrides env", func(t *testing.T) {
		cfg := newLoadedCommand(t, map[string]string{"NO_COLOR": "1"}, "--no-color=false")
		assert.False(t, cfg.Logging.NoColor)
	})
}

func TestLoadGit(t *testing.T) {
	t.Run("flags", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "ca.pem")
		require.NoError(t, os.WriteFile(bundle, []byte("pem"), 0o600))

		values := parsedValues(t,
			"--git-enable",
			"--git-image", "myapp:latest=https://github.com/org/app.git#main@minor",
			"--git-timeout", "45s",
			"--git-auth-token", "tok",
			"--git-username", "user",
			"--git-password", "pw",
			"--git-ssh-key-path", "/key",
			"--git-ssh-known-hosts", "/known",
			"--git-ca-bundle", bundle,
			"--git-insecure-skip-tls",
			"--git-dockerfile", "  build/docker/Dockerfile  ",
			"--git-context", "  .  ",
			"--git-compose-stash",
			"--compose-project", "web=/srv/web",
		)

		got, err := loadGit(values)
		require.NoError(t, err)
		assert.True(t, got.Enable)
		assert.Equal(t, git.DefaultRef, got.DefaultRef)
		assert.Equal(t, git.DefaultPolicy, got.SemverPolicy)
		assert.Equal(t, 45*time.Second, got.Timeout)
		assert.Equal(t, "tok", got.Token)
		assert.Equal(t, "user", got.Username)
		assert.Equal(t, "pw", got.Password)
		assert.Equal(t, "/key", got.SSHKeyPath)
		assert.Equal(t, "/known", got.SSHKnownHosts)
		assert.Equal(t, []byte("pem"), got.CABundle)
		assert.True(t, got.InsecureSkipTLS)
		assert.Equal(t, "build/docker/Dockerfile", got.Dockerfile)
		assert.Equal(t, ".", got.Context)
		assert.Equal(t, semver.PolicyMinor, got.Images["myapp:latest"].Policy)
		assert.True(t, got.ComposeStash)
		assert.Equal(t, "/srv/web", got.ComposeProjects["web"])
	})

	t.Run("defaults", func(t *testing.T) {
		values := parsedValues(t)
		got, err := loadGit(values)
		require.NoError(t, err)
		assert.False(t, got.Enable)
		assert.Equal(t, git.DefaultRef, got.DefaultRef)
		assert.Equal(t, git.DefaultPolicy, got.SemverPolicy)
		assert.Empty(t, got.Images)
		assert.Empty(t, got.ComposeProjects)
		assert.Nil(t, got.CABundle)
	})

	t.Run("invalid image", func(t *testing.T) {
		values := parsedValues(t, "--git-image", "nope")
		_, err := loadGit(values)
		require.ErrorIs(t, err, git.ErrInvalidGitImage)
		assert.ErrorContains(t, err, "git-image:")
	})

	t.Run("invalid compose project", func(t *testing.T) {
		values := parsedValues(t, "--compose-project", "nopair")
		_, err := loadGit(values)
		require.ErrorIs(t, err, git.ErrInvalidComposeProject)
		assert.ErrorContains(t, err, "compose-project:")
	})

	t.Run("missing ca bundle", func(t *testing.T) {
		values := parsedValues(t, "--git-ca-bundle", filepath.Join(t.TempDir(), "missing.pem"))
		_, err := loadGit(values)
		require.Error(t, err)
		assert.ErrorContains(t, err, "git-ca-bundle:")
	})
}

func TestReadOptionalFile(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got, err := readOptionalFile("  ")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("reads file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ca.pem")
		require.NoError(t, os.WriteFile(path, []byte("cert"), 0o600))
		got, err := readOptionalFile(path)
		require.NoError(t, err)
		assert.Equal(t, []byte("cert"), got)
	})

	t.Run("missing", func(t *testing.T) {
		_, err := readOptionalFile(filepath.Join(t.TempDir(), "missing"))
		require.Error(t, err)
	})
}

func TestValidate(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		require.NoError(t, validate(newNopLogger(), Config{}))
	})

	t.Run("rolling and monitor-only", func(t *testing.T) {
		err := validate(newNopLogger(), Config{
			Update: update.Update{
				RollingRestart: true,
				MonitorOnly:    true,
			},
		})
		require.ErrorIs(t, err, errRollingRestartWithMonitorOnly)
	})

	t.Run("monitor-only and no-pull warns", func(t *testing.T) {
		err := validate(newNopLogger(), Config{
			Update: update.Update{
				MonitorOnly: true,
				NoPull:      true,
			},
		})
		require.NoError(t, err)
	})
}

// Test_flagValues_reads verifies that flagValues reads each kind of setting
// from the prepared flags, which hold the command-line value or, for a flag
// the command line left unset, the environment value.
func Test_flagValues_reads(t *testing.T) {
	t.Setenv("WATCHTOWER_CLEANUP", "true")
	t.Setenv("WATCHTOWER_SCOPE", "env-scope")
	t.Setenv("WATCHTOWER_HTTP_API_CHECK_TIMEOUT", "7s")
	t.Setenv("WATCHTOWER_DISABLE_CONTAINERS", "db")
	t.Setenv("WATCHTOWER_POLL_INTERVAL", "010")

	cmd := newTestCommand()
	parseWithEnv(t, cmd, "--scope", "cli-scope", "--disable-containers", "web")

	values := &flagValues{flags: cmd.PersistentFlags()}

	assert.True(t, values.Bool("cleanup"), "the environment fills an unset flag")
	assert.Equal(t, "cli-scope", values.String("scope"), "the command line wins")
	assert.Equal(t, 7*time.Second, values.Duration("http-api-check-timeout"))
	assert.Equal(t, []string{"web"}, values.Strings("disable-containers"))
	assert.Equal(t, 10, values.Int("interval"), "an environment integer is decimal")
	require.NoError(t, values.err)
}

// Test_flagValues_Strings_emptyWhenUnset verifies that an unset list reads as
// an empty list rather than nil.
func Test_flagValues_Strings_emptyWhenUnset(t *testing.T) {
	t.Parallel()

	values := parsedValues(t)

	got := values.Strings("notification-url")
	require.NotNil(t, got)
	assert.Empty(t, got)
}

// Test_flagValues_Strings_returnsCopy verifies that changing the returned list
// leaves the flag's own value alone, since callers normalize the elements in
// place.
func Test_flagValues_Strings_returnsCopy(t *testing.T) {
	t.Parallel()

	values := parsedValues(t, "--disable-containers", "web")

	list := values.Strings("disable-containers")
	list[0] = "changed"

	assert.Equal(t, []string{"web"}, values.Strings("disable-containers"))
}

// Test_flagValues_failedRead verifies that reading a flag that is not
// registered, or not registered with the expected type, returns the zero value
// and keeps the first failure, so Load can report a misspelled setting name.
func Test_flagValues_failedRead(t *testing.T) {
	t.Parallel()

	values := parsedValues(t)

	assert.Empty(t, values.String("not-registered"))
	assert.False(t, values.Bool("scope"), "scope is a string flag")
	assert.Zero(t, values.Duration("cleanup"))
	assert.Zero(t, values.Int("scope"))
	assert.Equal(t, []string{}, values.Strings("scope"))

	require.Error(t, values.err)
	assert.ErrorContains(t, values.err, "read not-registered:")
}

// Test_flagValues_Strings_failedRead verifies which error a list read keeps
// for a flag that is missing or is not a list.
func Test_flagValues_Strings_failedRead(t *testing.T) {
	t.Parallel()

	flagSet := newParsedCommand(t).PersistentFlags()

	missing := &flagValues{flags: flagSet}
	assert.Equal(t, []string{}, missing.Strings("not-registered"))
	require.ErrorIs(t, missing.err, errFlagNotRegistered)

	notAList := &flagValues{flags: flagSet}
	assert.Equal(t, []string{}, notAList.Strings("scope"))
	require.ErrorIs(t, notAList.err, errUnsupportedFlagKind)
}

package image

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	registryConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/registry"
	updateConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/update"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/httpstub"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
)

// TestCooldownError verifies the two kinds of cooldown error. A deferral has no
// cause and matches ErrImageCooldown. A failed age lookup carries its cause and
// reads as it. It matches the cause instead of ErrImageCooldown.
func TestCooldownError(t *testing.T) {
	t.Parallel()

	cause := errors.New("registry unreachable")
	deferral := &CooldownError{Age: "2 hours", Delay: "1 day", Remaining: "22 hours"}
	failedLookup := &CooldownError{Delay: "1 day", Cause: cause}

	t.Run("a deferral reads as ErrImageCooldown", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, ErrImageCooldown.Error(), deferral.Error())
		require.ErrorIs(t, deferral, ErrImageCooldown)
		require.NoError(t, deferral.Unwrap())
	})

	t.Run("a deferral matches ErrImageCooldown through a wrap", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, fmt.Errorf("git cooldown: %w", deferral), ErrImageCooldown)
	})

	t.Run("a failed age lookup reads as its cause", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, cause.Error(), failedLookup.Error())
		require.ErrorIs(t, failedLookup, cause)
		require.NotErrorIs(t, failedLookup, ErrImageCooldown)
		assert.Equal(t, cause, failedLookup.Unwrap())
	})

	t.Run("neither matches an unrelated target", func(t *testing.T) {
		t.Parallel()

		unrelated := errors.New("unrelated")
		require.NotErrorIs(t, deferral, unrelated)
		require.NotErrorIs(t, failedLookup, unrelated)
	})

	t.Run("errors.As extracts the fields", func(t *testing.T) {
		t.Parallel()

		var cooldownErr *CooldownError
		require.ErrorAs(t, fmt.Errorf("wrapped: %w", deferral), &cooldownErr)
		assert.Equal(t, "2 hours", cooldownErr.Age)
		assert.Equal(t, "1 day", cooldownErr.Delay)
		assert.Equal(t, "22 hours", cooldownErr.Remaining)
	})
}

// notFoundRegistry starts a stub registry that answers every request with 404,
// so fetching an image's creation time from it fails.
//
// Parameters:
//   - t: Test that owns the registry.
//
// Returns:
//   - string: Host and port of the registry.
func notFoundRegistry(t *testing.T) string {
	t.Helper()

	server := httpstub.New(t)
	notFound := httpstub.RespondWith(http.StatusNotFound, "not found", nil)
	server.RouteToHandler(http.MethodGet, httpstub.PathRegexp(".*"), notFound)
	server.RouteToHandler(http.MethodHead, httpstub.PathRegexp(".*"), notFound)

	return server.Addr()
}

// createdRegistry starts a plain HTTP stub registry that serves the image
// org/app:1.0 as a single-platform manifest whose config blob records the
// given creation time. Every other path answers 404.
//
// Parameters:
//   - t: Test that owns the registry.
//   - created: Creation time the image config records.
//
// Returns:
//   - string: Host and port of the registry.
func createdRegistry(t *testing.T, created time.Time) string {
	t.Helper()

	const configDigest = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case "/v2/org/app/manifests/1.0":
			w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
			fmt.Fprintf(w, `{
				"schemaVersion": 2,
				"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
				"config": {"mediaType": "application/vnd.docker.container.image.v1+json", "digest": %q, "size": 2}
			}`, configDigest)
		case "/v2/org/app/blobs/" + configDigest:
			fmt.Fprintf(w, `{"created": %q}`, created.UTC().Format(time.RFC3339))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	return strings.TrimPrefix(server.URL, "http://")
}

// Test_isOutsideCooldown verifies that the cooldown gate lets a pull go ahead
// when no cooldown is configured, the delay is zero, or the container is
// monitor-only or no-pull, and that a failure to fetch the image's creation
// time holds the pull back with a CooldownError.
func Test_isOutsideCooldown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// when describes the configuration, and it the expected result.
		when, it string
		policy   updateConfig.Policy
	}{
		{when: "no cooldown delay is configured", it: "returns true with no registry calls", policy: updateConfig.Policy{}},
		{when: "cooldown delay is zero", it: "returns true immediately", policy: updateConfig.Policy{CooldownDelay: 0}},
		{
			when:   "container is monitor-only",
			it:     "returns true and bypasses the cooldown check",
			policy: updateConfig.Policy{MonitorOnly: true, CooldownDelay: 24 * time.Hour},
		},
		{
			when:   "container has no-pull enabled",
			it:     "returns true and bypasses the cooldown check",
			policy: updateConfig.Policy{NoPull: true, CooldownDelay: 24 * time.Hour},
		},
	}

	for _, tt := range tests {
		t.Run(tt.when, func(t *testing.T) {
			t.Parallel()

			t.Run(tt.it, func(t *testing.T) {
				t.Parallel()

				i := newTestImageClient(newImageAPI(t), newTestLogger(t), registryConfig.Registry{}, ratelimit.New())

				outside, err := i.isOutsideCooldown(t.Context(), modelfixture.Container(containerfixture.WithImageName("test:latest")), tt.policy)
				require.NoError(t, err)
				assert.True(t, outside)
			})
		})
	}

	t.Run("FetchImageCreationTime fails (registry manifest returns 404)", func(t *testing.T) {
		t.Parallel()

		t.Run("returns false with a CooldownError reflecting the age fetch failure", func(t *testing.T) {
			t.Parallel()

			// The test registry serves plain HTTP.
			i := newTestImageClient(newImageAPI(t), newTestLogger(t), registryConfig.Registry{TLSSkip: true}, ratelimit.New())

			outside, err := i.isOutsideCooldown(t.Context(), modelfixture.Container(containerfixture.WithImageName(notFoundRegistry(t)+"/test:latest")), updateConfig.Policy{
				CooldownDelay: 1 * time.Hour,
			})
			assert.False(t, outside)

			var cooldownErr *CooldownError
			require.ErrorAs(t, err, &cooldownErr)
		})
	})
}

// TestIsOutsideCooldown_NilLogger verifies that a client built without a
// logger runs a cooldown check without a nil pointer panic.
func TestIsOutsideCooldown_NilLogger(t *testing.T) {
	t.Parallel()

	// The test registry serves plain HTTP.
	client := NewClient(newImageAPI(t), nil, registryConfig.Registry{TLSSkip: true}, ratelimit.New())

	outside, err := client.isOutsideCooldown(
		t.Context(),
		modelfixture.Container(containerfixture.WithImageName(notFoundRegistry(t)+"/test:latest")),
		updateConfig.Policy{CooldownDelay: time.Hour},
	)
	assert.False(t, outside)

	var cooldownErr *CooldownError
	require.ErrorAs(t, err, &cooldownErr)
}

// Test_isOutsideCooldown_pullOptionsFailure verifies that when the registry
// credentials cannot be read, the cooldown gate holds the pull back with a
// CooldownError for the pull options failure, before any registry request.
//
// The test sets environment variables, so it does not run in parallel.
func Test_isOutsideCooldown_pullOptionsFailure(t *testing.T) {
	t.Run("GetPullOptions fails (registry auth unavailable)", func(t *testing.T) {
		t.Run("returns false with a CooldownError wrapping the pull options error", func(t *testing.T) {
			// A Docker config that cannot be parsed, with no environment
			// credentials to fall back on, makes the credential lookup fail.
			configDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{not json"), 0o600))
			t.Setenv("DOCKER_CONFIG", configDir)
			t.Setenv("REPO_USER", "")
			t.Setenv("REPO_PASS", "")

			i := newTestImageClient(newImageAPI(t), newTestLogger(t), registryConfig.Registry{}, ratelimit.New())

			outside, err := i.isOutsideCooldown(t.Context(), modelfixture.Container(containerfixture.WithImageName("registry.example.com/app:latest")), updateConfig.Policy{
				CooldownDelay: 1 * time.Hour,
			})
			assert.False(t, outside)

			var cooldownErr *CooldownError
			require.ErrorAs(t, err, &cooldownErr)
			assert.NotEmpty(t, cooldownErr.Delay)
			assert.Contains(t, cooldownErr.Error(), "could not get pull options for")
		})
	})
}

// TestPullImageCooldownGate verifies that a stale image whose creation time
// cannot be fetched is held back with a CooldownError instead of being pulled.
func TestPullImageCooldownGate(t *testing.T) {
	t.Parallel()

	t.Run("image is stale and cooldown age fetch fails", func(t *testing.T) {
		t.Parallel()

		t.Run("returns a CooldownError without pulling layers", func(t *testing.T) {
			t.Parallel()

			// An explicit registry host keeps local-only 404 handling from
			// applying, so the digest failure falls through to the cooldown gate.
			// ImagePull has no expectation, so a pull fails the test.
			image := notFoundRegistry(t) + "/test:latest"
			c := modelfixture.Container(
				containerfixture.WithImageName(image),
				containerfixture.WithRepoDigests([]string{image + "@sha256:12345"}),
			)

			// The test registry serves plain HTTP.
			i := newTestImageClient(newImageAPI(t), newTestLogger(t), registryConfig.Registry{TLSSkip: true}, ratelimit.New())

			err := i.PullImage(t.Context(), c, warnAuto, updateConfig.Policy{CooldownDelay: 1 * time.Hour})

			// A failed age lookup is not a deferral. The update counts it as a
			// failed check.
			var cooldownErr *CooldownError
			require.ErrorAs(t, err, &cooldownErr)
			require.NotErrorIs(t, err, ErrImageCooldown)
			require.Error(t, cooldownErr.Cause)
			assert.Empty(t, cooldownErr.Remaining)
		})
	})
}

func TestCheckLocalImageCooldown(t *testing.T) {
	t.Parallel()

	t.Run("defers when the local image is younger than the cooldown", func(t *testing.T) {
		t.Parallel()

		c := modelfixture.Container(
			containerfixture.WithImageName("app:latest"),
			containerfixture.WithImageCreated(time.Now().Add(-time.Hour)),
		)

		err := CheckLocalImageCooldown(c, updateConfig.Policy{CooldownDelay: 24 * time.Hour})
		require.ErrorIs(t, err, ErrImageCooldown)
	})

	t.Run("allows rebuilds when the local image is older than the cooldown", func(t *testing.T) {
		t.Parallel()

		c := modelfixture.Container(
			containerfixture.WithImageName("app:latest"),
			containerfixture.WithImageCreated(time.Now().Add(-48*time.Hour)),
		)

		err := CheckLocalImageCooldown(c, updateConfig.Policy{CooldownDelay: 24 * time.Hour})
		require.NoError(t, err)
	})
}

// TestCheckLocalImageCooldown_LogsContainer verifies that the local cooldown
// check logs its cooldown line with the container's name and image, as the
// registry check does. A split notification then carries the line with its
// container.
func TestCheckLocalImageCooldown_LogsContainer(t *testing.T) {
	t.Parallel()

	log, buf := logcapture.NewJSON(zerolog.InfoLevel)

	containerInfo, imageInfo := containerfixture.Inspect(
		containerfixture.WithName("app"),
		containerfixture.WithImageName("app:latest"),
		containerfixture.WithImageCreated(time.Now().Add(-time.Hour)),
	)
	c := container.NewContainer(log, containerInfo, imageInfo)

	err := CheckLocalImageCooldown(c, updateConfig.Policy{CooldownDelay: 24 * time.Hour})
	require.ErrorIs(t, err, ErrImageCooldown)

	entries := logcapture.Entries[logcapture.Entry](t, buf.String())
	require.Len(t, entries, 1)
	assert.Equal(t, logging.WithinCooldown, entries[0]["message"])
	assert.Equal(t, "app", entries[0]["container"])
	assert.Equal(t, "app:latest", entries[0]["image"])
}

// TestIsOutsideCooldown_RegistrySettings verifies that the cooldown check
// fetches the image creation time with the image client's registry settings,
// so a registry served over plain HTTP is reachable only when the settings
// skip TLS.
func TestIsOutsideCooldown_RegistrySettings(t *testing.T) {
	t.Parallel()

	c := modelfixture.Container(containerfixture.WithImageName(createdRegistry(t, time.Now().Add(-48*time.Hour)) + "/org/app:1.0"))
	policy := updateConfig.Policy{CooldownDelay: time.Hour}

	plain := NewClient(nil, newTestLogger(t), registryConfig.Registry{TLSSkip: true}, ratelimit.New())

	outside, err := plain.isOutsideCooldown(t.Context(), c, policy)
	require.NoError(t, err)
	assert.True(t, outside, "an image older than the cooldown was deferred")

	secure := NewClient(nil, newTestLogger(t), registryConfig.Registry{}, ratelimit.New())

	outside, err = secure.isOutsideCooldown(t.Context(), c, policy)
	require.Error(t, err, "an HTTPS request reached the plain HTTP registry")
	assert.False(t, outside)
}

// TestEvalImageAge verifies when an image is eligible for an update under a
// cooldown, including a creation time in the future, which counts as a new
// image and becomes eligible a cooldown after its creation time.
func TestEvalImageAge(t *testing.T) {
	t.Parallel()

	const delay = 24 * time.Hour

	tests := []struct {
		name string
		// created is the creation time relative to now.
		created time.Duration
		// wantEligibleIn is how long until the image is eligible. Zero means eligible now.
		wantEligibleIn time.Duration
	}{
		{name: "older than the cooldown", created: -48 * time.Hour},
		{name: "within the cooldown", created: -time.Hour, wantEligibleIn: 23 * time.Hour},
		{name: "a few minutes in the future", created: 5 * time.Minute, wantEligibleIn: delay + 5*time.Minute},
		{name: "far in the future", created: 365 * 24 * time.Hour, wantEligibleIn: delay + 365*24*time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				eligible, err := evalImageAge(time.Now().Add(tt.created), delay, nopLog())

				if tt.wantEligibleIn == 0 {
					require.NoError(t, err)
					assert.True(t, eligible)

					return
				}

				assert.False(t, eligible)
				require.ErrorIs(t, err, ErrImageCooldown)

				cooldownErr, ok := errors.AsType[*CooldownError](err)
				require.True(t, ok)
				assert.Equal(t, time.Now().Add(tt.wantEligibleIn), cooldownErr.EligibleAt)
			})
		})
	}
}

// TestCheckLocalImageCooldown_FutureCreationTime verifies that a local image
// whose creation time is in the future is held back by the cooldown.
func TestCheckLocalImageCooldown_FutureCreationTime(t *testing.T) {
	t.Parallel()

	c := modelfixture.Container(
		containerfixture.WithImageName("app:latest"),
		containerfixture.WithImageCreated(time.Now().Add(time.Hour)),
	)

	err := CheckLocalImageCooldown(c, updateConfig.Policy{CooldownDelay: 24 * time.Hour})
	require.ErrorIs(t, err, ErrImageCooldown)
}

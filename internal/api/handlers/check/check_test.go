package check

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	goGit "github.com/go-git/go-git/v5"

	"github.com/nicholas-fedor/watchtower/internal/container"
	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/container/oci"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/git/semver"
	gitConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/git"
	updateConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/update"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
)

// newTestLogger returns a logger that writes each event to the test's log, so a
// failing test shows what it logged.
//
// Parameters:
//   - t: Test that receives the log lines.
//
// Returns:
//   - *zerolog.Logger: Logger writing to t.
func newTestLogger(t *testing.T) *zerolog.Logger {
	t.Helper()

	// Frame 4 skips zerolog's write path, so each line names the log call.
	w := zerolog.TestWriter{T: t, Frame: 4}

	return new(zerolog.New(w))
}

func TestCheckForUpdates_DigestExtraction(t *testing.T) {
	client := mockContainerClient.NewMockClient(t)
	running := imageContainer("my-app", "nginx:latest", "sha256:abc", "nginx@sha256:digest123")
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]*container.Container{running}, nil)
	client.EXPECT().CheckContainerUpdate(mock.Anything, mock.Anything, mock.Anything).
		Return(false, container.ImageID("sha256:abc"), "", nil)

	results, err := CheckForUpdates(newTestLogger(t), t.Context(), client, nil, updateParams.Params{}, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "sha256:digest123", results[0].Digest)
}

func TestCheckForUpdates_CheckContainerUpdateError(t *testing.T) {
	client := mockContainerClient.NewMockClient(t)
	running := imageContainer("my-app", "nginx:latest", "sha256:abc")
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]*container.Container{running}, nil)
	client.EXPECT().CheckContainerUpdate(mock.Anything, mock.Anything, mock.Anything).
		Return(false, container.ImageID(""), "", errors.New("registry unavailable"))

	results, err := CheckForUpdates(newTestLogger(t), t.Context(), client, nil, updateParams.Params{}, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "registry unavailable", results[0].Error)
	assert.False(t, results[0].UpdateAvailable)
}

func TestCheckForUpdates_ParamsPropagation(t *testing.T) {
	client := mockContainerClient.NewMockClient(t)
	running := imageContainer("my-app", "nginx:latest", "sha256:abc")
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]*container.Container{running}, nil)
	client.EXPECT().CheckContainerUpdate(mock.Anything, mock.Anything, mock.MatchedBy(func(p updateConfig.Policy) bool {
		return p.MonitorOnly && p.NoPull && p.EnableChangelog && p.LabelPrecedence && p.CooldownDelay == 5*time.Minute
	})).Return(true, container.ImageID("sha256:new"), "sha256:newdigest", nil)

	params := updateParams.Params{
		MonitorOnly:     true,
		NoPull:          true,
		EnableChangelog: true,
		LabelPrecedence: true,
		CooldownDelay:   5 * time.Minute,
	}
	results, err := CheckForUpdates(newTestLogger(t), t.Context(), client, nil, params, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].UpdateAvailable)
	assert.Equal(t, "sha256:new", results[0].LatestImageID)
	assert.Equal(t, "sha256:newdigest", results[0].LatestDigest)
}

func TestCheckForUpdates_MixedStaleResults(t *testing.T) {
	client := mockContainerClient.NewMockClient(t)
	container1 := imageContainer("app1", "nginx:latest", "sha256:abc")
	container2 := imageContainer("app2", "redis:latest", "sha256:def")

	client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]*container.Container{container1, container2}, nil)
	client.EXPECT().CheckContainerUpdate(mock.Anything, container1, mock.Anything).
		Return(true, container.ImageID("sha256:new1"), "sha256:digest1", nil)
	client.EXPECT().CheckContainerUpdate(mock.Anything, container2, mock.Anything).
		Return(false, container.ImageID("sha256:def"), "", nil)

	results, err := CheckForUpdates(newTestLogger(t), t.Context(), client, nil, updateParams.Params{}, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.True(t, results[0].UpdateAvailable)
	assert.Equal(t, "sha256:new1", results[0].LatestImageID)
	assert.Equal(t, "sha256:digest1", results[0].LatestDigest)
	assert.False(t, results[1].UpdateAvailable)
	assert.Equal(t, "sha256:def", results[1].LatestImageID)
	assert.Empty(t, results[1].LatestDigest)
}

func TestCheckForUpdates_GitNoPullSkipsCheck(t *testing.T) {
	client := mockContainerClient.NewMockClient(t)
	watched := modelfixture.ContainerWithoutImageInfo(
		containerfixture.WithName("/app"),
		containerfixture.WithImageName("app:latest"),
		containerfixture.WithLabels(map[string]string{
			labels.GitRepo: "https://git.example.com/org/app.git",
		}),
	)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]*container.Container{watched}, nil)

	gitClient := git.New(newTestLogger(t), git.Options{Timeout: time.Millisecond})
	results, err := CheckForUpdates(newTestLogger(t), t.Context(), client, nil, updateParams.Params{
		NoPull: true,
		Git: gitConfig.Defaults{
			Enable: true,
		},
	}, gitClient)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "git", results[0].UpdateSource)
	assert.False(t, results[0].UpdateAvailable)
	assert.Empty(t, results[0].Error)
	assert.Empty(t, results[0].LatestImageID)
}

// TestCheckForUpdates_GitCommitField verifies that Git results expose the commit separately from the local image ID.
func TestCheckForUpdates_GitCommitField(t *testing.T) {
	repoDir, branch, commit := initCheckGitRepo(t)
	watched := modelfixture.ContainerWithoutImageInfo(
		containerfixture.WithName("/app"),
		containerfixture.WithImageName("app:latest"),
		containerfixture.WithLabels(map[string]string{
			labels.GitRepo: repoDir,
			labels.GitRef:  branch,
		}),
	)

	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]*container.Container{watched}, nil)

	results, err := CheckForUpdates(newTestLogger(t), t.Context(), client, nil, updateParams.Params{
		Git: gitConfig.Defaults{
			Enable: true,
		},
	}, git.New(newTestLogger(t), git.Options{Timeout: time.Second}))
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "git", results[0].UpdateSource)
	assert.Equal(t, commit, results[0].GitCommit)
	assert.Empty(t, results[0].LatestImageID)
}

// initCheckGitRepo creates a temporary Git repository and returns its directory, branch, and commit.
func initCheckGitRepo(t *testing.T) (string, string, string) {
	t.Helper()

	dir := t.TempDir()
	repo, err := goGit.PlainInit(dir, false)
	require.NoError(t, err)

	worktree, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README"), []byte("hello\n"), 0o600))

	_, err = worktree.Add("README")
	require.NoError(t, err)

	hash, err := worktree.Commit("init", &goGit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	})
	require.NoError(t, err)

	head, err := repo.Head()
	require.NoError(t, err)

	return dir, head.Name().Short(), hash.String()
}

// initCheckGitRepoWithTag creates a temporary Git repository with one commit, tags
// that commit, and returns the directory, branch, and commit.
func initCheckGitRepoWithTag(t *testing.T, tag string) (string, string, string) {
	t.Helper()

	dir, branch, commit := initCheckGitRepo(t)

	repo, err := goGit.PlainOpen(dir)
	require.NoError(t, err)

	_, err = repo.CreateTag(tag, plumbing.NewHash(commit), nil)
	require.NoError(t, err)

	return dir, branch, commit
}

func TestExtractFilterParams(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		key       string
		wantEmpty bool
		wantVals  []string
	}{
		{
			name:      "no params",
			query:     "",
			key:       "name",
			wantEmpty: true,
		},
		{
			name:     "single value",
			query:    "?name=my-container",
			key:      "name",
			wantVals: []string{"my-container"},
		},
		{
			name:     "comma-separated values",
			query:    "?name=container1,container2",
			key:      "name",
			wantVals: []string{"container1", "container2"},
		},
		{
			name:     "multiple params",
			query:    "?name=app1&name=app2",
			key:      "name",
			wantVals: []string{"app1", "app2"},
		},
		{
			name:      "empty value filtered out",
			query:     "?name=",
			key:       "name",
			wantEmpty: true,
		},
		{
			name:     "whitespace trimmed",
			query:    "?name=%20app1%20&name=app2",
			key:      "name",
			wantVals: []string{"app1", "app2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New(fiber.Config{})
			app.Get("/test", func(c fiber.Ctx) error {
				vals := extractFilterParams(c, tt.key)
				if tt.wantEmpty {
					assert.Empty(t, vals)
				} else {
					assert.Equal(t, tt.wantVals, vals)
				}

				return c.SendStatus(http.StatusOK)
			})

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test"+tt.query, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}

// imageContainer returns a running container with the given name and image,
// whose image has the given ID and repository digests.
func imageContainer(name, imageName, imageID string, repoDigests ...string) *container.Container {
	return modelfixture.Container(
		containerfixture.WithName(name),
		containerfixture.WithImageName(imageName),
		containerfixture.WithImageID(imageID),
		containerfixture.WithRepoDigests(repoDigests),
	)
}

// gitContainer returns a running container of app:latest associated with the
// given Git repository, whose image carries the given labels.
func gitContainer(repo string, imageLabels map[string]string) *container.Container {
	return modelfixture.Container(
		containerfixture.WithName("/app"),
		containerfixture.WithImageName("app:latest"),
		containerfixture.WithImageID("sha256:image"),
		containerfixture.WithLabels(map[string]string{labels.GitRepo: repo}),
		containerfixture.WithImageLabels(imageLabels),
	)
}

// TestNewContainerCheck verifies that a result carries the container's
// identity and the digest of its own image repository, leaving the source and
// availability to the check helpers.
func TestNewContainerCheck(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	// The image has digests in two repositories, so only the container's own
	// image name picks the expected one.
	c := imageContainer(
		"/app",
		"ghcr.io/org/app:latest",
		"sha256:abc",
		"docker.io/org/app@sha256:hub",
		"ghcr.io/org/app@sha256:ghcr",
	)

	got := newContainerCheck(c, now)

	assert.Equal(t, "app", got.Name)
	assert.Equal(t, "ghcr.io/org/app:latest", got.Image)
	assert.Equal(t, "sha256:abc", got.ImageID)
	assert.Equal(t, "sha256:ghcr", got.Digest)
	assert.Equal(t, now, got.Timestamp)
	// Source and availability belong to the check helpers, not the identity.
	assert.Empty(t, got.UpdateSource)
	assert.False(t, got.UpdateAvailable)
}

func TestApplyReportMetadata(t *testing.T) {
	t.Parallel()

	t.Run("concrete container resolves labels and OCI annotations", func(t *testing.T) {
		t.Parallel()

		c := gitContainer("https://github.com/org/app.git", map[string]string{
			oci.SourceLabel:   "https://github.com/org/app.git",
			oci.VersionLabel:  "1.2.2",
			oci.RevisionLabel: "abc123",
		})

		var result ContainerCheck
		applyReportMetadata(c, updateParams.Params{}, &result)

		assert.Equal(t, "https://github.com/org/app.git", result.GitRepo)
		assert.Equal(t, "https://github.com/org/app/releases", result.Changelog)
		assert.Equal(t, "https://github.com/org/app.git", result.OCISource)
		assert.Equal(t, "1.2.2", result.CurrentImageVersion)
		assert.Equal(t, "abc123", result.CurrentImageRevision)
		// The endpoint never pulls, so the latest* fields stay empty.
		assert.Empty(t, result.LatestImageVersion)
		assert.Empty(t, result.LatestImageRevision)
	})
}

func TestIsGitWatched(t *testing.T) {
	t.Parallel()

	gitClient := git.New(newTestLogger(t), git.Options{Timeout: time.Second})

	tests := []struct {
		name      string
		container *container.Container
		params    updateParams.Params
		gitClient *git.Client
		want      bool
	}{
		{
			name:      "no git client falls back to the registry",
			container: gitContainer("https://github.com/org/app.git", nil),
			params:    updateParams.Params{Git: gitConfig.Defaults{Enable: true}},
			gitClient: nil,
			want:      false,
		},
		{
			name:      "git monitoring disabled",
			container: gitContainer("https://github.com/org/app.git", nil),
			params:    updateParams.Params{},
			gitClient: gitClient,
			want:      false,
		},
		{
			name:      "associated container with monitoring enabled",
			container: gitContainer("https://github.com/org/app.git", nil),
			params:    updateParams.Params{Git: gitConfig.Defaults{Enable: true}},
			gitClient: gitClient,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isGitWatched(newTestLogger(t), tt.container, tt.params, tt.gitClient))
		})
	}
}

func TestCheckGitSource(t *testing.T) {
	t.Parallel()

	t.Run("no-pull reports the skip without querying", func(t *testing.T) {
		t.Parallel()

		repoDir, branch, _ := initCheckGitRepo(t)
		c := modelfixture.ContainerWithoutImageInfo(
			containerfixture.WithName("/app"),
			containerfixture.WithImageName("app:latest"),
			containerfixture.WithLabels(map[string]string{
				labels.GitRepo: repoDir,
				labels.GitRef:  branch,
			}),
		)

		var result ContainerCheck

		checkGitSource(
			t.Context(),
			newTestLogger(t),
			c,
			updateParams.Params{NoPull: true, Git: gitConfig.Defaults{Enable: true}},
			git.New(newTestLogger(t), git.Options{Timeout: time.Second}),
			&result,
		)

		assert.Equal(t, "git", result.UpdateSource)
		assert.False(t, result.UpdateAvailable)
		assert.Empty(t, result.Error)
		assert.Empty(t, result.GitCommit)
	})

	t.Run("an unreadable repository surfaces the error", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "absent")
		c := gitContainer(missing, nil)

		var result ContainerCheck

		checkGitSource(
			t.Context(),
			newTestLogger(t),
			c,
			updateParams.Params{Git: gitConfig.Defaults{Enable: true}},
			git.New(newTestLogger(t), git.Options{Timeout: time.Second}),
			&result,
		)

		assert.Equal(t, "git", result.UpdateSource)
		assert.NotEmpty(t, result.Error)
		assert.False(t, result.UpdateAvailable)
		assert.Empty(t, result.GitCommit)
	})

	t.Run("a fresh commit reports no update", func(t *testing.T) {
		t.Parallel()

		repoDir, branch, commit := initCheckGitRepo(t)
		c := gitContainer(repoDir, nil)
		c.SetLabel(labels.GitRef, branch)

		var result ContainerCheck

		checkGitSource(
			t.Context(),
			newTestLogger(t),
			c,
			updateParams.Params{Git: gitConfig.Defaults{Enable: true}},
			git.New(newTestLogger(t), git.Options{Timeout: time.Second}),
			&result,
		)

		assert.Equal(t, "git", result.UpdateSource)
		assert.Empty(t, result.Error)
		assert.Equal(t, commit, result.GitCommit)
	})
	t.Run("a resolved tag refines the changelog to that release", func(t *testing.T) {
		t.Parallel()

		repoDir, branch, commit := initCheckGitRepoWithTag(t, "v1.2.3")
		c := gitContainer(repoDir, nil)
		c.SetLabel(labels.GitRef, branch)
		// A semver policy is what makes the check resolve a tag at all.
		c.SetLabel(labels.GitSemverPolicy, string(semver.PolicyPatch))
		// A template makes the tag substitution observable, because a local
		// repository path has no known host to derive a releases URL from.
		c.SetLabel(labels.ChangelogURL, "https://example.com/notes/{tag}")

		var result ContainerCheck

		checkGitSource(
			t.Context(),
			newTestLogger(t),
			c,
			updateParams.Params{Git: gitConfig.Defaults{Enable: true}},
			git.New(newTestLogger(t), git.Options{Timeout: time.Second}),
			&result,
		)

		assert.Equal(t, "git", result.UpdateSource)
		assert.Equal(t, commit, result.GitCommit)
		assert.Equal(t, "https://example.com/notes/v1.2.3", result.Changelog)
	})
}

func TestCheckRegistrySource(t *testing.T) {
	t.Parallel()

	t.Run("an error is reported without update fields", func(t *testing.T) {
		t.Parallel()

		client := mockContainerClient.NewMockClient(t)
		client.EXPECT().CheckContainerUpdate(mock.Anything, mock.Anything, mock.Anything).
			Return(false, container.ImageID(""), "", errors.New("registry unreachable"))

		c := imageContainer("/app", "app:latest", "sha256:abc")

		var result ContainerCheck
		checkRegistrySource(t.Context(), newTestLogger(t), client, c, updateParams.Params{}, &result)

		assert.Equal(t, "registry", result.UpdateSource)
		assert.Equal(t, "registry unreachable", result.Error)
		assert.False(t, result.UpdateAvailable)
		assert.Empty(t, result.LatestDigest)
		assert.Empty(t, result.LatestImageID)
	})

	t.Run("a newer image populates the update fields", func(t *testing.T) {
		t.Parallel()

		client := mockContainerClient.NewMockClient(t)
		client.EXPECT().CheckContainerUpdate(mock.Anything, mock.Anything, mock.Anything).
			Return(true, container.ImageID("sha256:def"), "sha256:def", nil)

		c := imageContainer("/app", "app:latest", "sha256:abc")

		var result ContainerCheck
		checkRegistrySource(t.Context(), newTestLogger(t), client, c, updateParams.Params{}, &result)

		assert.Equal(t, "registry", result.UpdateSource)
		assert.True(t, result.UpdateAvailable)
		assert.Equal(t, "sha256:def", result.LatestImageID)
		assert.Equal(t, "sha256:def", result.LatestDigest)
		assert.Empty(t, result.Error)
	})

	t.Run("empty digest and image id are left unset", func(t *testing.T) {
		t.Parallel()

		client := mockContainerClient.NewMockClient(t)
		client.EXPECT().CheckContainerUpdate(mock.Anything, mock.Anything, mock.Anything).
			Return(false, container.ImageID(""), "", nil)

		c := imageContainer("/app", "app:latest", "sha256:abc")

		var result ContainerCheck
		checkRegistrySource(t.Context(), newTestLogger(t), client, c, updateParams.Params{}, &result)

		assert.Equal(t, "registry", result.UpdateSource)
		assert.False(t, result.UpdateAvailable)
		assert.Empty(t, result.LatestDigest)
		assert.Empty(t, result.LatestImageID)
	})
}

// logCheckFailure must stay out of the notification stream, because a failing
// container is already reported in the response body.
func TestLogCheckFailure(t *testing.T) {
	t.Parallel()

	c := imageContainer("/app", "app:latest", "sha256:abc")

	var buf bytes.Buffer

	log := zerolog.New(&buf)

	logCheckFailure(&log, c, errors.New("boom"), "Failed to check container for updates")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))

	assert.Equal(t, "debug", entry["level"])
	assert.Equal(t, "no", entry["notify"], "the entry must not raise a notification")
	assert.Equal(t, "app", entry["container"])
	assert.Equal(t, "app:latest", entry["image"])
	assert.Equal(t, "Failed to check container for updates", entry["message"])
	assert.Equal(t, "boom", entry["error"])
}

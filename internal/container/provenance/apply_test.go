package provenance

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestGitWrappers verifies the container's Git helpers: resolving the Git
// association, deciding whether to monitor, and reading and applying Git stamps
// and associations.
func TestGitWrappers(t *testing.T) {
	t.Parallel()

	t.Run("ResolveAssociation", func(t *testing.T) {
		t.Parallel()

		t.Run("returns false for a nil container", func(t *testing.T) {
			t.Parallel()

			assoc, ok := ResolveAssociation(nil, types.UpdateParams{})
			assert.False(t, ok)
			assert.Equal(t, Association{}, assoc)
		})

		t.Run("prefers a git-repo label over an image mapping", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container(
				containerfixture.WithImageName("myapp:latest"),
				containerfixture.WithLabels(map[string]string{
					labels.GitRepo: "https://github.com/org/from-label.git",
					labels.GitRef:  "release",
				}),
			)

			assoc, ok := ResolveAssociation(c, types.UpdateParams{
				GitImages: map[string]types.GitImage{
					"myapp:latest": {Repo: "https://github.com/org/from-map.git", Ref: "mapped"},
				},
			})
			assert.True(t, ok)
			assert.Equal(t, "https://github.com/org/from-label.git", assoc.Repo)
			assert.Equal(t, "release", assoc.Ref)
		})

		t.Run("uses an exact image mapping when labels are absent", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container(containerfixture.WithImageName("myapp:latest"))

			assoc, ok := ResolveAssociation(c, types.UpdateParams{
				GitImages: map[string]types.GitImage{
					"myapp:latest": {Repo: "https://github.com/org/mapped.git", Ref: "v1"},
				},
			})
			assert.True(t, ok)
			assert.Equal(t, "https://github.com/org/mapped.git", assoc.Repo)
			assert.Equal(t, "v1", assoc.Ref)
		})

		t.Run("returns false when the container is not associated", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container(containerfixture.WithImageName("nginx:latest"))
			assoc, ok := ResolveAssociation(c, types.UpdateParams{})
			assert.False(t, ok)
			assert.Equal(t, Association{}, assoc)
		})
	})

	t.Run("ShouldMonitor", func(t *testing.T) {
		t.Parallel()

		t.Run("returns false for a nil container", func(t *testing.T) {
			t.Parallel()

			assert.False(t, ShouldMonitor(nil, nil, types.UpdateParams{EnableGitMonitoring: true}))
		})

		t.Run("returns false for Watchtower even when associated", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container(containerfixture.WithLabels(map[string]string{
				labels.Watchtower: "true",
				labels.GitRepo:    "https://github.com/nicholas-fedor/watchtower.git",
			}))

			assert.False(t, ShouldMonitor(nil, c, types.UpdateParams{EnableGitMonitoring: true}))
		})

		t.Run("returns true when associated and monitoring is enabled", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container(containerfixture.WithLabels(map[string]string{
				labels.GitRepo: "https://github.com/org/app.git",
			}))

			assert.True(t, ShouldMonitor(nil, c, types.UpdateParams{EnableGitMonitoring: true}))
		})

		t.Run("returns false when the container turns watch off", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container(containerfixture.WithLabels(map[string]string{
				labels.GitRepo:  "https://github.com/org/app.git",
				labels.GitWatch: "false",
			}))

			assert.False(t, ShouldMonitor(nil, c, types.UpdateParams{EnableGitMonitoring: true}))
		})

		t.Run("returns false when watch is on but the container is not associated", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container(
				containerfixture.WithImageName("nginx:latest"),
				containerfixture.WithLabels(map[string]string{labels.GitWatch: "true"}),
			)
			nop := zerolog.Nop()

			assert.False(t, ShouldMonitor(&nop, c, types.UpdateParams{}))
		})
	})

	t.Run("LastGitStamp", func(t *testing.T) {
		t.Parallel()

		t.Run("returns empty values for a nil container", func(t *testing.T) {
			t.Parallel()

			commit, tag := LastStamp(nil)
			assert.Empty(t, commit)
			assert.Empty(t, tag)
		})

		t.Run("reads last-commit and last-tag labels", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container(containerfixture.WithLabels(map[string]string{
				labels.GitLastCommit: "abc123",
				labels.GitLastTag:    "v1.2.3",
			}))

			commit, tag := LastStamp(c)
			assert.Equal(t, "abc123", commit)
			assert.Equal(t, "v1.2.3", tag)
		})
	})

	t.Run("ApplyGitStamp", func(t *testing.T) {
		t.Parallel()

		t.Run("is a no-op for a nil container", func(t *testing.T) {
			t.Parallel()

			ApplyGitStamp(nil, "abc", "v1.0.0")
		})

		t.Run("writes the commit and omits an empty tag", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container()
			c.SetLabel(labels.GitLastTag, "v0.1.0")
			ApplyGitStamp(c, "abc123", "")

			commit, ok := c.GetLabel(labels.GitLastCommit)
			assert.True(t, ok)
			assert.Equal(t, "abc123", commit)

			_, ok = c.GetLabel(labels.GitLastTag)
			assert.False(t, ok)
		})

		t.Run("writes commit and tag", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container()
			ApplyGitStamp(c, "abc123", "v1.2.3")

			commit, tag := LastStamp(c)
			assert.Equal(t, "abc123", commit)
			assert.Equal(t, "v1.2.3", tag)
		})
	})

	t.Run("ApplyGitAssociation", func(t *testing.T) {
		t.Parallel()

		t.Run("is a no-op for a nil container or empty repo", func(t *testing.T) {
			t.Parallel()

			ApplyGitAssociation(nil, Association{Repo: "https://github.com/org/app.git"}, true)

			c := modelfixture.Container()
			ApplyGitAssociation(c, Association{}, true)
			assert.NotContains(t, c.ContainerInfo().Config.Labels, labels.GitRepo)
		})

		t.Run("persists association labels and watch", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container()
			ApplyGitAssociation(c, Association{
				Repo:       "https://github.com/org/app.git",
				Ref:        "main",
				Policy:     types.GitPolicyPatch,
				Host:       "https://git.example.com:3000",
				Dockerfile: "build/docker/Dockerfile",
				Context:    ".",
			}, true)

			assert.Equal(t, map[string]string{
				labels.GitRepo:         "https://github.com/org/app.git",
				labels.GitRef:          "main",
				labels.GitSemverPolicy: types.GitPolicyPatch,
				labels.GitHost:         "https://git.example.com:3000",
				labels.GitDockerfile:   "build/docker/Dockerfile",
				labels.GitContext:      ".",
				labels.GitWatch:        "true",
			}, c.ContainerInfo().Config.Labels)
		})

		t.Run("preserves bare local repository paths in labels", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container()
			ApplyGitAssociation(c, Association{Repo: "../repos/app", Ref: "main"}, true)

			repo, ok := c.GetLabel(labels.GitRepo)
			assert.True(t, ok)
			assert.Equal(t, "../repos/app", repo)
		})

		t.Run("preserves SSH identities and benign local path queries", func(t *testing.T) {
			t.Parallel()

			sshContainer := modelfixture.Container()
			ApplyGitAssociation(sshContainer, Association{Repo: "ssh://git@git.example.com/org/app.git"}, true)
			sshRepo, ok := sshContainer.GetLabel(labels.GitRepo)
			assert.True(t, ok)
			assert.Equal(t, "ssh://git@git.example.com/org/app.git", sshRepo)

			pathContainer := modelfixture.Container()
			ApplyGitAssociation(pathContainer, Association{Repo: "/srv/repos/app?release=1"}, true)
			pathRepo, ok := pathContainer.GetLabel(labels.GitRepo)
			assert.True(t, ok)
			assert.Equal(t, "/srv/repos/app?release=1", pathRepo)
		})

		t.Run("redacts credentials and query tokens before persisting labels", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container()
			ApplyGitAssociation(c, Association{
				Repo: "https://user:secret@github.com/org/app.git?access_token=query#main",
				Host: "https://git.example.com:3000?token=host-secret",
				Ref:  "main",
			}, true)

			repo, ok := c.GetLabel(labels.GitRepo)
			assert.True(t, ok)
			assert.Equal(t, "https://github.com/org/app.git#main", repo)
			assert.NotContains(t, repo, "secret")

			host, ok := c.GetLabel(labels.GitHost)
			assert.True(t, ok)
			assert.Equal(t, "https://git.example.com:3000", host)
			assert.NotContains(t, host, "secret")
		})

		t.Run("removes association labels when a repository cannot be sanitized", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container(containerfixture.WithLabels(map[string]string{
				labels.GitRepo: "https://user:secret@github.com/org/app.git",
			}))
			ApplyGitAssociation(c, Association{Repo: "://invalid"}, true)

			_, ok := c.GetLabel(labels.GitRepo)
			assert.False(t, ok)
		})

		t.Run("does not persist process defaults", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container()
			ApplyGitAssociation(c, Association{
				Repo:                  "https://github.com/org/app.git",
				Ref:                   "main",
				Policy:                types.GitPolicyNone,
				Dockerfile:            "Dockerfile",
				Context:               ".",
				RefFromDefault:        true,
				PolicyFromDefault:     true,
				DockerfileFromDefault: true,
				ContextFromDefault:    true,
			}, false)

			assert.Equal(t, map[string]string{
				labels.GitRepo: "https://github.com/org/app.git",
			}, c.ContainerInfo().Config.Labels)
		})

		t.Run("omits empty optional fields and does not force watch", func(t *testing.T) {
			t.Parallel()

			c := modelfixture.Container()
			c.SetLabel(labels.GitRef, "old")
			c.SetLabel(labels.GitWatch, "true")
			ApplyGitAssociation(c, Association{Repo: "https://github.com/org/app.git"}, false)

			assert.Equal(t, map[string]string{
				labels.GitRepo: "https://github.com/org/app.git",
			}, c.ContainerInfo().Config.Labels)
		})
	})
}

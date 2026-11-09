package provenance

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/git/semver"
	gitConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/git"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
)

// TestAssociation_Validate verifies that an invalid policy or git-host label
// is reported with its own error and that the policy label is checked first.
func TestAssociation_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		assoc   Association
		wantErr error
		wantMsg string
	}{
		{name: "no labels", assoc: Association{Policy: semver.PolicyNone}},
		{name: "valid host", assoc: Association{Host: "https://git.example.com:3000"}},
		{
			name:    "invalid policy",
			assoc:   Association{Policy: semver.PolicyNone, invalidPolicy: "nightly"},
			wantErr: semver.ErrInvalidPolicy,
			wantMsg: "invalid git semver policy: nightly",
		},
		{
			name:    "invalid host",
			assoc:   Association{Host: "ftp://git.example.com"},
			wantErr: ErrInvalidHost,
			wantMsg: "invalid git-host: scheme must be http or https",
		},
		{
			name:    "invalid policy reported before invalid host",
			assoc:   Association{Host: "ftp://git.example.com", invalidPolicy: "nightly"},
			wantErr: semver.ErrInvalidPolicy,
			wantMsg: "invalid git semver policy: nightly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.assoc.Validate()
			if tt.wantErr == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tt.wantErr)
			require.EqualError(t, err, tt.wantMsg)
		})
	}
}

func TestResolveAssociation(t *testing.T) {
	t.Parallel()

	t.Run("nil container", func(t *testing.T) {
		t.Parallel()

		got, ok := ResolveAssociation(nil, gitConfig.Defaults{})
		assert.False(t, ok)
		assert.Equal(t, Association{}, got)
	})

	t.Run("repo label wins over mapping", func(t *testing.T) {
		t.Parallel()

		c := testContainer(map[string]string{
			labels.GitRepo:       "https://github.com/org/from-label.git",
			labels.GitRef:        "release",
			labels.GitDockerfile: "build/Dockerfile",
			labels.GitContext:    "src",
		}, "myapp:latest")

		got, ok := ResolveAssociation(c, gitConfig.Defaults{
			DefaultRef:   "main",
			SemverPolicy: semver.PolicyPatch,
			Dockerfile:   "Dockerfile",
			Context:      ".",
			Images: map[string]gitConfig.Image{
				"myapp:latest": {Repo: "https://github.com/org/from-map.git", Ref: "mapped"},
			},
		})
		require.True(t, ok)
		assert.Equal(t, Association{
			Repo:              "https://github.com/org/from-label.git",
			Ref:               "release",
			Policy:            semver.PolicyPatch,
			Dockerfile:        "build/Dockerfile",
			Context:           "src",
			PolicyFromDefault: true,
		}, got)
	})

	t.Run("git-host label is kept on the association", func(t *testing.T) {
		t.Parallel()

		c := testContainer(map[string]string{
			labels.GitRepo: "git@git.example.com:org/app.git",
			labels.GitHost: "https://git.example.com:3000",
		}, "myapp:latest")

		got, ok := ResolveAssociation(c, gitConfig.Defaults{})
		require.True(t, ok)
		assert.Equal(t, "https://git.example.com:3000", got.Host)
	})

	t.Run("label defaults for ref policy and paths", func(t *testing.T) {
		t.Parallel()

		c := testContainer(map[string]string{
			labels.GitRepo: "https://github.com/org/app.git",
		}, "myapp:latest")

		got, ok := ResolveAssociation(c, gitConfig.Defaults{})
		require.True(t, ok)
		assert.Equal(t, "main", got.Ref)
		assert.Equal(t, semver.PolicyNone, got.Policy)
		assert.Empty(t, got.Dockerfile)
		assert.Empty(t, got.Context)
	})

	t.Run("git-branch alias", func(t *testing.T) {
		t.Parallel()

		c := testContainer(map[string]string{
			labels.GitRepo:   "https://github.com/org/app.git",
			labels.GitBranch: "develop",
		}, "myapp:latest")

		got, ok := ResolveAssociation(c, gitConfig.Defaults{})
		require.True(t, ok)
		assert.Equal(t, "develop", got.Ref)
	})

	t.Run("invalid policy label falls back and is recorded", func(t *testing.T) {
		t.Parallel()

		c := testContainer(map[string]string{
			labels.GitRepo:         "https://github.com/org/app.git",
			labels.GitSemverPolicy: " Nightly ",
		}, "myapp:latest")

		got, ok := ResolveAssociation(c, gitConfig.Defaults{})
		require.True(t, ok)
		assert.Equal(t, semver.PolicyNone, got.Policy)
		assert.Equal(t, "Nightly", got.invalidPolicy)
	})

	t.Run("invalid policy label on a mapped image is recorded", func(t *testing.T) {
		t.Parallel()

		c := testContainer(map[string]string{
			labels.GitSemverPolicy: "nightly",
		}, "myapp:latest")

		got, ok := ResolveAssociation(c, gitConfig.Defaults{
			Images: map[string]gitConfig.Image{
				"myapp:latest": {Repo: "https://github.com/org/mapped.git", Policy: semver.PolicyMinor},
			},
		})
		require.True(t, ok)
		assert.Equal(t, semver.PolicyMinor, got.Policy)
		assert.Equal(t, "nightly", got.invalidPolicy)
	})

	t.Run("image mapping", func(t *testing.T) {
		t.Parallel()

		c := testContainer(map[string]string{
			labels.GitDockerfile: "label.Dockerfile",
		}, "myapp:latest")

		got, ok := ResolveAssociation(c, gitConfig.Defaults{
			Images: map[string]gitConfig.Image{
				"myapp:latest": {
					Repo:       "https://github.com/org/mapped.git",
					Ref:        "v1",
					Policy:     semver.PolicyMinor,
					Dockerfile: "map.Dockerfile",
					Context:    "app",
				},
			},
		})
		require.True(t, ok)
		assert.Equal(t, Association{
			Repo:       "https://github.com/org/mapped.git",
			Ref:        "v1",
			Policy:     semver.PolicyMinor,
			Dockerfile: "label.Dockerfile",
			Context:    "app",
		}, got)
	})

	t.Run("mapping falls back to process defaults", func(t *testing.T) {
		t.Parallel()

		c := testContainer(nil, "myapp:latest")
		got, ok := ResolveAssociation(c, gitConfig.Defaults{
			DefaultRef:   "trunk",
			SemverPolicy: semver.PolicyMajor,
			Dockerfile:   "ci/Dockerfile",
			Context:      "docker",
			Images: map[string]gitConfig.Image{
				"myapp:latest": {Repo: "https://github.com/org/mapped.git"},
			},
		})
		require.True(t, ok)
		assert.Equal(t, Association{
			Repo:                  "https://github.com/org/mapped.git",
			Ref:                   "trunk",
			Policy:                semver.PolicyMajor,
			Dockerfile:            "ci/Dockerfile",
			Context:               "docker",
			RefFromDefault:        true,
			PolicyFromDefault:     true,
			DockerfileFromDefault: true,
			ContextFromDefault:    true,
		}, got)
	})

	t.Run("unassociated", func(t *testing.T) {
		t.Parallel()

		c := testContainer(nil, "nginx:latest")
		got, ok := ResolveAssociation(c, gitConfig.Defaults{})
		assert.False(t, ok)
		assert.Equal(t, Association{}, got)
	})
}

func TestShouldMonitor(t *testing.T) {
	t.Parallel()

	t.Run("nil container", func(t *testing.T) {
		t.Parallel()

		assert.False(t, ShouldMonitor(nil, nil, gitConfig.Defaults{Enable: true}))
	})

	t.Run("associated and process watch", func(t *testing.T) {
		t.Parallel()

		c := testContainer(map[string]string{labels.GitRepo: "https://github.com/org/app.git"}, "myapp:latest")
		assert.True(t, ShouldMonitor(nil, c, gitConfig.Defaults{Enable: true}))
	})

	t.Run("associated but watch off", func(t *testing.T) {
		t.Parallel()

		c := testContainer(map[string]string{
			labels.GitRepo:  "https://github.com/org/app.git",
			labels.GitWatch: "false",
		}, "myapp:latest")
		assert.False(t, ShouldMonitor(nil, c, gitConfig.Defaults{Enable: true}))
	})

	t.Run("watchtower is excluded", func(t *testing.T) {
		t.Parallel()

		c := testContainer(map[string]string{
			labels.Watchtower: "true",
			labels.GitRepo:    "https://github.com/org/app.git",
		}, "watchtower:latest")
		assert.False(t, ShouldMonitor(nil, c, gitConfig.Defaults{Enable: true}))
	})

	t.Run("watch without association", func(t *testing.T) {
		t.Parallel()

		c := testContainer(map[string]string{labels.GitWatch: "true"}, "nginx:latest")
		nop := zerolog.Nop()
		assert.False(t, ShouldMonitor(&nop, c, gitConfig.Defaults{}))
		assert.False(t, ShouldMonitor(nil, c, gitConfig.Defaults{}))
	})
}

func TestLabel(t *testing.T) {
	t.Parallel()

	assert.Empty(t, label(nil, labels.GitRepo))
	assert.Empty(t, label(testContainer(nil, "app:latest"), labels.GitRepo))
	assert.Equal(t, "https://github.com/org/app.git", label(testContainer(map[string]string{
		labels.GitRepo: "  https://github.com/org/app.git  ",
	}, "app:latest"), labels.GitRepo))
}

func TestRefLabel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "release", refLabel(testContainer(map[string]string{
		labels.GitRef:    "release",
		labels.GitBranch: "ignored",
	}, "app:latest")))
	assert.Equal(t, "develop", refLabel(testContainer(map[string]string{
		labels.GitBranch: "develop",
	}, "app:latest")))
	assert.Empty(t, refLabel(testContainer(nil, "app:latest")))
}

func TestImageMapping(t *testing.T) {
	t.Parallel()

	images := map[string]gitConfig.Image{
		"myapp:latest": {Repo: "https://github.com/org/app.git"},
	}

	assert.Equal(t, images["myapp:latest"], imageMapping("myapp:latest", images))
	assert.Equal(t, gitConfig.Image{}, imageMapping("other:latest", images))
	assert.Equal(t, gitConfig.Image{}, imageMapping("myapp:latest", nil))
	assert.Equal(t, gitConfig.Image{}, imageMapping("", images))
}

// TestWatchLabel covers which git-watch values count as set and which of
// those turn the watcher on.
func TestWatchLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		label       string
		present     bool
		wantEnabled bool
		wantSet     bool
	}{
		{name: "absent"},
		{name: "blank", label: "  ", present: true},
		{name: "true", label: "true", present: true, wantEnabled: true, wantSet: true},
		{name: "yes", label: " YES ", present: true, wantEnabled: true, wantSet: true},
		{name: "one", label: "1", present: true, wantEnabled: true, wantSet: true},
		{name: "t", label: "t", present: true, wantEnabled: true, wantSet: true},
		{name: "false", label: "false", present: true, wantSet: true},
		{name: "no", label: "No", present: true, wantSet: true},
		{name: "zero", label: "0", present: true, wantSet: true},
		{name: "f", label: "f", present: true, wantSet: true},
		{name: "unknown", label: "maybe", present: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			containerLabels := map[string]string{}
			if tt.present {
				containerLabels[labels.GitWatch] = tt.label
			}

			enabled, set := watchLabel(testContainer(containerLabels, "app:latest"))
			assert.Equal(t, tt.wantEnabled, enabled)
			assert.Equal(t, tt.wantSet, set)
		})
	}

	enabled, set := watchLabel(nil)
	assert.False(t, enabled)
	assert.False(t, set)
}

func TestIsWatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		label   string
		present bool
		process bool
		want    bool
	}{
		{name: "missing uses process true", process: true, want: true},
		{name: "missing uses process false"},
		{name: "blank uses process", label: "  ", present: true, process: true, want: true},
		{name: "true", label: "true", present: true, want: true},
		{name: "yes", label: "YES", present: true, want: true},
		{name: "one", label: "1", present: true, want: true},
		{name: "t", label: "t", present: true, want: true},
		{name: "false", label: "false", present: true, process: true},
		{name: "no", label: "no", present: true, process: true},
		{name: "zero", label: "0", present: true, process: true},
		{name: "f", label: "f", present: true, process: true},
		{name: "unknown uses process", label: "maybe", present: true, process: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			containerLabels := map[string]string{}
			if tt.present {
				containerLabels[labels.GitWatch] = tt.label
			}

			got := isWatch(testContainer(containerLabels, "app:latest"), gitConfig.Defaults{Enable: tt.process})
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultRef(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "main", defaultRef(gitConfig.Defaults{}))
	assert.Equal(t, "develop", defaultRef(gitConfig.Defaults{DefaultRef: "develop"}))
}

func TestDefaultPolicy(t *testing.T) {
	t.Parallel()

	assert.Equal(t, semver.PolicyNone, defaultPolicy(gitConfig.Defaults{}))
	assert.Equal(t, semver.PolicyMinor, defaultPolicy(gitConfig.Defaults{SemverPolicy: semver.PolicyMinor}))
	assert.Equal(t, semver.PolicyMajor, defaultPolicy(gitConfig.Defaults{SemverPolicy: " Major "}))
	assert.Equal(t, semver.PolicyNone, defaultPolicy(gitConfig.Defaults{SemverPolicy: "nightly"}))
}

// testContainer returns a container with the given labels and image name.
// Its image info is missing, so no OCI annotations apply.
//
// Parameters:
//   - containerLabels: Label key to value map. Missing keys are unset.
//   - imageName: Image reference the container runs.
//
// Returns:
//   - *container.Container: Container built from inspect data.
func testContainer(containerLabels map[string]string, imageName string) *container.Container {
	return modelfixture.ContainerWithoutImageInfo(
		containerfixture.WithLabels(containerLabels),
		containerfixture.WithImageName(imageName),
	)
}

package assert

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiffFidelityAllowsImageIDChangeButNotNetworks(t *testing.T) {
	t.Parallel()

	before := InspectSnapshot{
		Name:     "app",
		ImageRef: "e2e/app:latest",
		ImageID:  "sha256:aaa",
		Env:      []string{"FOO=bar"},
		Labels:   map[string]string{"com.centurylinklabs.watchtower.enable": "true"},
		Networks: []string{"bridge", "e2e-a"},
	}
	after := before
	after.ImageID = "sha256:bbb"

	require.NoError(t, DiffFidelity(before, after))

	after.Networks = []string{"bridge"}
	err := DiffFidelity(before, after)
	require.Error(t, err)
	require.Contains(t, err.Error(), "networks")
}

func TestParsePorcelainFieldSet(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(PorcelainReport{
		Containers: []PorcelainContainer{
			{
				Name:            "app",
				Image:           "e2e/app:latest",
				ImageID:         "sha256:aaa",
				LatestImageID:   "sha256:bbb",
				State:           "Updated",
				UpdateAvailable: true,
			},
		},
	})
	require.NoError(t, err)

	report, parseErr := ParsePorcelain(raw)
	require.NoError(t, parseErr)
	require.NoError(t, RequireUpdated(report))
}

func TestForbiddenSecrets(t *testing.T) {
	t.Parallel()

	require.NoError(t, ForbiddenSecrets(`{"token":"redacted"}`, []string{"super-secret"}))
	require.Error(t, ForbiddenSecrets("Authorization: Bearer super-secret", []string{"super-secret"}))
}

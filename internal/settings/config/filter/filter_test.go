package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
)

// scopedContainer returns a container named name whose scope label is scope,
// or one without a scope label when scope is empty.
//
// Parameters:
//   - name: Container name.
//   - scope: Value of the scope label. An empty value leaves the label off.
//
// Returns:
//   - *container.Container: The container.
func scopedContainer(name, scope string) *container.Container {
	containerLabels := map[string]string{}
	if scope != "" {
		containerLabels[labels.Scope] = scope
	}

	return modelfixture.Container(
		containerfixture.WithName(name),
		containerfixture.WithLabels(containerLabels),
	)
}

// TestFilter_Build verifies that Build sets the predicate and description from
// the filter's settings and keeps the settings themselves.
func TestFilter_Build(t *testing.T) {
	t.Parallel()

	settings := Filter{
		Scope:          "prod",
		Names:          []string{"web", "db"},
		SkipImageNames: []string{"busybox"},
	}

	built, err := settings.Build(nil)
	require.NoError(t, err)

	assert.Equal(t,
		`Only checking containers which name matches "web" or "db", whose image is not one of "busybox", in scope prod`,
		built.Desc)
	assert.Equal(t, settings.Scope, built.Scope)
	assert.Equal(t, settings.Names, built.Names)
	assert.Equal(t, settings.SkipImageNames, built.SkipImageNames)

	require.NotNil(t, built.Predicate)
	assert.True(t, built.Predicate(scopedContainer("/web", "prod")), "a named container in the scope is selected")
	assert.False(t, built.Predicate(scopedContainer("/web", "staging")), "a container in another scope is not selected")
	assert.False(t, built.Predicate(scopedContainer("/cache", "prod")), "a container not named is not selected")
}

// TestFilter_Build_malformedLabelPair verifies that Build reports a label pair
// without an equals sign, naming the filter build.
func TestFilter_Build_malformedLabelPair(t *testing.T) {
	t.Parallel()

	_, err := Filter{EnableContainersByLabel: []string{"env"}}.Build(nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "build filter:")
}

// TestFilter_WithLabelScope verifies that a configured scope wins over the
// scope label and that a non-empty label, "none" included, sets the scope of
// a filter without one and rebuilds it for that scope. ScopeWithLabel gives
// the same scope.
func TestFilter_WithLabelScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configured string
		label      string
		wantScope  string
		wantDesc   string
	}{
		{
			name:       "configured scope wins",
			configured: "preset",
			label:      "production",
			wantScope:  "preset",
			wantDesc:   "Only checking containers in scope preset",
		},
		{
			name:       "configured scope wins over a none label",
			configured: "preset",
			label:      "none",
			wantScope:  "preset",
			wantDesc:   "Only checking containers in scope preset",
		},
		{
			name:      "label sets the scope",
			label:     "production",
			wantScope: "production",
			wantDesc:  "Only checking containers in scope production",
		},
		{
			name:      "none label sets the scope",
			label:     "none",
			wantScope: "none",
			wantDesc:  "Only checking containers without a scope",
		},
		{
			name:     "no label keeps the filter unscoped",
			wantDesc: "Only checking containers without a scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			configured, err := Filter{Scope: tt.configured}.Build(nil)
			require.NoError(t, err)

			assert.Equal(t, tt.wantScope, configured.ScopeWithLabel(tt.label))

			got, err := configured.WithLabelScope(nil, tt.label)
			require.NoError(t, err)

			assert.Equal(t, tt.wantScope, got.Scope)
			assert.Equal(t, tt.wantDesc, got.Desc)

			if tt.wantScope == "" || tt.wantScope == "none" {
				assert.True(t, got.Predicate(scopedContainer("/app", "")),
					"a container without a scope is selected")
				assert.False(t, got.Predicate(scopedContainer("/app", "production")),
					"a container in a scope is not selected")
			} else {
				assert.True(t, got.Predicate(scopedContainer("/app", tt.wantScope)),
					"a container in the resulting scope is selected")
				assert.False(t, got.Predicate(scopedContainer("/app", "")),
					"a container without a scope is not selected")
			}
		})
	}
}

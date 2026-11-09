package sorter_test

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container"
	containerLabels "github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	"github.com/nicholas-fedor/watchtower/internal/update/sorter"
)

// TestContainerSorting verifies through the exported API that containers are
// sorted after their dependencies, including chained, multiple, and diamond
// dependencies given as slash-prefixed links, with Watchtower containers last.
func TestContainerSorting(t *testing.T) {
	t.Parallel()

	t.Run("SortByDependencies", func(t *testing.T) {
		t.Parallel()

		t.Run("sorting by dependencies", func(t *testing.T) {
			t.Parallel()

			t.Run("sorts containers with no links first", func(t *testing.T) {
				t.Parallel()

				c1 := modelContainer("c1", "c2")
				c2 := modelContainer("c2", "")
				containers := []*container.Container{c1, c2}
				err := sorter.SortByDependencies(newTestLogger(t), containers, true)
				require.NoError(t, err)
				require.Len(t, containers, 2)
				assert.Equal(t, "c2", containers[0].Name()) // No links
				assert.Equal(t, "c1", containers[1].Name()) // Depends on c2
			})

			t.Run("handles multiple dependencies", func(t *testing.T) {
				t.Parallel()

				c1 := modelContainer("c1", "c2,c3")
				c2 := modelContainer("c2", "c3")
				c3 := modelContainer("c3", "")
				containers := []*container.Container{c1, c2, c3}
				err := sorter.SortByDependencies(newTestLogger(t), containers, true)
				require.NoError(t, err)
				require.Len(t, containers, 3)
				assert.Equal(t, "c3", containers[0].Name()) // No links
				assert.Equal(t, "c2", containers[1].Name()) // Links to c3
				assert.Equal(t, "c1", containers[2].Name()) // Links to c2, c3
			})

			t.Run("detects circular references", func(t *testing.T) {
				t.Parallel()

				c1 := modelContainer("c1", "c2")
				c2 := modelContainer("c2", "c1")
				containers := []*container.Container{c1, c2}
				err := sorter.SortByDependencies(newTestLogger(t), containers, true)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "circular reference detected")
				assert.Contains(t, err.Error(), "c1 -> c2 -> c1")
			})

			t.Run("handles missing dependencies gracefully", func(t *testing.T) {
				t.Parallel()

				c1 := modelContainer("c1", "c2")
				c3 := modelContainer("c3", "")
				containers := []*container.Container{c1, c3}
				err := sorter.SortByDependencies(newTestLogger(t), containers, true)
				require.NoError(t, err)
				require.Len(t, containers, 2)
				assert.Equal(t, "c3", containers[0].Name()) // No links
				assert.Equal(t, "c1", containers[1].Name()) // Links to missing c2
			})

			t.Run("handles empty list", func(t *testing.T) {
				t.Parallel()

				containers := []*container.Container{}
				err := sorter.SortByDependencies(newTestLogger(t), containers, true)
				require.NoError(t, err)
				assert.Empty(t, containers)
			})

			t.Run("places Watchtower containers last", func(t *testing.T) {
				t.Parallel()

				watchtower := watchtowerContainer("watchtower")
				c1 := modelContainer("c1", "c2")
				c2 := modelContainer("c2", "")
				containers := []*container.Container{watchtower, c1, c2}
				err := sorter.SortByDependencies(newTestLogger(t), containers, true)
				require.NoError(t, err)
				require.Len(t, containers, 3)
				assert.Equal(t, "c2", containers[0].Name()) // No links
				assert.Equal(t, "c1", containers[1].Name()) // Depends on c2
				assert.Equal(t, "watchtower", containers[2].Name())
				// Watchtower last
			})

			t.Run("places multiple Watchtower containers last", func(t *testing.T) {
				t.Parallel()

				watchtower1 := watchtowerContainer("watchtower1")
				c1 := modelContainer("c1", "c2")
				watchtower2 := watchtowerContainer("watchtower2")
				c2 := modelContainer("c2", "")
				containers := []*container.Container{watchtower1, c1, watchtower2, c2}
				err := sorter.SortByDependencies(newTestLogger(t), containers, true)
				require.NoError(t, err)
				require.Len(t, containers, 4)
				assert.Equal(t, "c2", containers[0].Name()) // No links
				assert.Equal(t, "c1", containers[1].Name()) // Depends on c2
				// Watchtower containers at the end (order may vary)
				watchtowerNames := []string{containers[2].Name(), containers[3].Name()}
				assert.Contains(t, watchtowerNames, "watchtower1")
				assert.Contains(t, watchtowerNames, "watchtower2")
			})

			t.Run("handles chained dependencies with slash-prefixed links", func(t *testing.T) {
				t.Parallel()

				testCases := []struct {
					name          string
					containers    []*container.Container
					expectedOrder []string
				}{
					{
						name: "simple chain with slashes",
						containers: []*container.Container{
							modelContainer("c", ""),
							modelContainer("b", "/c"),
							modelContainer("a", "/b"),
						},
						expectedOrder: []string{"c", "b", "a"},
					},
					{
						name: "multiple dependencies with slashes",
						containers: []*container.Container{
							modelContainer("d", ""),
							modelContainer("c", ""),
							modelContainer("b", "/d"),
							modelContainer("a", "/b,/c"),
						},
						expectedOrder: []string{"d", "c", "b", "a"},
					},
					{
						name: "diamond dependency graph",
						containers: []*container.Container{
							modelContainer("d", ""),
							modelContainer("b", "/d"),
							modelContainer("c", "/d"),
							modelContainer("a", "/b,/c"),
						},
						expectedOrder: []string{
							"d",
							"b",
							"c",
							"a",
						}, // D first, then B and C (order may vary), then A
					},
					{
						name: "no dependencies",
						containers: []*container.Container{
							modelContainer("c", ""),
							modelContainer("b", ""),
							modelContainer("a", ""),
						},
						expectedOrder: []string{"c", "b", "a"},
					},
				}
				for _, tc := range testCases {
					t.Run(tc.name, func(t *testing.T) {
						t.Parallel()

						containers := tc.containers
						err := sorter.SortByDependencies(newTestLogger(t), containers, true)
						require.NoError(t, err)
						require.Len(t, containers, len(tc.expectedOrder))
						// For diamond, check that D is first, A is last, and B/C are in middle
						if tc.name == "diamond dependency graph" {
							assert.Equal(t, "d", containers[0].Name())
							assert.Equal(t, "a", containers[3].Name())
							middleNames := []string{containers[1].Name(), containers[2].Name()}
							assert.Contains(t, middleNames, "b")
							assert.Contains(t, middleNames, "c")
						} else {
							for i, name := range tc.expectedOrder {
								assert.Equal(t, name, containers[i].Name())
							}
						}
					})
				}
			})
		})
	})
}

// modelContainer builds a container from inspect data with the given name and
// a Watchtower depends-on label of dependsOn. Its ID is derived from the name.
//
// Parameters:
//   - name: Container name.
//   - dependsOn: Value of the depends-on label, or empty for none.
//
// Returns:
//   - *container.Container: The container.
func modelContainer(name, dependsOn string) *container.Container {
	labels := map[string]string{}
	if dependsOn != "" {
		labels[containerLabels.DependsOn] = dependsOn
	}

	return modelfixture.Container(
		containerfixture.WithName("/"+name),
		containerfixture.WithID("id-"+name),
		containerfixture.WithLabels(labels),
	)
}

// watchtowerContainer builds a Watchtower container from inspect data with the
// given name and an ID derived from it.
//
// Parameters:
//   - name: Container name.
//
// Returns:
//   - *container.Container: The container.
func watchtowerContainer(name string) *container.Container {
	return modelfixture.Container(
		containerfixture.WithName("/"+name),
		containerfixture.WithID("id-"+name),
		containerfixture.WithLabels(map[string]string{containerLabels.Watchtower: "true"}),
	)
}

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

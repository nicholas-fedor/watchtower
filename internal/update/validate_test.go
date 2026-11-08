package update

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	composeLabels "github.com/nicholas-fedor/watchtower/internal/compose/labels"
	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	containerLabels "github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestValidateRollingRestartDependencies verifies that rolling restarts are
// rejected for containers with links and that listing failures are reported.
func TestValidateRollingRestartDependencies(t *testing.T) {
	t.Parallel()

	// filter accepts every container.
	filter := types.Filter(func(types.FilterableContainer) bool { return true })

	t.Run("ListContainers fails", func(t *testing.T) {
		t.Parallel()

		t.Run("should return the client's error as it is", func(t *testing.T) {
			t.Parallel()

			mockClient := mockContainerClient.NewMockClient(t)
			clientErr := errors.New("failed to list containers: daemon unavailable")
			mockClient.EXPECT().ListContainers(mock.Anything, mock.Anything).Return(nil, clientErr)

			err := ValidateRollingRestartDependencies(newTestLogger(t), t.Context(), mockClient, filter, true)

			require.Same(t, clientErr, err)
			require.EqualError(t, err, "failed to list containers: daemon unavailable")
		})
	})

	t.Run("no containers are found", func(t *testing.T) {
		t.Parallel()

		t.Run("should return nil", func(t *testing.T) {
			t.Parallel()

			mockClient := mockContainerClient.NewMockClient(t)
			mockClient.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{}, nil)

			err := ValidateRollingRestartDependencies(newTestLogger(t), t.Context(), mockClient, filter, true)

			require.NoError(t, err)
		})
	})

	t.Run("containers have no links", func(t *testing.T) {
		t.Parallel()

		t.Run("should return nil", func(t *testing.T) {
			t.Parallel()

			mockClient := mockContainerClient.NewMockClient(t)
			mockClient.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{
				labeledContainer("app", nil),
				labeledContainer("cache", nil),
			}, nil)

			err := ValidateRollingRestartDependencies(newTestLogger(t), t.Context(), mockClient, filter, true)

			require.NoError(t, err)
		})
	})

	t.Run("a container has links", func(t *testing.T) {
		t.Parallel()

		t.Run("should return error with container name and links", func(t *testing.T) {
			t.Parallel()

			mockClient := mockContainerClient.NewMockClient(t)
			mockClient.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{
				labeledContainer("app", nil),
				labeledContainer("web-container", map[string]string{containerLabels.DependsOn: "db,redis"}),
			}, nil)

			err := ValidateRollingRestartDependencies(newTestLogger(t), t.Context(), mockClient, filter, true)

			require.ErrorIs(t, err, errRollingRestartDependency)
			assert.Contains(t, err.Error(), `"web-container" depends on [db redis]`)
		})

		t.Run("should return error when Compose depends_on is not read", func(t *testing.T) {
			t.Parallel()

			// The Watchtower depends-on label counts whether or not Compose
			// depends_on is read.
			mockClient := mockContainerClient.NewMockClient(t)
			mockClient.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{
				labeledContainer("web-container", map[string]string{containerLabels.DependsOn: "db"}),
			}, nil)

			err := ValidateRollingRestartDependencies(newTestLogger(t), t.Context(), mockClient, filter, false)

			require.ErrorIs(t, err, errRollingRestartDependency)
			assert.Contains(t, err.Error(), `"web-container" depends on [db]`)
		})
	})

	t.Run("a container has only a Compose depends_on label", func(t *testing.T) {
		t.Parallel()

		// web names db only in its Compose depends_on label, which counts as a
		// dependency only when Compose depends_on is read.
		web := labeledContainer("web", map[string]string{
			composeLabels.ComposeDependsOnLabel: "db:service_started:false",
		})

		t.Run("should return error when Compose depends_on is read", func(t *testing.T) {
			t.Parallel()

			mockClient := mockContainerClient.NewMockClient(t)
			mockClient.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{web}, nil)

			err := ValidateRollingRestartDependencies(newTestLogger(t), t.Context(), mockClient, filter, true)

			require.ErrorIs(t, err, errRollingRestartDependency)
			assert.Contains(t, err.Error(), `"web" depends on [db]`)
		})

		t.Run("should return nil when Compose depends_on is not read", func(t *testing.T) {
			t.Parallel()

			mockClient := mockContainerClient.NewMockClient(t)
			mockClient.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{web}, nil)

			err := ValidateRollingRestartDependencies(newTestLogger(t), t.Context(), mockClient, filter, false)

			require.NoError(t, err)
		})
	})
}

// labeledContainer builds a container from inspect data with the given name
// and labels.
//
// Parameters:
//   - name: Container name.
//   - labels: Container labels, or nil for none.
//
// Returns:
//   - types.Container: The container.
func labeledContainer(name string, labels map[string]string) types.Container {
	return modelfixture.Container(containerfixture.WithName(name), containerfixture.WithLabels(labels))
}

// listFiltered returns a ListContainers implementation that keeps the
// containers every given filter accepts, as the client does, and records them
// in selected.
//
// Parameters:
//   - selected: Receives the containers the filters accept.
//   - containers: Containers the fake daemon holds.
//
// Returns:
//   - func(context.Context, ...types.Filter) ([]types.Container, error): The ListContainers implementation.
func listFiltered(
	selected *[]types.Container,
	containers ...types.Container,
) func(context.Context, ...types.Filter) ([]types.Container, error) {
	return func(_ context.Context, filters ...types.Filter) ([]types.Container, error) {
		for _, c := range containers {
			if !slices.ContainsFunc(filters, func(f types.Filter) bool { return !f(c) }) {
				*selected = append(*selected, c)
			}
		}

		return *selected, nil
	}
}

// TestScopeValidationErrorCases verifies that rolling restart validation
// checks only the containers a scope filter selects when scope labels are
// empty, malformed, or conflicting.
func TestScopeValidationErrorCases(t *testing.T) {
	t.Parallel()

	t.Run("scope labels are malformed", func(t *testing.T) {
		t.Parallel()

		t.Run("should handle empty scope labels gracefully", func(t *testing.T) {
			t.Parallel()

			// Test that empty scope labels are treated as unscoped
			mockClient := mockContainerClient.NewMockClient(t)

			// Container with no scope label
			container := labeledContainer("unscoped", nil)

			// Scoped container the filter excludes
			scoped := labeledContainer("scoped", map[string]string{containerLabels.Scope: "prod"})

			var selected []types.Container

			mockClient.EXPECT().
				ListContainers(mock.Anything, mock.Anything).
				RunAndReturn(listFiltered(&selected, container, scoped))

			// Create unscoped filter (empty scope should default to unscoped)
			filter := func(c types.FilterableContainer) bool {
				scope, hasScope := c.Scope()
				if !hasScope || scope == "" {
					return true // Unscoped containers pass
				}

				return false
			}

			err := ValidateRollingRestartDependencies(newTestLogger(t), t.Context(), mockClient, filter, true)

			require.NoError(t, err)
			assert.Equal(t, []types.Container{container}, selected)
		})

		t.Run("should handle containers with invalid scope label formats", func(t *testing.T) {
			t.Parallel()

			mockClient := mockContainerClient.NewMockClient(t)

			// Container with malformed scope label (e.g., contains special chars)
			container1 := labeledContainer("valid", map[string]string{containerLabels.Scope: "valid-scope"})
			container2 := labeledContainer("malformed", map[string]string{containerLabels.Scope: "invalid$scope@123"})

			var selected []types.Container

			mockClient.EXPECT().
				ListContainers(mock.Anything, mock.Anything).
				RunAndReturn(listFiltered(&selected, container1, container2))

			// Filter that matches malformed scope
			filter := func(c types.FilterableContainer) bool {
				scope, _ := c.Scope()

				return scope == "invalid$scope@123"
			}

			err := ValidateRollingRestartDependencies(newTestLogger(t), t.Context(), mockClient, filter, true)

			require.NoError(t, err)
			assert.Equal(t, []types.Container{container2}, selected)
		})

		t.Run("should handle scope labels with whitespace and normalization", func(t *testing.T) {
			t.Parallel()

			mockClient := mockContainerClient.NewMockClient(t)

			// Container with scope label containing leading and trailing spaces
			container := labeledContainer("spaced", map[string]string{containerLabels.Scope: "  spaced scope  "})

			// Container whose scope matches only after trimming
			trimmed := labeledContainer("trimmed", map[string]string{containerLabels.Scope: "spaced scope"})

			var selected []types.Container

			mockClient.EXPECT().
				ListContainers(mock.Anything, mock.Anything).
				RunAndReturn(listFiltered(&selected, container, trimmed))

			// Filter that expects the scope as written
			filter := func(c types.FilterableContainer) bool {
				scope, _ := c.Scope()

				return scope == "  spaced scope  " // Exact match including spaces
			}

			err := ValidateRollingRestartDependencies(newTestLogger(t), t.Context(), mockClient, filter, true)

			require.NoError(t, err)
			assert.Equal(t, []types.Container{container}, selected)
		})
	})

	t.Run("scope validation encounters errors", func(t *testing.T) {
		t.Parallel()

		t.Run("should handle scope label retrieval errors gracefully", func(t *testing.T) {
			t.Parallel()

			mockClient := mockContainerClient.NewMockClient(t)

			// Container without a scope label
			container := labeledContainer("unscoped", nil)

			// Scoped container the filter excludes
			scoped := labeledContainer("scoped", map[string]string{containerLabels.Scope: "prod"})

			var selected []types.Container

			mockClient.EXPECT().
				ListContainers(mock.Anything, mock.Anything).
				RunAndReturn(listFiltered(&selected, container, scoped))

			// Filter that handles missing scopes
			filter := func(c types.FilterableContainer) bool {
				_, hasScope := c.Scope()

				return !hasScope // Include containers without scope labels
			}

			err := ValidateRollingRestartDependencies(newTestLogger(t), t.Context(), mockClient, filter, true)

			require.NoError(t, err)
			assert.Equal(t, []types.Container{container}, selected)
		})

		t.Run("should validate scope consistency across operations", func(t *testing.T) {
			t.Parallel()

			mockClient := mockContainerClient.NewMockClient(t)

			// Multiple containers that should have consistent scope behavior
			container1 := labeledContainer("first", map[string]string{containerLabels.Scope: "consistent-scope"})
			container2 := labeledContainer("second", map[string]string{containerLabels.Scope: "consistent-scope"})

			// Container in another scope the filter excludes
			other := labeledContainer("other", map[string]string{containerLabels.Scope: "other-scope"})

			var selected []types.Container

			mockClient.EXPECT().
				ListContainers(mock.Anything, mock.Anything).
				RunAndReturn(listFiltered(&selected, container1, other, container2))

			// Filter requiring scope consistency
			filter := func(c types.FilterableContainer) bool {
				scope, hasScope := c.Scope()

				return hasScope && scope == "consistent-scope"
			}

			err := ValidateRollingRestartDependencies(newTestLogger(t), t.Context(), mockClient, filter, true)

			require.NoError(t, err)
			assert.Equal(t, []types.Container{container1, container2}, selected)
		})

		t.Run("should handle scope label conflicts during validation", func(t *testing.T) {
			t.Parallel()

			mockClient := mockContainerClient.NewMockClient(t)

			// Container whose scope label is present but empty
			container := labeledContainer("empty-scope", map[string]string{containerLabels.Scope: ""})

			// Container without a scope label the filter excludes
			unscoped := labeledContainer("unscoped", nil)

			var selected []types.Container

			mockClient.EXPECT().
				ListContainers(mock.Anything, mock.Anything).
				RunAndReturn(listFiltered(&selected, container, unscoped))

			// Filter that detects empty scope labels
			filter := func(c types.FilterableContainer) bool {
				scope, hasScope := c.Scope()

				return hasScope && scope == "" // Allow empty scoped containers
			}

			err := ValidateRollingRestartDependencies(newTestLogger(t), t.Context(), mockClient, filter, true)

			require.NoError(t, err)
			assert.Equal(t, []types.Container{container}, selected)
		})
	})
}

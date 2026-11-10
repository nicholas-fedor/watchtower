package update

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	gitConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/git"
	updateConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/update"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
)

// TestUpdateAction_MonitorOnly verifies that monitor-only containers, set by
// label or globally, are not updated and collect no images, and how the
// label-precedence setting resolves the two.
func TestUpdateAction_MonitorOnly(t *testing.T) {
	t.Parallel()

	t.Run("watchtower has been instructed to monitor only", func(t *testing.T) {
		t.Parallel()

		t.Run("certain containers are set to monitor only", func(t *testing.T) {
			t.Parallel()

			t.Run("should not update those containers and collect no image IDs", func(t *testing.T) {
				t.Parallel()

				client := mockUpdate.CreateFakeClient(
					&mockUpdate.TestData{
						NameOfContainerToKeep: "test-container-02",
						Containers: []*container.Container{
							mockUpdate.CreateMockContainer(
								"test-container-01",
								"test-container-01",
								"fake-image1:latest",
								time.Now(),
							),
							mockUpdate.CreateMockContainerWithConfig(
								"test-container-02",
								"/test-container-02",
								"fake-image2:latest",
								false,
								false,
								time.Now(),
								&dockerContainer.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower.monitor-only": "true",
									},
								},
							),
						},
					},
					false,
					false,
				)
				client.TestData.Staleness = map[string]bool{
					"test-container-01": true,
					"test-container-02": true,
				}
				report, cleanupImageInfos, err := update(newTestLogger(t), t.Context(), client, updateParams.Params{Cleanup: true}, ratelimit.New())

				require.NoError(t, err)
				require.Len(t, report.Updated(), 1)
				assert.Contains(t, removedImageIDs(cleanupImageInfos), container.ImageID("fake-image1:latest"))
				require.Len(t, cleanupImageInfos, 1)
				assert.Equal(t, int32(0), client.TestData.TriedToRemoveImageCount.Load(), "RemoveImageByID should not be called during Update")
			})
		})

		t.Run("monitor only is set globally", func(t *testing.T) {
			t.Parallel()

			t.Run("should not update any containers and collect no image IDs", func(t *testing.T) {
				t.Parallel()

				client := mockUpdate.CreateFakeClient(
					&mockUpdate.TestData{
						Containers: []*container.Container{
							mockUpdate.CreateMockContainer(
								"test-container-01",
								"test-container-01",
								"fake-image:latest",
								time.Now(),
							),
							mockUpdate.CreateMockContainer(
								"test-container-02",
								"test-container-02",
								"fake-image:latest",
								time.Now(),
							),
						},
					},
					false,
					false,
				)
				client.TestData.Staleness = map[string]bool{
					"test-container-01": true,
					"test-container-02": true,
				}
				report, cleanupImageInfos, err := update(newTestLogger(t), t.Context(), client, updateParams.Params{Cleanup: true, MonitorOnly: true}, ratelimit.New())

				require.NoError(t, err)
				assert.Empty(t, report.Updated())
				assert.Empty(t, cleanupImageInfos)
				assert.Equal(t, int32(0), client.TestData.TriedToRemoveImageCount.Load(), "RemoveImageByID should not be called during Update")
			})

			t.Run("watchtower has been instructed to have label take precedence", func(t *testing.T) {
				t.Parallel()

				t.Run("it should update containers when monitor only is set to false", func(t *testing.T) {
					t.Parallel()

					client := mockUpdate.CreateFakeClient(
						&mockUpdate.TestData{
							Containers: []*container.Container{
								mockUpdate.CreateMockContainerWithConfig(
									"test-container-02",
									"test-container-02",
									"fake-image2:latest",
									false,
									false,
									time.Now(),
									&dockerContainer.Config{
										Labels: map[string]string{
											"com.centurylinklabs.watchtower.monitor-only": "false",
										},
									},
								),
							},
						},
						false,
						false,
					)
					client.TestData.Staleness = map[string]bool{
						"test-container-02": true,
					}
					report, cleanupImageInfos, err := update(newTestLogger(t), t.Context(), client, updateParams.Params{
						Cleanup:         true,
						MonitorOnly:     true,
						LabelPrecedence: true,
					}, ratelimit.New())

					require.NoError(t, err)
					require.Len(t, report.Updated(), 1)
					assert.Contains(t, removedImageIDs(cleanupImageInfos), container.ImageID("fake-image2:latest"))
					require.Len(t, cleanupImageInfos, 1)
					assert.Equal(t, int32(0), client.TestData.TriedToRemoveImageCount.Load(), "RemoveImageByID should not be called during Update")
				})

				t.Run("it should not update containers when monitor only is set to true", func(t *testing.T) {
					t.Parallel()

					client := mockUpdate.CreateFakeClient(
						&mockUpdate.TestData{
							Containers: []*container.Container{
								mockUpdate.CreateMockContainerWithConfig(
									"test-container-02",
									"test-container-02",
									"fake-image2:latest",
									false,
									false,
									time.Now(),
									&dockerContainer.Config{
										Labels: map[string]string{
											"com.centurylinklabs.watchtower.monitor-only": "true",
										},
									},
								),
							},
						},
						false,
						false,
					)
					client.TestData.Staleness = map[string]bool{
						"test-container-02": true,
					}
					report, cleanupImageInfos, err := update(newTestLogger(t), t.Context(), client, updateParams.Params{
						Cleanup:         true,
						MonitorOnly:     true,
						LabelPrecedence: true,
					}, ratelimit.New())

					require.NoError(t, err)
					assert.Empty(t, report.Updated())
					assert.Empty(t, cleanupImageInfos)
					assert.Equal(t, int32(0), client.TestData.TriedToRemoveImageCount.Load(), "RemoveImageByID should not be called during Update")
				})

				t.Run("it should not update containers when monitor only is not set", func(t *testing.T) {
					t.Parallel()

					client := mockUpdate.CreateFakeClient(
						&mockUpdate.TestData{
							Containers: []*container.Container{
								mockUpdate.CreateMockContainer(
									"test-container-01",
									"test-container-01",
									"fake-image:latest",
									time.Now(),
								),
							},
						},
						false,
						false,
					)
					client.TestData.Staleness = map[string]bool{
						"test-container-01": true,
					}
					report, cleanupImageInfos, err := update(newTestLogger(t), t.Context(), client, updateParams.Params{
						Cleanup:         true,
						MonitorOnly:     true,
						LabelPrecedence: true,
					}, ratelimit.New())

					require.NoError(t, err)
					assert.Empty(t, report.Updated())
					assert.Empty(t, cleanupImageInfos)
					assert.Equal(t, int32(0), client.TestData.TriedToRemoveImageCount.Load(), "RemoveImageByID should not be called during Update")
				})
			})
		})
	})
}

// TestUpdate_GitWatchedNoPullIsNotRebuilt verifies that a Git-watched
// container with no-pull, set by label or globally, is reported stale when
// its remote advances, but is neither rebuilt nor recreated. The client mock
// expects only the listing, so a stop, build or recreate fails the test.
func TestUpdate_GitWatchedNoPullIsNotRebuilt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		labels map[string]string
		policy updateConfig.Policy
	}{
		{
			name:   "no-pull label",
			labels: map[string]string{labels.NoPull: "true"},
		},
		{
			name:   "no-pull configuration option",
			policy: updateConfig.Policy{NoPull: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app, gitClient, _ := gitWatchedTestContainer(t, tt.labels)

			client := newMockClient(t)
			client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]*container.Container{app}, nil).Once()

			report, _, err := update(newTestLogger(t), t.Context(), client,
				updateParams.Params{Git: gitConfig.Defaults{Enable: true}, Policy: tt.policy},
				ratelimit.New(), gitClient)
			require.NoError(t, err)

			require.Len(t, report.Stale(), 1)
			assert.Equal(t, app.ID(), report.Stale()[0].ID())
			assert.Empty(t, report.Updated())
			assert.Empty(t, report.Failed())
			assert.False(t, app.IsStale())
		})
	}
}

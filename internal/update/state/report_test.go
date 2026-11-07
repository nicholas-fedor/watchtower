package state

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

func TestReport_Scanned(t *testing.T) {
	tests := []struct {
		name string
		r    *Report
		want []types.ContainerReport
	}{
		{
			name: "empty report",
			r:    &Report{scanned: []types.ContainerReport{}},
			want: []types.ContainerReport{},
		},
		{
			name: "single scanned container",
			r: func() *Report {
				mock := mockTypes.NewMockContainerReport(t)
				mock.EXPECT().ID().Return(types.ContainerID("cont1"))
				// Only expect ID() since that's all we check
				return &Report{scanned: []types.ContainerReport{mock}}
			}(),
			want: []types.ContainerReport{
				func() types.ContainerReport {
					mock := mockTypes.NewMockContainerReport(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont1"))

					return mock
				}(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.Scanned()
			if len(got) != len(tt.want) {
				t.Errorf("report.Scanned() length = %d, want %d", len(got), len(tt.want))

				return
			}

			for i := range got {
				if got[i].ID() != tt.want[i].ID() {
					t.Errorf(
						"report.Scanned()[%d].ID() = %v, want %v",
						i,
						got[i].ID(),
						tt.want[i].ID(),
					)
				}
			}
		})
	}
}

func TestReport_Updated(t *testing.T) {
	tests := []struct {
		name string
		r    *Report
		want []types.ContainerReport
	}{
		{
			name: "empty report",
			r:    &Report{updated: []types.ContainerReport{}},
			want: []types.ContainerReport{},
		},
		{
			name: "single updated container",
			r: func() *Report {
				mock := mockTypes.NewMockContainerReport(t)
				mock.EXPECT().ID().Return(types.ContainerID("cont2"))

				return &Report{updated: []types.ContainerReport{mock}}
			}(),
			want: []types.ContainerReport{
				func() types.ContainerReport {
					mock := mockTypes.NewMockContainerReport(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont2"))

					return mock
				}(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.Updated()
			if len(got) != len(tt.want) {
				t.Errorf("report.Updated() length = %d, want %d", len(got), len(tt.want))

				return
			}

			for i := range got {
				if got[i].ID() != tt.want[i].ID() {
					t.Errorf(
						"report.Updated()[%d].ID() = %v, want %v",
						i,
						got[i].ID(),
						tt.want[i].ID(),
					)
				}
			}
		})
	}
}

func TestReport_Failed(t *testing.T) {
	tests := []struct {
		name string
		r    *Report
		want []types.ContainerReport
	}{
		{
			name: "empty report",
			r:    &Report{failed: []types.ContainerReport{}},
			want: []types.ContainerReport{},
		},
		{
			name: "single failed container",
			r: func() *Report {
				mock := mockTypes.NewMockContainerReport(t)
				mock.EXPECT().ID().Return(types.ContainerID("cont3"))

				return &Report{failed: []types.ContainerReport{mock}}
			}(),
			want: []types.ContainerReport{
				func() types.ContainerReport {
					mock := mockTypes.NewMockContainerReport(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont3"))

					return mock
				}(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.Failed()
			if len(got) != len(tt.want) {
				t.Errorf("report.Failed() length = %d, want %d", len(got), len(tt.want))

				return
			}

			for i := range got {
				if got[i].ID() != tt.want[i].ID() {
					t.Errorf(
						"report.Failed()[%d].ID() = %v, want %v",
						i,
						got[i].ID(),
						tt.want[i].ID(),
					)
				}
			}
		})
	}
}

func TestReport_Skipped(t *testing.T) {
	tests := []struct {
		name string
		r    *Report
		want []types.ContainerReport
	}{
		{
			name: "empty report",
			r:    &Report{skipped: []types.ContainerReport{}},
			want: []types.ContainerReport{},
		},
		{
			name: "single skipped container",
			r: func() *Report {
				mock := mockTypes.NewMockContainerReport(t)
				mock.EXPECT().ID().Return(types.ContainerID("cont4"))

				return &Report{skipped: []types.ContainerReport{mock}}
			}(),
			want: []types.ContainerReport{
				func() types.ContainerReport {
					mock := mockTypes.NewMockContainerReport(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont4"))

					return mock
				}(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.Skipped()
			if len(got) != len(tt.want) {
				t.Errorf("report.Skipped() length = %d, want %d", len(got), len(tt.want))

				return
			}

			for i := range got {
				if got[i].ID() != tt.want[i].ID() {
					t.Errorf(
						"report.Skipped()[%d].ID() = %v, want %v",
						i,
						got[i].ID(),
						tt.want[i].ID(),
					)
				}
			}
		})
	}
}

func TestReport_Stale(t *testing.T) {
	tests := []struct {
		name string
		r    *Report
		want []types.ContainerReport
	}{
		{
			name: "empty report",
			r:    &Report{stale: []types.ContainerReport{}},
			want: []types.ContainerReport{},
		},
		{
			name: "single stale container",
			r: func() *Report {
				mock := mockTypes.NewMockContainerReport(t)
				mock.EXPECT().ID().Return(types.ContainerID("cont5"))

				return &Report{stale: []types.ContainerReport{mock}}
			}(),
			want: []types.ContainerReport{
				func() types.ContainerReport {
					mock := mockTypes.NewMockContainerReport(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont5"))

					return mock
				}(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.Stale()
			if len(got) != len(tt.want) {
				t.Errorf("report.Stale() length = %d, want %d", len(got), len(tt.want))

				return
			}

			for i := range got {
				if got[i].ID() != tt.want[i].ID() {
					t.Errorf(
						"report.Stale()[%d].ID() = %v, want %v",
						i,
						got[i].ID(),
						tt.want[i].ID(),
					)
				}
			}
		})
	}
}

func TestReport_Fresh(t *testing.T) {
	tests := []struct {
		name string
		r    *Report
		want []types.ContainerReport
	}{
		{
			name: "empty report",
			r:    &Report{fresh: []types.ContainerReport{}},
			want: []types.ContainerReport{},
		},
		{
			name: "single fresh container",
			r: func() *Report {
				mock := mockTypes.NewMockContainerReport(t)
				mock.EXPECT().ID().Return(types.ContainerID("cont6"))

				return &Report{fresh: []types.ContainerReport{mock}}
			}(),
			want: []types.ContainerReport{
				func() types.ContainerReport {
					mock := mockTypes.NewMockContainerReport(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont6"))

					return mock
				}(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.Fresh()
			if len(got) != len(tt.want) {
				t.Errorf("report.Fresh() length = %d, want %d", len(got), len(tt.want))

				return
			}

			for i := range got {
				if got[i].ID() != tt.want[i].ID() {
					t.Errorf(
						"report.Fresh()[%d].ID() = %v, want %v",
						i,
						got[i].ID(),
						tt.want[i].ID(),
					)
				}
			}
		})
	}
}

func TestReport_Restarted(t *testing.T) {
	tests := []struct {
		name string
		r    *Report
		want []types.ContainerReport
	}{
		{
			name: "empty report",
			r:    &Report{restarted: []types.ContainerReport{}},
			want: []types.ContainerReport{},
		},
		{
			name: "single restarted container",
			r: func() *Report {
				mock := mockTypes.NewMockContainerReport(t)
				mock.EXPECT().ID().Return(types.ContainerID("cont7"))

				return &Report{restarted: []types.ContainerReport{mock}}
			}(),
			want: []types.ContainerReport{
				func() types.ContainerReport {
					mock := mockTypes.NewMockContainerReport(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont7"))

					return mock
				}(),
			},
		},
		{
			name: "multiple restarted containers",
			r: func() *Report {
				mock1 := mockTypes.NewMockContainerReport(t)
				mock1.EXPECT().ID().Return(types.ContainerID("cont8"))

				mock2 := mockTypes.NewMockContainerReport(t)
				mock2.EXPECT().ID().Return(types.ContainerID("cont9"))

				return &Report{restarted: []types.ContainerReport{mock1, mock2}}
			}(),
			want: []types.ContainerReport{
				func() types.ContainerReport {
					mock := mockTypes.NewMockContainerReport(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont8"))

					return mock
				}(),
				func() types.ContainerReport {
					mock := mockTypes.NewMockContainerReport(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont9"))

					return mock
				}(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.Restarted()
			if len(got) != len(tt.want) {
				t.Errorf("report.Restarted() length = %d, want %d", len(got), len(tt.want))

				return
			}

			for i := range got {
				if got[i].ID() != tt.want[i].ID() {
					t.Errorf(
						"report.Restarted()[%d].ID() = %v, want %v",
						i,
						got[i].ID(),
						tt.want[i].ID(),
					)
				}
			}
		})
	}
}

func TestReport_All(t *testing.T) {
	tests := []struct {
		name string
		r    *Report
		want []string
	}{
		{
			name: "empty report",
			r:    &Report{},
			want: []string{},
		},
		{
			name: "mixed containers with deduplication",
			r: func() *Report {
				mock1 := mockTypes.NewMockContainerReport(t)
				// No strict expectation. Allow calls without failing if unmet
				mock1.EXPECT().ID().Return(types.ContainerID("cont1")).Times(0)

				mock2 := mockTypes.NewMockContainerReport(t)
				mock2.EXPECT().ID().Return(types.ContainerID("cont2")).Times(0)

				return &Report{
					updated: []types.ContainerReport{mock1},
					scanned: []types.ContainerReport{mock1, mock2},
				}
			}(),
			want: []string{"cont1", "cont2"},
		},
		{
			name: "containers with restarted priority",
			r: func() *Report {
				mock1 := mockTypes.NewMockContainerReport(t)
				mock1.EXPECT().ID().Return(types.ContainerID("cont1")).Times(0)

				mock2 := mockTypes.NewMockContainerReport(t)
				mock2.EXPECT().ID().Return(types.ContainerID("cont2")).Times(0)

				mock3 := mockTypes.NewMockContainerReport(t)
				mock3.EXPECT().ID().Return(types.ContainerID("cont3")).Times(0)

				return &Report{
					updated:   []types.ContainerReport{mock1},
					restarted: []types.ContainerReport{mock2},
					failed:    []types.ContainerReport{mock3},
					scanned:   []types.ContainerReport{mock1, mock2, mock3},
				}
			}(),
			want: []string{"cont1", "cont2", "cont3"}, // updated first, then restarted, then failed
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.All()
			if len(got) != len(tt.want) {
				t.Errorf("report.All() length = %d, want %d", len(got), len(tt.want))

				return
			}

			for i := range got {
				t.Logf("Calling ID() on got[%d]", i)

				gotID := got[i].ID()
				if gotID != types.ContainerID(tt.want[i]) {
					t.Errorf("report.All()[%d].ID() = %v, want %v", i, gotID, tt.want[i])
				}
			}
		})
	}
}

// TestReport_AllKeepsMostSignificant verifies that All keeps the entry from
// the most significant category when two entries share a container ID.
func TestReport_AllKeepsMostSignificant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// moreSignificant and lessSignificant set the two lists that hold an
		// entry with the same container ID.
		moreSignificant, lessSignificant func(r *Report, entry types.ContainerReport)
	}{
		{
			name:            "updated over restarted",
			moreSignificant: func(r *Report, e types.ContainerReport) { r.updated = []types.ContainerReport{e} },
			lessSignificant: func(r *Report, e types.ContainerReport) { r.restarted = []types.ContainerReport{e} },
		},
		{
			name:            "restarted over failed",
			moreSignificant: func(r *Report, e types.ContainerReport) { r.restarted = []types.ContainerReport{e} },
			lessSignificant: func(r *Report, e types.ContainerReport) { r.failed = []types.ContainerReport{e} },
		},
		{
			name:            "failed over scanned",
			moreSignificant: func(r *Report, e types.ContainerReport) { r.failed = []types.ContainerReport{e} },
			lessSignificant: func(r *Report, e types.ContainerReport) { r.scanned = []types.ContainerReport{e} },
		},
		{
			name:            "fresh over scanned",
			moreSignificant: func(r *Report, e types.ContainerReport) { r.fresh = []types.ContainerReport{e} },
			lessSignificant: func(r *Report, e types.ContainerReport) { r.scanned = []types.ContainerReport{e} },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			kept := &ContainerStatus{containerID: "cont1", containerName: "kept"}
			dropped := &ContainerStatus{containerID: "cont1", containerName: "dropped"}

			report := &Report{}
			tt.moreSignificant(report, kept)
			tt.lessSignificant(report, dropped)

			all := report.All()
			require.Len(t, all, 1)
			assert.Same(t, kept, all[0])
		})
	}
}

// TestReport_Focus verifies that a split report returns the container it is
// about and that the report of a whole session has no focus.
func TestReport_Focus(t *testing.T) {
	t.Parallel()

	updated := &ContainerStatus{containerID: "cont1", containerName: "updated", state: UpdatedState}
	session := &Report{
		scanned: []types.ContainerReport{updated},
		updated: []types.ContainerReport{updated},
	}

	assert.Nil(t, session.Focus())
	assert.Same(t, updated, NewSingleContainerReport(updated, session).Focus())
}

func TestNewReport(t *testing.T) {
	type args struct {
		progress Progress
	}

	tests := []struct {
		name string
		args args
		want *Report
	}{
		{
			name: "empty progress",
			args: args{progress: Progress{}},
			want: &Report{
				scanned:   []types.ContainerReport{},
				updated:   []types.ContainerReport{},
				failed:    []types.ContainerReport{},
				skipped:   []types.ContainerReport{},
				stale:     []types.ContainerReport{},
				fresh:     []types.ContainerReport{},
				restarted: []types.ContainerReport{},
			},
		},
		{
			name: "mixed states",
			args: args{
				progress: Progress{
					"cont1": &ContainerStatus{
						state:         SkippedState,
						containerID:   "cont1",
						containerName: "container1",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image1:latest",
					},
					"cont2": &ContainerStatus{
						state:         UpdatedState,
						containerID:   "cont2",
						containerName: "container2",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image2:latest",
					},
					"cont3": &ContainerStatus{
						state:          FailedState,
						containerID:    "cont3",
						containerName:  "container3",
						oldImage:       "img1",
						newImage:       "img2",
						imageName:      "image3:latest",
						containerError: errors.New("failed"),
					},
					"cont4": &ContainerStatus{
						state:         ScannedState,
						containerID:   "cont4",
						containerName: "container4",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image4:latest",
					},
					"cont5": &ContainerStatus{
						state:         ScannedState,
						containerID:   "cont5",
						containerName: "container5",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image5:latest",
					},
				},
			},
			want: &Report{
				scanned: []types.ContainerReport{
					&ContainerStatus{
						state:         UpdatedState,
						containerID:   "cont2",
						containerName: "container2",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image2:latest",
					},
					&ContainerStatus{
						state:          FailedState,
						containerID:    "cont3",
						containerName:  "container3",
						oldImage:       "img1",
						newImage:       "img2",
						imageName:      "image3:latest",
						containerError: errors.New("failed"),
					},
					&ContainerStatus{
						state:         FreshState,
						containerID:   "cont4",
						containerName: "container4",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image4:latest",
					},
					&ContainerStatus{
						state:         StaleState,
						containerID:   "cont5",
						containerName: "container5",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image5:latest",
					},
				},
				updated: []types.ContainerReport{
					&ContainerStatus{
						state:         UpdatedState,
						containerID:   "cont2",
						containerName: "container2",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image2:latest",
					},
				},
				failed: []types.ContainerReport{
					&ContainerStatus{
						state:          FailedState,
						containerID:    "cont3",
						containerName:  "container3",
						oldImage:       "img1",
						newImage:       "img2",
						imageName:      "image3:latest",
						containerError: errors.New("failed"),
					},
				},
				skipped: []types.ContainerReport{
					&ContainerStatus{
						state:         SkippedState,
						containerID:   "cont1",
						containerName: "container1",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image1:latest",
					},
				},
				stale: []types.ContainerReport{
					&ContainerStatus{
						state:         StaleState,
						containerID:   "cont5",
						containerName: "container5",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image5:latest",
					},
				},
				fresh: []types.ContainerReport{
					&ContainerStatus{
						state:         FreshState,
						containerID:   "cont4",
						containerName: "container4",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image4:latest",
					},
				},
				restarted: []types.ContainerReport{},
			},
		},
		{
			name: "stale container with unknown state",
			args: args{
				progress: Progress{
					"cont6": &ContainerStatus{
						state:         UnknownState,
						containerID:   "cont6",
						containerName: "container6",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image6:latest",
					},
				},
			},
			want: &Report{
				scanned: []types.ContainerReport{
					&ContainerStatus{
						state:         StaleState,
						containerID:   "cont6",
						containerName: "container6",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image6:latest",
					},
				},
				updated: []types.ContainerReport{},
				failed:  []types.ContainerReport{},
				skipped: []types.ContainerReport{},
				stale: []types.ContainerReport{
					&ContainerStatus{
						state:         StaleState,
						containerID:   "cont6",
						containerName: "container6",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image6:latest",
					},
				},
				fresh:     []types.ContainerReport{},
				restarted: []types.ContainerReport{},
			},
		},
		{
			name: "stale container with initial stale state",
			args: args{
				progress: Progress{
					"cont7": &ContainerStatus{
						state:         StaleState,
						containerID:   "cont7",
						containerName: "container7",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image7:latest",
					},
				},
			},
			want: &Report{
				scanned: []types.ContainerReport{
					&ContainerStatus{
						state:         StaleState,
						containerID:   "cont7",
						containerName: "container7",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image7:latest",
					},
				},
				updated: []types.ContainerReport{},
				failed:  []types.ContainerReport{},
				skipped: []types.ContainerReport{},
				stale: []types.ContainerReport{
					&ContainerStatus{
						state:         StaleState,
						containerID:   "cont7",
						containerName: "container7",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image7:latest",
					},
				},
				fresh:     []types.ContainerReport{},
				restarted: []types.ContainerReport{},
			},
		},
		{
			name: "restarted container",
			args: args{
				progress: Progress{
					"cont8": &ContainerStatus{
						state:         RestartedState,
						containerID:   "cont8",
						containerName: "container8",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image8:latest",
					},
				},
			},
			want: &Report{
				scanned: []types.ContainerReport{
					&ContainerStatus{
						state:         RestartedState,
						containerID:   "cont8",
						containerName: "container8",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image8:latest",
					},
				},
				updated: []types.ContainerReport{},
				failed:  []types.ContainerReport{},
				skipped: []types.ContainerReport{},
				stale:   []types.ContainerReport{},
				fresh:   []types.ContainerReport{},
				restarted: []types.ContainerReport{
					&ContainerStatus{
						state:         RestartedState,
						containerID:   "cont8",
						containerName: "container8",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image8:latest",
					},
				},
			},
		},
		{
			name: "multiple restarted containers",
			args: args{
				progress: Progress{
					"cont9": &ContainerStatus{
						state:         RestartedState,
						containerID:   "cont9",
						containerName: "container9",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image9:latest",
					},
					"cont10": &ContainerStatus{
						state:         RestartedState,
						containerID:   "cont10",
						containerName: "container10",
						oldImage:      "img2",
						newImage:      "img2",
						imageName:     "image10:latest",
					},
				},
			},
			want: &Report{
				scanned: []types.ContainerReport{
					&ContainerStatus{
						state:         RestartedState,
						containerID:   "cont10",
						containerName: "container10",
						oldImage:      "img2",
						newImage:      "img2",
						imageName:     "image10:latest",
					},
					&ContainerStatus{
						state:         RestartedState,
						containerID:   "cont9",
						containerName: "container9",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image9:latest",
					},
				},
				updated: []types.ContainerReport{},
				failed:  []types.ContainerReport{},
				skipped: []types.ContainerReport{},
				stale:   []types.ContainerReport{},
				fresh:   []types.ContainerReport{},
				restarted: []types.ContainerReport{
					&ContainerStatus{
						state:         RestartedState,
						containerID:   "cont10",
						containerName: "container10",
						oldImage:      "img2",
						newImage:      "img2",
						imageName:     "image10:latest",
					},
					&ContainerStatus{
						state:         RestartedState,
						containerID:   "cont9",
						containerName: "container9",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image9:latest",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newReport(newTestLogger(t), tt.args.progress)
			if !compareReports(got, tt.want) {
				t.Errorf("newReport(testLog(), ) = %+v, want %+v", got, tt.want)
			}
			// Explicitly exercise Stale() to ensure coverage
			stale := got.Stale()
			wantStale := tt.want.Stale()

			if len(stale) != len(wantStale) {
				t.Errorf("newReport(testLog(), ).Stale() length = %d, want %d", len(stale), len(wantStale))
			}

			for i := range stale {
				if stale[i].ID() != wantStale[i].ID() {
					t.Errorf(
						"newReport(testLog(), ).Stale()[%d].ID() = %v, want %v",
						i,
						stale[i].ID(),
						wantStale[i].ID(),
					)
				}
			}
		})
	}
}

// compareReports compares two report structs by their fields, ignoring pointer addresses.
func compareReports(got, want *Report) bool {
	compareSlice := func(gotSlice, wantSlice []types.ContainerReport) bool {
		if len(gotSlice) != len(wantSlice) {
			return false
		}

		for i := range gotSlice {
			g := gotSlice[i].(*ContainerStatus)

			w := wantSlice[i].(*ContainerStatus)
			if g.containerID != w.containerID ||
				g.state != w.state ||
				g.oldImage != w.oldImage ||
				g.newImage != w.newImage ||
				g.containerName != w.containerName ||
				g.imageName != w.imageName ||
				g.Error() != w.Error() { // Compare error strings
				return false
			}
		}

		return true
	}

	return compareSlice(got.scanned, want.scanned) &&
		compareSlice(got.updated, want.updated) &&
		compareSlice(got.failed, want.failed) &&
		compareSlice(got.skipped, want.skipped) &&
		compareSlice(got.stale, want.stale) &&
		compareSlice(got.fresh, want.fresh) &&
		compareSlice(got.restarted, want.restarted)
}

func Test_categorizeContainer(t *testing.T) {
	type args struct {
		r      *Report
		update *ContainerStatus
	}

	tests := []struct {
		name string
		args args
		want *Report
	}{
		{
			name: "skipped container",
			args: args{
				r:      &Report{},
				update: &ContainerStatus{state: SkippedState, containerID: "cont1"},
			},
			want: &Report{
				skipped: []types.ContainerReport{
					&ContainerStatus{state: SkippedState, containerID: "cont1"},
				},
			},
		},
		{
			name: "fresh container",
			args: args{
				r: &Report{},
				update: &ContainerStatus{
					state:       ScannedState,
					oldImage:    "img1",
					newImage:    "img1",
					containerID: "cont2",
				},
			},
			want: &Report{
				scanned: []types.ContainerReport{
					&ContainerStatus{
						state:       FreshState,
						oldImage:    "img1",
						newImage:    "img1",
						containerID: "cont2",
					},
				},
				fresh: []types.ContainerReport{
					&ContainerStatus{
						state:       FreshState,
						oldImage:    "img1",
						newImage:    "img1",
						containerID: "cont2",
					},
				},
			},
		},
		{
			name: "updated container",
			args: args{
				r: &Report{},
				update: &ContainerStatus{
					state:       UpdatedState,
					oldImage:    "img1",
					newImage:    "img2",
					containerID: "cont3",
				},
			},
			want: &Report{
				scanned: []types.ContainerReport{
					&ContainerStatus{
						state:       UpdatedState,
						oldImage:    "img1",
						newImage:    "img2",
						containerID: "cont3",
					},
				},
				updated: []types.ContainerReport{
					&ContainerStatus{
						state:       UpdatedState,
						oldImage:    "img1",
						newImage:    "img2",
						containerID: "cont3",
					},
				},
			},
		},
		{
			name: "restarted container",
			args: args{
				r: &Report{},
				update: &ContainerStatus{
					state:       RestartedState,
					oldImage:    "img1",
					newImage:    "img1",
					containerID: "cont4",
				},
			},
			want: &Report{
				scanned: []types.ContainerReport{
					&ContainerStatus{
						state:       RestartedState,
						oldImage:    "img1",
						newImage:    "img1",
						containerID: "cont4",
					},
				},
				restarted: []types.ContainerReport{
					&ContainerStatus{
						state:       RestartedState,
						oldImage:    "img1",
						newImage:    "img1",
						containerID: "cont4",
					},
				},
			},
		},
		{
			name: "restarted container with different images",
			args: args{
				r: &Report{},
				update: &ContainerStatus{
					state:       RestartedState,
					oldImage:    "img1",
					newImage:    "img2",
					containerID: "cont5",
				},
			},
			want: &Report{
				scanned: []types.ContainerReport{
					&ContainerStatus{
						state:       RestartedState,
						oldImage:    "img1",
						newImage:    "img2",
						containerID: "cont5",
					},
				},
				restarted: []types.ContainerReport{
					&ContainerStatus{
						state:       RestartedState,
						oldImage:    "img1",
						newImage:    "img2",
						containerID: "cont5",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			categorizeContainer(newTestLogger(t), tt.args.r, tt.args.update)

			if !compareReports(tt.args.r, tt.want) {
				t.Errorf("categorizeContainer(testLog(), ) resulted in %v, want %v", tt.args.r, tt.want)
			}
		})
	}
}

func Test_sortCategories(t *testing.T) {
	type args struct {
		r *Report
	}

	tests := []struct {
		name string
		args args
		want *Report
	}{
		{
			name: "unsorted scanned",
			args: args{
				r: &Report{
					scanned: []types.ContainerReport{
						&ContainerStatus{containerID: "cont2"},
						&ContainerStatus{containerID: "cont1"},
					},
				},
			},
			want: &Report{
				scanned: []types.ContainerReport{
					&ContainerStatus{containerID: "cont1"},
					&ContainerStatus{containerID: "cont2"},
				},
			},
		},
		{
			name: "mixed unsorted",
			args: args{
				r: &Report{
					updated: []types.ContainerReport{
						&ContainerStatus{containerID: "cont3"},
						&ContainerStatus{containerID: "cont1"},
					},
					failed: []types.ContainerReport{
						&ContainerStatus{containerID: "cont4"},
						&ContainerStatus{containerID: "cont2"},
					},
				},
			},
			want: &Report{
				updated: []types.ContainerReport{
					&ContainerStatus{containerID: "cont1"},
					&ContainerStatus{containerID: "cont3"},
				},
				failed: []types.ContainerReport{
					&ContainerStatus{containerID: "cont2"},
					&ContainerStatus{containerID: "cont4"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortCategories(tt.args.r)

			if !compareReports(tt.args.r, tt.want) {
				t.Errorf("sortCategories() resulted in %v, want %v", tt.args.r, tt.want)
			}
		})
	}
}

// TestCompareIDs verifies that compareIDs orders container reports by
// container ID.
func TestCompareIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b types.ContainerID
		want int
	}{
		{name: "first ID sorts first", a: "aaa", b: "bbb", want: -1},
		{name: "second ID sorts first", a: "bbb", b: "aaa", want: 1},
		{name: "equal IDs", a: "aaa", b: "aaa", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := mockTypes.NewMockContainerReport(t)
			a.EXPECT().ID().Return(tt.a)

			b := mockTypes.NewMockContainerReport(t)
			b.EXPECT().ID().Return(tt.b)

			assert.Equal(t, tt.want, compareIDs(a, b))
		})
	}
}

// TestNewSingleContainerReport verifies the context rule of split reports. The
// updated and restarted lists hold the focus container only when the session
// lists it there. The other lists are the session's whatever the focus.
func TestNewSingleContainerReport(t *testing.T) {
	t.Parallel()

	updated := &ContainerStatus{containerName: "updated", state: UpdatedState}
	restarted := &ContainerStatus{containerName: "restarted", state: RestartedState}
	stale := &ContainerStatus{containerName: "stale", state: StaleState}
	skipped := &ContainerStatus{containerName: "skipped", state: SkippedState}
	failed := &ContainerStatus{containerName: "failed", state: FailedState}
	fresh := &ContainerStatus{containerName: "fresh", state: FreshState}
	unlisted := &ContainerStatus{containerName: "unlisted", state: UpdatedState}

	session := &Report{
		scanned:   []types.ContainerReport{updated, restarted, stale, failed, fresh},
		updated:   []types.ContainerReport{updated},
		restarted: []types.ContainerReport{restarted},
		stale:     []types.ContainerReport{stale},
		skipped:   []types.ContainerReport{skipped},
		failed:    []types.ContainerReport{failed},
		fresh:     []types.ContainerReport{fresh},
	}

	// withContext returns a split report about focus with the session's
	// context lists and the given primary lists.
	withContext := func(focus types.ContainerReport, updatedList, restartedList []types.ContainerReport) *Report {
		return &Report{
			focus:     focus,
			updated:   updatedList,
			restarted: restartedList,
			scanned:   session.scanned,
			failed:    session.failed,
			skipped:   session.skipped,
			stale:     session.stale,
			fresh:     session.fresh,
		}
	}

	tests := []struct {
		name  string
		focus types.ContainerReport
		want  *Report
	}{
		{
			name:  "updated container",
			focus: updated,
			want:  withContext(updated, []types.ContainerReport{updated}, nil),
		},
		{
			name:  "restarted container",
			focus: restarted,
			want:  withContext(restarted, nil, []types.ContainerReport{restarted}),
		},
		{name: "stale container", focus: stale, want: withContext(stale, nil, nil)},
		{name: "skipped container", focus: skipped, want: withContext(skipped, nil, nil)},
		{
			// The session decides the category, not the container's own state.
			name:  "updated state the session does not list as updated",
			focus: unlisted,
			want:  withContext(unlisted, nil, nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, NewSingleContainerReport(tt.focus, session))
		})
	}
}

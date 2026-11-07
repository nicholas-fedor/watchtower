package state

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestReport_Lists verifies that each list method returns its own category,
// whether it holds no, one or several containers.
func TestReport_Lists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// list returns the category under test.
		list func(*Report) []*ContainerStatus
		// set stores containers in that category only.
		set  func(*Report, []*ContainerStatus)
		name string
	}{
		{name: "scanned", list: (*Report).Scanned, set: func(r *Report, c []*ContainerStatus) { r.scanned = c }},
		{name: "updated", list: (*Report).Updated, set: func(r *Report, c []*ContainerStatus) { r.updated = c }},
		{name: "failed", list: (*Report).Failed, set: func(r *Report, c []*ContainerStatus) { r.failed = c }},
		{name: "skipped", list: (*Report).Skipped, set: func(r *Report, c []*ContainerStatus) { r.skipped = c }},
		{name: "stale", list: (*Report).Stale, set: func(r *Report, c []*ContainerStatus) { r.stale = c }},
		{name: "fresh", list: (*Report).Fresh, set: func(r *Report, c []*ContainerStatus) { r.fresh = c }},
		{
			name: "restarted",
			list: (*Report).Restarted,
			set:  func(r *Report, c []*ContainerStatus) { r.restarted = c },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, want := range [][]*ContainerStatus{
				{},
				{{containerID: "cont1"}},
				{{containerID: "cont1"}, {containerID: "cont2"}},
			} {
				report := &Report{}
				tt.set(report, want)

				assert.Equal(t, want, tt.list(report))
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
				status1 := &ContainerStatus{containerID: "cont1"}
				status2 := &ContainerStatus{containerID: "cont2"}

				return &Report{
					updated: []*ContainerStatus{status1},
					scanned: []*ContainerStatus{status1, status2},
				}
			}(),
			want: []string{"cont1", "cont2"},
		},
		{
			name: "containers with restarted priority",
			r: func() *Report {
				status1 := &ContainerStatus{containerID: "cont1"}
				status2 := &ContainerStatus{containerID: "cont2"}
				status3 := &ContainerStatus{containerID: "cont3"}

				return &Report{
					updated:   []*ContainerStatus{status1},
					restarted: []*ContainerStatus{status2},
					failed:    []*ContainerStatus{status3},
					scanned:   []*ContainerStatus{status1, status2, status3},
				}
			}(),
			want: []string{"cont1", "cont2", "cont3"}, // updated first, then restarted, then failed
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := []string{}
			for _, status := range tt.r.All() {
				ids = append(ids, string(status.ID()))
			}

			assert.Equal(t, tt.want, ids)
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
		moreSignificant, lessSignificant func(r *Report, entry *ContainerStatus)
	}{
		{
			name:            "updated over restarted",
			moreSignificant: func(r *Report, e *ContainerStatus) { r.updated = []*ContainerStatus{e} },
			lessSignificant: func(r *Report, e *ContainerStatus) { r.restarted = []*ContainerStatus{e} },
		},
		{
			name:            "restarted over failed",
			moreSignificant: func(r *Report, e *ContainerStatus) { r.restarted = []*ContainerStatus{e} },
			lessSignificant: func(r *Report, e *ContainerStatus) { r.failed = []*ContainerStatus{e} },
		},
		{
			name:            "failed over scanned",
			moreSignificant: func(r *Report, e *ContainerStatus) { r.failed = []*ContainerStatus{e} },
			lessSignificant: func(r *Report, e *ContainerStatus) { r.scanned = []*ContainerStatus{e} },
		},
		{
			name:            "fresh over scanned",
			moreSignificant: func(r *Report, e *ContainerStatus) { r.fresh = []*ContainerStatus{e} },
			lessSignificant: func(r *Report, e *ContainerStatus) { r.scanned = []*ContainerStatus{e} },
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
		scanned: []*ContainerStatus{updated},
		updated: []*ContainerStatus{updated},
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
				scanned:   []*ContainerStatus{},
				updated:   []*ContainerStatus{},
				failed:    []*ContainerStatus{},
				skipped:   []*ContainerStatus{},
				stale:     []*ContainerStatus{},
				fresh:     []*ContainerStatus{},
				restarted: []*ContainerStatus{},
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
				scanned: []*ContainerStatus{
					{
						state:         UpdatedState,
						containerID:   "cont2",
						containerName: "container2",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image2:latest",
					},
					{
						state:          FailedState,
						containerID:    "cont3",
						containerName:  "container3",
						oldImage:       "img1",
						newImage:       "img2",
						imageName:      "image3:latest",
						containerError: errors.New("failed"),
					},
					{
						state:         FreshState,
						containerID:   "cont4",
						containerName: "container4",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image4:latest",
					},
					{
						state:         StaleState,
						containerID:   "cont5",
						containerName: "container5",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image5:latest",
					},
				},
				updated: []*ContainerStatus{
					{
						state:         UpdatedState,
						containerID:   "cont2",
						containerName: "container2",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image2:latest",
					},
				},
				failed: []*ContainerStatus{
					{
						state:          FailedState,
						containerID:    "cont3",
						containerName:  "container3",
						oldImage:       "img1",
						newImage:       "img2",
						imageName:      "image3:latest",
						containerError: errors.New("failed"),
					},
				},
				skipped: []*ContainerStatus{
					{
						state:         SkippedState,
						containerID:   "cont1",
						containerName: "container1",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image1:latest",
					},
				},
				stale: []*ContainerStatus{
					{
						state:         StaleState,
						containerID:   "cont5",
						containerName: "container5",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image5:latest",
					},
				},
				fresh: []*ContainerStatus{
					{
						state:         FreshState,
						containerID:   "cont4",
						containerName: "container4",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image4:latest",
					},
				},
				restarted: []*ContainerStatus{},
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
				scanned: []*ContainerStatus{
					{
						state:         StaleState,
						containerID:   "cont6",
						containerName: "container6",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image6:latest",
					},
				},
				updated: []*ContainerStatus{},
				failed:  []*ContainerStatus{},
				skipped: []*ContainerStatus{},
				stale: []*ContainerStatus{
					{
						state:         StaleState,
						containerID:   "cont6",
						containerName: "container6",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image6:latest",
					},
				},
				fresh:     []*ContainerStatus{},
				restarted: []*ContainerStatus{},
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
				scanned: []*ContainerStatus{
					{
						state:         StaleState,
						containerID:   "cont7",
						containerName: "container7",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image7:latest",
					},
				},
				updated: []*ContainerStatus{},
				failed:  []*ContainerStatus{},
				skipped: []*ContainerStatus{},
				stale: []*ContainerStatus{
					{
						state:         StaleState,
						containerID:   "cont7",
						containerName: "container7",
						oldImage:      "img1",
						newImage:      "img2",
						imageName:     "image7:latest",
					},
				},
				fresh:     []*ContainerStatus{},
				restarted: []*ContainerStatus{},
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
				scanned: []*ContainerStatus{
					{
						state:         RestartedState,
						containerID:   "cont8",
						containerName: "container8",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image8:latest",
					},
				},
				updated: []*ContainerStatus{},
				failed:  []*ContainerStatus{},
				skipped: []*ContainerStatus{},
				stale:   []*ContainerStatus{},
				fresh:   []*ContainerStatus{},
				restarted: []*ContainerStatus{
					{
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
				scanned: []*ContainerStatus{
					{
						state:         RestartedState,
						containerID:   "cont10",
						containerName: "container10",
						oldImage:      "img2",
						newImage:      "img2",
						imageName:     "image10:latest",
					},
					{
						state:         RestartedState,
						containerID:   "cont9",
						containerName: "container9",
						oldImage:      "img1",
						newImage:      "img1",
						imageName:     "image9:latest",
					},
				},
				updated: []*ContainerStatus{},
				failed:  []*ContainerStatus{},
				skipped: []*ContainerStatus{},
				stale:   []*ContainerStatus{},
				fresh:   []*ContainerStatus{},
				restarted: []*ContainerStatus{
					{
						state:         RestartedState,
						containerID:   "cont10",
						containerName: "container10",
						oldImage:      "img2",
						newImage:      "img2",
						imageName:     "image10:latest",
					},
					{
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
			assert.Equal(t, tt.want, got)
		})
	}
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
				skipped: []*ContainerStatus{
					{state: SkippedState, containerID: "cont1"},
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
				scanned: []*ContainerStatus{
					{
						state:       FreshState,
						oldImage:    "img1",
						newImage:    "img1",
						containerID: "cont2",
					},
				},
				fresh: []*ContainerStatus{
					{
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
				scanned: []*ContainerStatus{
					{
						state:       UpdatedState,
						oldImage:    "img1",
						newImage:    "img2",
						containerID: "cont3",
					},
				},
				updated: []*ContainerStatus{
					{
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
				scanned: []*ContainerStatus{
					{
						state:       RestartedState,
						oldImage:    "img1",
						newImage:    "img1",
						containerID: "cont4",
					},
				},
				restarted: []*ContainerStatus{
					{
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
				scanned: []*ContainerStatus{
					{
						state:       RestartedState,
						oldImage:    "img1",
						newImage:    "img2",
						containerID: "cont5",
					},
				},
				restarted: []*ContainerStatus{
					{
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

			assert.Equal(t, tt.want, tt.args.r)
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
					scanned: []*ContainerStatus{
						{containerID: "cont2"},
						{containerID: "cont1"},
					},
				},
			},
			want: &Report{
				scanned: []*ContainerStatus{
					{containerID: "cont1"},
					{containerID: "cont2"},
				},
			},
		},
		{
			name: "mixed unsorted",
			args: args{
				r: &Report{
					updated: []*ContainerStatus{
						{containerID: "cont3"},
						{containerID: "cont1"},
					},
					failed: []*ContainerStatus{
						{containerID: "cont4"},
						{containerID: "cont2"},
					},
				},
			},
			want: &Report{
				updated: []*ContainerStatus{
					{containerID: "cont1"},
					{containerID: "cont3"},
				},
				failed: []*ContainerStatus{
					{containerID: "cont2"},
					{containerID: "cont4"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortCategories(tt.args.r)

			assert.Equal(t, tt.want, tt.args.r)
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

			a := &ContainerStatus{containerID: tt.a}
			b := &ContainerStatus{containerID: tt.b}

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
		scanned:   []*ContainerStatus{updated, restarted, stale, failed, fresh},
		updated:   []*ContainerStatus{updated},
		restarted: []*ContainerStatus{restarted},
		stale:     []*ContainerStatus{stale},
		skipped:   []*ContainerStatus{skipped},
		failed:    []*ContainerStatus{failed},
		fresh:     []*ContainerStatus{fresh},
	}

	// withContext returns a split report about focus with the session's
	// context lists and the given primary lists.
	withContext := func(focus *ContainerStatus, updatedList, restartedList []*ContainerStatus) *Report {
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
		focus *ContainerStatus
		want  *Report
	}{
		{
			name:  "updated container",
			focus: updated,
			want:  withContext(updated, []*ContainerStatus{updated}, nil),
		},
		{
			name:  "restarted container",
			focus: restarted,
			want:  withContext(restarted, nil, []*ContainerStatus{restarted}),
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

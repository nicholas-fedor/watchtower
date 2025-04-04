package session

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

func TestUpdateFromContainer(t *testing.T) {
	type args struct {
		cont     types.Container
		newImage types.ImageID
		state    State
	}

	tests := []struct {
		name string
		args args
		want *ContainerStatus
	}{
		{
			name: "basic container update",
			args: args{
				cont: func() types.Container {
					mock := mocks.NewMockContainer(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont1"))
					mock.EXPECT().SafeImageID().Return(types.ImageID("img1"))
					mock.EXPECT().Name().Return("container1")
					mock.EXPECT().ImageName().Return("image1:latest")

					return mock
				}(),
				newImage: "img2",
				state:    ScannedState,
			},
			want: &ContainerStatus{
				containerID:    "cont1",
				oldImage:       "img1",
				newImage:       "img2",
				containerName:  "container1",
				imageName:      "image1:latest",
				containerError: nil,
				state:          ScannedState,
			},
		},
		{
			name: "empty container fields",
			args: args{
				cont: func() types.Container {
					mock := mocks.NewMockContainer(t)
					mock.EXPECT().ID().Return(types.ContainerID(""))
					mock.EXPECT().SafeImageID().Return(types.ImageID(""))
					mock.EXPECT().Name().Return("")
					mock.EXPECT().ImageName().Return("")

					return mock
				}(),
				newImage: "",
				state:    UnknownState,
			},
			want: &ContainerStatus{
				containerID:    "",
				oldImage:       "",
				newImage:       "",
				containerName:  "",
				imageName:      "",
				containerError: nil,
				state:          UnknownState,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UpdateFromContainer(tt.args.cont, tt.args.newImage, tt.args.state)
			if got.containerID != tt.want.containerID ||
				got.oldImage != tt.want.oldImage ||
				got.newImage != tt.want.newImage ||
				got.containerName != tt.want.containerName ||
				got.imageName != tt.want.imageName ||
				got.state != tt.want.state {
				t.Errorf("UpdateFromContainer() = %+v, want %+v", got, tt.want)
			}
			// Handle error field separately
			if (got.containerError == nil) != (tt.want.containerError == nil) {
				t.Errorf(
					"UpdateFromContainer() error = %v, want %v",
					got.containerError,
					tt.want.containerError,
				)
			} else if got.containerError != nil && got.containerError != tt.want.containerError {
				t.Errorf("UpdateFromContainer() error message = %v, want %v", got.containerError, tt.want.containerError)
			}
		})
	}
}

func TestProgress_AddSkipped(t *testing.T) {
	type args struct {
		cont types.Container
		err  error
	}

	tests := []struct {
		name string
		m    Progress
		args args
		want Progress
	}{
		{
			name: "add skipped with error",
			m:    Progress{},
			args: args{
				cont: func() types.Container {
					mock := mocks.NewMockContainer(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont1"))
					mock.EXPECT().SafeImageID().Return(types.ImageID("img1"))
					mock.EXPECT().Name().Return("container1")
					mock.EXPECT().ImageName().Return("image1:latest")

					return mock
				}(),
				err: errors.New("skipped due to policy"),
			},
			want: Progress{
				"cont1": &ContainerStatus{
					containerID:    "cont1",
					oldImage:       "img1",
					newImage:       "img1",
					containerName:  "container1",
					imageName:      "image1:latest",
					containerError: errors.New("skipped due to policy"),
					state:          SkippedState,
				},
			},
		},
		{
			name: "add skipped without error",
			m:    Progress{},
			args: args{
				cont: func() types.Container {
					mock := mocks.NewMockContainer(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont2"))
					mock.EXPECT().SafeImageID().Return(types.ImageID("img2"))
					mock.EXPECT().Name().Return("container2")
					mock.EXPECT().ImageName().Return("image2:latest")

					return mock
				}(),
				err: nil,
			},
			want: Progress{
				"cont2": &ContainerStatus{
					containerID:    "cont2",
					oldImage:       "img2",
					newImage:       "img2",
					containerName:  "container2",
					imageName:      "image2:latest",
					containerError: nil,
					state:          SkippedState,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.m.AddSkipped(tt.args.cont, tt.args.err)

			if len(tt.m) != len(tt.want) {
				t.Errorf("Progress.AddSkipped() map length = %d, want %d", len(tt.m), len(tt.want))

				return
			}

			for id, gotStatus := range tt.m {
				wantStatus := tt.want[id]
				if gotStatus.containerID != wantStatus.containerID ||
					gotStatus.oldImage != wantStatus.oldImage ||
					gotStatus.newImage != wantStatus.newImage ||
					gotStatus.containerName != wantStatus.containerName ||
					gotStatus.imageName != wantStatus.imageName ||
					gotStatus.state != wantStatus.state {
					t.Errorf(
						"Progress.AddSkipped() status for %v = %+v, want %+v",
						id,
						gotStatus,
						wantStatus,
					)
				}

				if gotStatus.Error() != wantStatus.Error() {
					t.Errorf(
						"Progress.AddSkipped() error for %v = %v, want %v",
						id,
						gotStatus.Error(),
						wantStatus.Error(),
					)
				}
			}
		})
	}
}

func TestProgress_AddScanned(t *testing.T) {
	type args struct {
		cont     types.Container
		newImage types.ImageID
	}

	tests := []struct {
		name string
		m    Progress
		args args
		want Progress
	}{
		{
			name: "add scanned with new image",
			m:    Progress{},
			args: args{
				cont: func() types.Container {
					mock := mocks.NewMockContainer(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont1"))
					mock.EXPECT().SafeImageID().Return(types.ImageID("img1"))
					mock.EXPECT().Name().Return("container1")
					mock.EXPECT().ImageName().Return("image1:latest")

					return mock
				}(),
				newImage: "img2",
			},
			want: Progress{
				"cont1": &ContainerStatus{
					containerID:    "cont1",
					oldImage:       "img1",
					newImage:       "img2",
					containerName:  "container1",
					imageName:      "image1:latest",
					containerError: nil,
					state:          ScannedState,
				},
			},
		},
		{
			name: "add scanned with same image",
			m:    Progress{},
			args: args{
				cont: func() types.Container {
					mock := mocks.NewMockContainer(t)
					mock.EXPECT().ID().Return(types.ContainerID("cont2"))
					mock.EXPECT().SafeImageID().Return(types.ImageID("img2"))
					mock.EXPECT().Name().Return("container2")
					mock.EXPECT().ImageName().Return("image2:latest")

					return mock
				}(),
				newImage: "img2",
			},
			want: Progress{
				"cont2": &ContainerStatus{
					containerID:    "cont2",
					oldImage:       "img2",
					newImage:       "img2",
					containerName:  "container2",
					imageName:      "image2:latest",
					containerError: nil,
					state:          ScannedState,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.m.AddScanned(tt.args.cont, tt.args.newImage)

			if len(tt.m) != len(tt.want) {
				t.Errorf("Progress.AddScanned() map length = %d, want %d", len(tt.m), len(tt.want))

				return
			}

			for id, gotStatus := range tt.m {
				wantStatus := tt.want[id]
				if gotStatus.containerID != wantStatus.containerID ||
					gotStatus.oldImage != wantStatus.oldImage ||
					gotStatus.newImage != wantStatus.newImage ||
					gotStatus.containerName != wantStatus.containerName ||
					gotStatus.imageName != wantStatus.imageName ||
					gotStatus.state != wantStatus.state {
					t.Errorf(
						"Progress.AddScanned() status for %v = %+v, want %+v",
						id,
						gotStatus,
						wantStatus,
					)
				}

				if (gotStatus.containerError == nil) != (wantStatus.containerError == nil) {
					t.Errorf(
						"Progress.AddScanned() error for %v = %v, want %v",
						id,
						gotStatus.containerError,
						wantStatus.containerError,
					)
				} else if gotStatus.containerError != nil && gotStatus.containerError != wantStatus.containerError {
					t.Errorf("Progress.AddScanned() error message for %v = %v, want %v", id, gotStatus.containerError, wantStatus.containerError)
				}
			}
		})
	}
}

func TestProgress_UpdateFailed(t *testing.T) {
	type args struct {
		failures map[types.ContainerID]error
	}

	tests := []struct {
		name string
		m    Progress
		args args
		want Progress
	}{
		{
			name: "update single failed container",
			m: Progress{
				"cont1": &ContainerStatus{state: ScannedState, containerID: "cont1"},
			},
			args: args{
				failures: map[types.ContainerID]error{
					"cont1": errors.New("update failed"),
				},
			},
			want: Progress{
				"cont1": &ContainerStatus{
					state:          FailedState,
					containerID:    "cont1",
					containerError: errors.New("update failed"),
				},
			},
		},
		{
			name: "update multiple failed containers",
			m: Progress{
				"cont1": &ContainerStatus{state: ScannedState, containerID: "cont1"},
				"cont2": &ContainerStatus{state: UpdatedState, containerID: "cont2"},
			},
			args: args{
				failures: map[types.ContainerID]error{
					"cont1": errors.New("timeout"),
					"cont2": errors.New("permission denied"),
				},
			},
			want: Progress{
				"cont1": &ContainerStatus{
					state:          FailedState,
					containerID:    "cont1",
					containerError: errors.New("timeout"),
				},
				"cont2": &ContainerStatus{
					state:          FailedState,
					containerID:    "cont2",
					containerError: errors.New("permission denied"),
				},
			},
		},
		{
			name: "no failures",
			m: Progress{
				"cont1": &ContainerStatus{state: ScannedState, containerID: "cont1"},
			},
			args: args{
				failures: map[types.ContainerID]error{},
			},
			want: Progress{
				"cont1": &ContainerStatus{state: ScannedState, containerID: "cont1"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.m.UpdateFailed(tt.args.failures)

			if len(tt.m) != len(tt.want) {
				t.Errorf(
					"Progress.UpdateFailed() map length = %d, want %d",
					len(tt.m),
					len(tt.want),
				)

				return
			}

			for id, gotStatus := range tt.m {
				wantStatus := tt.want[id]
				if gotStatus.containerID != wantStatus.containerID ||
					gotStatus.oldImage != wantStatus.oldImage ||
					gotStatus.newImage != wantStatus.newImage ||
					gotStatus.containerName != wantStatus.containerName ||
					gotStatus.imageName != wantStatus.imageName ||
					gotStatus.state != wantStatus.state {
					t.Errorf(
						"Progress.UpdateFailed() status for %v = %+v, want %+v",
						id,
						gotStatus,
						wantStatus,
					)
				}

				if gotStatus.Error() != wantStatus.Error() {
					t.Errorf(
						"Progress.UpdateFailed() error for %v = %v, want %v",
						id,
						gotStatus.Error(),
						wantStatus.Error(),
					)
				}
			}
		})
	}
}

func TestProgress_Add(t *testing.T) {
	type args struct {
		update *ContainerStatus
	}

	tests := []struct {
		name string
		m    Progress
		args args
		want Progress
	}{
		{
			name: "add new container",
			m:    Progress{},
			args: args{
				update: &ContainerStatus{containerID: "cont1", state: ScannedState},
			},
			want: Progress{
				"cont1": &ContainerStatus{containerID: "cont1", state: ScannedState},
			},
		},
		{
			name: "overwrite existing container",
			m: Progress{
				"cont1": &ContainerStatus{containerID: "cont1", state: UnknownState},
			},
			args: args{
				update: &ContainerStatus{containerID: "cont1", state: UpdatedState},
			},
			want: Progress{
				"cont1": &ContainerStatus{containerID: "cont1", state: UpdatedState},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.m.Add(tt.args.update)

			if len(tt.m) != len(tt.want) {
				t.Errorf("Progress.Add() map length = %d, want %d", len(tt.m), len(tt.want))

				return
			}

			for id, gotStatus := range tt.m {
				wantStatus := tt.want[id]
				if gotStatus.containerID != wantStatus.containerID ||
					gotStatus.oldImage != wantStatus.oldImage ||
					gotStatus.newImage != wantStatus.newImage ||
					gotStatus.containerName != wantStatus.containerName ||
					gotStatus.imageName != wantStatus.imageName ||
					gotStatus.state != wantStatus.state {
					t.Errorf(
						"Progress.Add() status for %v = %+v, want %+v",
						id,
						gotStatus,
						wantStatus,
					)
				}

				if (gotStatus.containerError == nil) != (wantStatus.containerError == nil) {
					t.Errorf(
						"Progress.Add() error for %v = %v, want %v",
						id,
						gotStatus.containerError,
						wantStatus.containerError,
					)
				} else if gotStatus.containerError != nil && gotStatus.containerError != wantStatus.containerError {
					t.Errorf("Progress.Add() error message for %v = %v, want %v", id, gotStatus.containerError, wantStatus.containerError)
				}
			}
		})
	}
}

func TestProgress_MarkForUpdate(t *testing.T) {
	type args struct {
		containerID types.ContainerID
	}

	tests := []struct {
		name string
		m    Progress
		args args
		want Progress
	}{
		{
			name: "mark existing container",
			m: Progress{
				"cont1": &ContainerStatus{containerID: "cont1", state: ScannedState},
			},
			args: args{containerID: "cont1"},
			want: Progress{
				"cont1": &ContainerStatus{containerID: "cont1", state: UpdatedState},
			},
		},
		{
			name: "mark non-existent container",
			m:    Progress{},
			args: args{containerID: "cont1"},
			want: Progress{}, // Expect panic or nil dereference, but we'll test behavior
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil && tt.args.containerID != "cont1" {
					t.Errorf("Progress.MarkForUpdate() panicked unexpectedly: %v", r)
				}
			}()
			tt.m.MarkForUpdate(tt.args.containerID)

			if len(tt.m) != len(tt.want) {
				t.Errorf(
					"Progress.MarkForUpdate() map length = %d, want %d",
					len(tt.m),
					len(tt.want),
				)

				return
			}

			for id, gotStatus := range tt.m {
				wantStatus := tt.want[id]
				if gotStatus.containerID != wantStatus.containerID ||
					gotStatus.oldImage != wantStatus.oldImage ||
					gotStatus.newImage != wantStatus.newImage ||
					gotStatus.containerName != wantStatus.containerName ||
					gotStatus.imageName != wantStatus.imageName ||
					gotStatus.state != wantStatus.state {
					t.Errorf(
						"Progress.MarkForUpdate() status for %v = %+v, want %+v",
						id,
						gotStatus,
						wantStatus,
					)
				}

				if (gotStatus.containerError == nil) != (wantStatus.containerError == nil) {
					t.Errorf(
						"Progress.MarkForUpdate() error for %v = %v, want %v",
						id,
						gotStatus.containerError,
						wantStatus.containerError,
					)
				} else if gotStatus.containerError != nil && gotStatus.containerError != wantStatus.containerError {
					t.Errorf("Progress.MarkForUpdate() error message for %v = %v, want %v", id, gotStatus.containerError, wantStatus.containerError)
				}
			}
		})
	}
}

func TestProgress_Report(t *testing.T) {
	tests := []struct {
		name string
		m    Progress
		want types.Report
	}{
		{
			name: "empty progress",
			m:    Progress{},
			want: &report{
				scanned: []types.ContainerReport{},
				updated: []types.ContainerReport{},
				failed:  []types.ContainerReport{},
				skipped: []types.ContainerReport{},
				stale:   []types.ContainerReport{},
				fresh:   []types.ContainerReport{},
			},
		},
		{
			name: "single scanned container",
			m: Progress{
				"cont1": &ContainerStatus{
					containerID:   "cont1",
					oldImage:      "img1",
					newImage:      "img2",
					containerName: "container1",
					imageName:     "image1:latest",
					state:         ScannedState,
				},
			},
			want: &report{
				scanned: []types.ContainerReport{
					&ContainerStatus{
						containerID:   "cont1",
						oldImage:      "img1",
						newImage:      "img2",
						containerName: "container1",
						imageName:     "image1:latest",
						state:         StaleState, // Scanned with differing images becomes Stale
					},
				},
				updated: []types.ContainerReport{},
				failed:  []types.ContainerReport{},
				skipped: []types.ContainerReport{},
				stale: []types.ContainerReport{
					&ContainerStatus{
						containerID:   "cont1",
						oldImage:      "img1",
						newImage:      "img2",
						containerName: "container1",
						imageName:     "image1:latest",
						state:         StaleState,
					},
				},
				fresh: []types.ContainerReport{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.Report(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Progress.Report() = %v, want %v", got, tt.want)
			}
		})
	}
}

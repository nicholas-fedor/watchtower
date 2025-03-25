package session

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestContainerStatus_ID(t *testing.T) {
	tests := []struct {
		name string
		u    *ContainerStatus
		want types.ContainerID
	}{
		{
			name: "valid container ID",
			u:    &ContainerStatus{containerID: "cont1"},
			want: "cont1",
		},
		{
			name: "empty container ID",
			u:    &ContainerStatus{containerID: ""},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.u.ID(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ContainerStatus.ID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainerStatus_Name(t *testing.T) {
	tests := []struct {
		name string
		u    *ContainerStatus
		want string
	}{
		{
			name: "valid container name",
			u:    &ContainerStatus{containerName: "my-container"},
			want: "my-container",
		},
		{
			name: "empty container name",
			u:    &ContainerStatus{containerName: ""},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.u.Name(); got != tt.want {
				t.Errorf("ContainerStatus.Name() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainerStatus_CurrentImageID(t *testing.T) {
	tests := []struct {
		name string
		u    *ContainerStatus
		want types.ImageID
	}{
		{
			name: "valid current image ID",
			u:    &ContainerStatus{oldImage: "img123"},
			want: "img123",
		},
		{
			name: "empty current image ID",
			u:    &ContainerStatus{oldImage: ""},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.u.CurrentImageID(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ContainerStatus.CurrentImageID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainerStatus_LatestImageID(t *testing.T) {
	tests := []struct {
		name string
		u    *ContainerStatus
		want types.ImageID
	}{
		{
			name: "valid latest image ID",
			u:    &ContainerStatus{newImage: "img456"},
			want: "img456",
		},
		{
			name: "empty latest image ID",
			u:    &ContainerStatus{newImage: ""},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.u.LatestImageID(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ContainerStatus.LatestImageID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainerStatus_ImageName(t *testing.T) {
	tests := []struct {
		name string
		u    *ContainerStatus
		want string
	}{
		{
			name: "valid image name",
			u:    &ContainerStatus{imageName: "myimage:latest"},
			want: "myimage:latest",
		},
		{
			name: "empty image name",
			u:    &ContainerStatus{imageName: ""},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.u.ImageName(); got != tt.want {
				t.Errorf("ContainerStatus.ImageName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainerStatus_Error(t *testing.T) {
	tests := []struct {
		name string
		u    *ContainerStatus
		want string
	}{
		{
			name: "no error",
			u:    &ContainerStatus{containerError: nil},
			want: "",
		},
		{
			name: "with error",
			u:    &ContainerStatus{containerError: errors.New("update failed")},
			want: "update failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.u.Error(); got != tt.want {
				t.Errorf("ContainerStatus.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainerStatus_State(t *testing.T) {
	tests := []struct {
		name string
		u    *ContainerStatus
		want string
	}{
		{
			name: "unknown state",
			u:    &ContainerStatus{state: UnknownState},
			want: "Unknown",
		},
		{
			name: "skipped state",
			u:    &ContainerStatus{state: SkippedState},
			want: "Skipped",
		},
		{
			name: "scanned state",
			u:    &ContainerStatus{state: ScannedState},
			want: "Scanned",
		},
		{
			name: "updated state",
			u:    &ContainerStatus{state: UpdatedState},
			want: "Updated",
		},
		{
			name: "failed state",
			u:    &ContainerStatus{state: FailedState},
			want: "Failed",
		},
		{
			name: "fresh state",
			u:    &ContainerStatus{state: FreshState},
			want: "Fresh",
		},
		{
			name: "stale state",
			u:    &ContainerStatus{state: StaleState},
			want: "Stale",
		},
		{
			name: "invalid state",
			u:    &ContainerStatus{state: State(999)}, // Beyond defined states
			want: "Unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.u.State(); got != tt.want {
				t.Errorf("ContainerStatus.State() = %v, want %v", got, tt.want)
			}
		})
	}
}

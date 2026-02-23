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
			name: "restarted state",
			u:    &ContainerStatus{state: RestartedState},
			want: RestartedStateString,
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

func TestContainerStatus_RestartedStateBehavior(t *testing.T) {
	tests := []struct {
		name string
		u    *ContainerStatus
	}{
		{
			name: "restarted state with all fields",
			u: &ContainerStatus{
				containerID:    "cont1",
				oldImage:       "img123",
				newImage:       "img456",
				containerName:  "my-container",
				imageName:      "nginx:latest",
				containerError: nil,
				state:          RestartedState,
				monitorOnly:    false,
				newContainerID: "newcont1",
			},
		},
		{
			name: "restarted state with minimal fields",
			u: &ContainerStatus{
				containerID: "cont1",
				state:       RestartedState,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify state is Restarted
			if got := tt.u.State(); got != RestartedStateString {
				t.Errorf("ContainerStatus.State() = %v, want %v", got, RestartedStateString)
			}
			// Verify other methods return expected values
			if got := tt.u.ID(); got != tt.u.containerID {
				t.Errorf("ContainerStatus.ID() = %v, want %v", got, tt.u.containerID)
			}

			if got := tt.u.Name(); got != tt.u.containerName {
				t.Errorf("ContainerStatus.Name() = %v, want %v", got, tt.u.containerName)
			}

			if got := tt.u.CurrentImageID(); got != tt.u.oldImage {
				t.Errorf("ContainerStatus.CurrentImageID() = %v, want %v", got, tt.u.oldImage)
			}

			if got := tt.u.LatestImageID(); got != tt.u.newImage {
				t.Errorf("ContainerStatus.LatestImageID() = %v, want %v", got, tt.u.newImage)
			}

			if got := tt.u.ImageName(); got != tt.u.imageName {
				t.Errorf("ContainerStatus.ImageName() = %v, want %v", got, tt.u.imageName)
			}

			if got := tt.u.IsMonitorOnly(); got != tt.u.monitorOnly {
				t.Errorf("ContainerStatus.IsMonitorOnly() = %v, want %v", got, tt.u.monitorOnly)
			}

			if got := tt.u.NewContainerID(); got != tt.u.newContainerID {
				t.Errorf("ContainerStatus.NewContainerID() = %v, want %v", got, tt.u.newContainerID)
			}
		})
	}
}

func TestContainerStatus_RestartedStateErrorHandling(t *testing.T) {
	tests := []struct {
		name string
		u    *ContainerStatus
		want string
	}{
		{
			name: "restarted state with no error",
			u: &ContainerStatus{
				state:          RestartedState,
				containerError: nil,
			},
			want: "",
		},
		{
			name: "restarted state with error",
			u: &ContainerStatus{
				state:          RestartedState,
				containerError: errors.New("restart failed"),
			},
			want: "restart failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.u.Error(); got != tt.want {
				t.Errorf("ContainerStatus.Error() = %v, want %v", got, tt.want)
			}
			// Ensure state is still Restarted
			if got := tt.u.State(); got != RestartedStateString {
				t.Errorf("ContainerStatus.State() = %v, want %v", got, RestartedStateString)
			}
		})
	}
}

func TestContainerStatus_RestartedStateWithMissingData(t *testing.T) {
	tests := []struct {
		name string
		u    *ContainerStatus
	}{
		{
			name: "restarted state with empty container ID",
			u: &ContainerStatus{
				containerID: "",
				state:       RestartedState,
			},
		},
		{
			name: "restarted state with empty images",
			u: &ContainerStatus{
				containerID: "cont1",
				oldImage:    "",
				newImage:    "",
				state:       RestartedState,
			},
		},
		{
			name: "restarted state with empty name and image name",
			u: &ContainerStatus{
				containerID:   "cont1",
				containerName: "",
				imageName:     "",
				state:         RestartedState,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify state is Restarted
			if got := tt.u.State(); got != RestartedStateString {
				t.Errorf("ContainerStatus.State() = %v, want %v", got, RestartedStateString)
			}
			// Verify methods return empty values appropriately
			if tt.u.ID() != tt.u.containerID {
				t.Errorf("ID() mismatch")
			}

			if tt.u.Name() != tt.u.containerName {
				t.Errorf("Name() mismatch")
			}

			if tt.u.CurrentImageID() != tt.u.oldImage {
				t.Errorf("CurrentImageID() mismatch")
			}

			if tt.u.LatestImageID() != tt.u.newImage {
				t.Errorf("LatestImageID() mismatch")
			}

			if tt.u.ImageName() != tt.u.imageName {
				t.Errorf("ImageName() mismatch")
			}
		})
	}
}

func TestContainerStatus_RestartedStateIntegration(t *testing.T) {
	tests := []struct {
		name string
		u    *ContainerStatus
	}{
		{
			name: "restarted state with monitor only",
			u: &ContainerStatus{
				containerID:   "cont1",
				containerName: "my-container",
				state:         RestartedState,
				monitorOnly:   true,
			},
		},
		{
			name: "restarted state with new container ID",
			u: &ContainerStatus{
				containerID:    "cont1",
				newContainerID: "newcont1",
				state:          RestartedState,
			},
		},
		{
			name: "restarted state with all fields and error",
			u: &ContainerStatus{
				containerID:    "cont1",
				oldImage:       "img123",
				newImage:       "img456",
				containerName:  "my-container",
				imageName:      "nginx:latest",
				containerError: errors.New("some error"),
				state:          RestartedState,
				monitorOnly:    true,
				newContainerID: "newcont1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify state is Restarted
			if got := tt.u.State(); got != RestartedStateString {
				t.Errorf("ContainerStatus.State() = %v, want %v", got, RestartedStateString)
			}
			// Verify integration of fields
			if tt.u.ID() != tt.u.containerID {
				t.Errorf("ID integration failed")
			}

			if tt.u.IsMonitorOnly() != tt.u.monitorOnly {
				t.Errorf("IsMonitorOnly integration failed")
			}

			if tt.u.NewContainerID() != tt.u.newContainerID {
				t.Errorf("NewContainerID integration failed")
			}
			// Test SetNewContainerID
			tt.u.SetNewContainerID("updatedID")

			if tt.u.NewContainerID() != "updatedID" {
				t.Errorf("SetNewContainerID failed")
			}
		})
	}
}

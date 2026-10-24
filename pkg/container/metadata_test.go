package container

import (
	"bytes"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestContainer_GetLifecyclePreCheckCommand(t *testing.T) {
	tests := []struct {
		name string
		c    *Container
		want string
	}{
		{
			name: "PreCheckLabelSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecyclePreCheck: "echo pre-check",
						},
					},
				},
			},
			want: "echo pre-check",
		},
		{
			name: "PreCheckLabelNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.GetLifecyclePreCheckCommand()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainer_GetLifecyclePostCheckCommand(t *testing.T) {
	tests := []struct {
		name string
		c    *Container
		want string
	}{
		{
			name: "PostCheckLabelSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecyclePostCheck: "echo post-check",
						},
					},
				},
			},
			want: "echo post-check",
		},
		{
			name: "PostCheckLabelNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.GetLifecyclePostCheckCommand()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainer_GetLifecyclePreUpdateCommand(t *testing.T) {
	tests := []struct {
		name string
		c    *Container
		want string
	}{
		{
			name: "PreUpdateLabelSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecyclePreUpdate: "echo pre-update",
						},
					},
				},
			},
			want: "echo pre-update",
		},
		{
			name: "PreUpdateLabelNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.GetLifecyclePreUpdateCommand()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainer_GetLifecyclePostUpdateCommand(t *testing.T) {
	tests := []struct {
		name string
		c    *Container
		want string
	}{
		{
			name: "PostUpdateLabelSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecyclePostUpdate: "echo post-update",
						},
					},
				},
			},
			want: "echo post-update",
		},
		{
			name: "PostUpdateLabelNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.GetLifecyclePostUpdateCommand()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainer_PreUpdateTimeout(t *testing.T) {
	tests := []struct {
		name string
		c    *Container
		want int
	}{
		{
			name: "ValidTimeout",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecyclePreUpdateTimeout: "5",
						},
					},
				},
			},
			want: 5,
		},
		{
			name: "InvalidTimeout",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecyclePreUpdateTimeout: "invalid",
						},
					},
				},
			},
			want: 1,
		},
		{
			name: "TimeoutNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.PreUpdateTimeout()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainer_PostUpdateTimeout(t *testing.T) {
	tests := []struct {
		name string
		c    *Container
		want int
	}{
		{
			name: "ValidTimeout",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecyclePostUpdateTimeout: "10",
						},
					},
				},
			},
			want: 10,
		},
		{
			name: "InvalidTimeout",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecyclePostUpdateTimeout: "invalid",
						},
					},
				},
			},
			want: 1,
		},
		{
			name: "TimeoutNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.PostUpdateTimeout()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainer_Enabled(t *testing.T) {
	tests := []struct {
		name  string
		c     *Container
		want  bool
		want1 bool
	}{
		{
			name: "EnabledTrue",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.Enable: "true",
						},
					},
				},
			},
			want:  true,
			want1: true,
		},
		{
			name: "EnabledFalse",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.Enable: "false",
						},
					},
				},
			},
			want:  false,
			want1: true,
		},
		{
			name: "LabelNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want:  false,
			want1: false,
		},
		{
			name: "InvalidValue",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.Enable: "invalid",
						},
					},
				},
			},
			want:  false,
			want1: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.c.Enabled()
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want1, got1)
		})
	}
}

func TestContainer_IsMonitorOnly(t *testing.T) {
	type args struct {
		params types.UpdateParams
	}

	tests := []struct {
		name string
		c    *Container
		args args
		want bool
	}{
		{
			name: "LabelTruePrecedence",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.MonitorOnly: "true",
						},
					},
				},
			},
			args: args{
				params: types.UpdateParams{
					MonitorOnly:     false,
					LabelPrecedence: true,
				},
			},
			want: true,
		},
		{
			name: "GlobalTrueNoPrecedence",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.MonitorOnly: "false",
						},
					},
				},
			},
			args: args{
				params: types.UpdateParams{
					MonitorOnly:     true,
					LabelPrecedence: false,
				},
			},
			want: true,
		},
		{
			name: "LabelNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			args: args{
				params: types.UpdateParams{
					MonitorOnly: false,
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.IsMonitorOnly(tt.args.params)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainer_IsNoPull(t *testing.T) {
	type args struct {
		params types.UpdateParams
	}

	tests := []struct {
		name string
		c    *Container
		args args
		want bool
	}{
		{
			name: "LabelTruePrecedence",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.NoPull: "true",
						},
					},
				},
			},
			args: args{
				params: types.UpdateParams{
					NoPull:          false,
					LabelPrecedence: true,
				},
			},
			want: true,
		},
		{
			name: "GlobalTrueNoPrecedence",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.NoPull: "false",
						},
					},
				},
			},
			args: args{
				params: types.UpdateParams{
					NoPull:          true,
					LabelPrecedence: false,
				},
			},
			want: true,
		},
		{
			name: "LabelNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			args: args{
				params: types.UpdateParams{
					NoPull: false,
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.IsNoPull(tt.args.params)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainer_Scope(t *testing.T) {
	tests := []struct {
		name  string
		c     *Container
		want  string
		want1 bool
	}{
		{
			name: "ScopeSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.Scope: "test-scope",
						},
					},
				},
			},
			want:  "test-scope",
			want1: true,
		},
		{
			name: "ScopeNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want:  "",
			want1: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.c.Scope()
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want1, got1)
		})
	}
}

func TestContainer_IsWatchtower(t *testing.T) {
	tests := []struct {
		name string
		c    *Container
		want bool
	}{
		{
			name: "IsWatchtowerTrue",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.Watchtower: "true",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "IsWatchtowerFalse",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.Watchtower: "false",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "LabelNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want: false,
		},
		{
			name: "NoContainerInfo",
			c:    &Container{},
			want: false,
		},
		{
			name: "NoConfig",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{Name: "/test-container"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.IsWatchtower()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainer_StopSignal(t *testing.T) {
	tests := []struct {
		name string
		c    *Container
		want string
	}{
		{
			name: "SignalSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.StopSignal: "SIGTERM",
						},
					},
				},
			},
			want: "SIGTERM",
		},
		{
			name: "SignalNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want: "SIGTERM",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.StopSignal()
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestContainer_StopTimeout_WhenSet verifies StopTimeout returns the configured value.
func TestContainer_StopTimeout_WhenSet(t *testing.T) {
	timeout60 := 60
	c := &Container{
		containerInfo: &dockerContainer.InspectResponse{
			Name: "/test-container",
			Config: &dockerContainer.Config{
				StopTimeout: &timeout60,
				Labels:      map[string]string{},
			},
		},
	}

	got := c.StopTimeout()
	assert.NotNil(t, got)
	assert.Equal(t, timeout60, *got)
}

// TestContainer_StopTimeout_WhenSetToZero verifies StopTimeout returns zero when configured as zero.
func TestContainer_StopTimeout_WhenSetToZero(t *testing.T) {
	timeout0 := 0
	c := &Container{
		containerInfo: &dockerContainer.InspectResponse{
			Name: "/test-container",
			Config: &dockerContainer.Config{
				StopTimeout: &timeout0,
				Labels:      map[string]string{},
			},
		},
	}

	got := c.StopTimeout()
	assert.NotNil(t, got)
	assert.Equal(t, timeout0, *got)
}

// TestContainer_StopTimeout_WhenNotSet verifies StopTimeout returns nil when not configured.
func TestContainer_StopTimeout_WhenNotSet(t *testing.T) {
	c := &Container{
		containerInfo: &dockerContainer.InspectResponse{
			Name:   "/test-container",
			Config: &dockerContainer.Config{Labels: map[string]string{}},
		},
	}

	got := c.StopTimeout()
	assert.Nil(t, got)
}

// TestContainer_StopTimeout_NilContainerInfo verifies StopTimeout returns nil for nil containerInfo.
func TestContainer_StopTimeout_NilContainerInfo(t *testing.T) {
	c := &Container{containerInfo: nil}

	got := c.StopTimeout()
	assert.Nil(t, got)
}

// TestContainer_StopTimeout_NilConfig verifies StopTimeout returns nil for nil Config.
func TestContainer_StopTimeout_NilConfig(t *testing.T) {
	c := &Container{
		containerInfo: &dockerContainer.InspectResponse{
			Name:   "/test-container",
			Config: nil,
		},
	}

	got := c.StopTimeout()
	assert.Nil(t, got)
}

func TestContainsWatchtowerLabel(t *testing.T) {
	type args struct {
		labels map[string]string
	}

	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "WatchtowerTrue",
			args: args{
				labels: map[string]string{
					labels.Watchtower: "true",
				},
			},
			want: true,
		},
		{
			name: "WatchtowerFalse",
			args: args{
				labels: map[string]string{
					labels.Watchtower: "false",
				},
			},
			want: false,
		},
		{
			name: "LabelNotSet",
			args: args{
				labels: map[string]string{},
			},
			want: false,
		},
		{
			name: "LabelsNil",
			args: args{
				labels: nil,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsWatchtowerLabel(tt.args.labels)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainer_getLabelValueOrEmpty(t *testing.T) {
	type args struct {
		label string
	}

	tests := []struct {
		name string
		c    *Container
		args args
		want string
	}{
		{
			name: "LabelSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"test.label": "value",
						},
					},
				},
			},
			args: args{label: "test.label"},
			want: "value",
		},
		{
			name: "LabelNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			args: args{label: "test.label"},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.getLabelValueOrEmpty(tt.args.label)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainer_getLabelValue(t *testing.T) {
	type args struct {
		label string
	}

	tests := []struct {
		name  string
		c     *Container
		args  args
		want  string
		want1 bool
	}{
		{
			name: "LabelSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"test.label": "value",
						},
					},
				},
			},
			args:  args{label: "test.label"},
			want:  "value",
			want1: true,
		},
		{
			name: "LabelNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			args:  args{label: "test.label"},
			want:  "",
			want1: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.c.getLabelValue(tt.args.label)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want1, got1)
		})
	}
}

func TestContainer_getBoolLabelValue(t *testing.T) {
	type args struct {
		label string
	}

	tests := []struct {
		name    string
		c       *Container
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "TrueValue",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"test.label": "true",
						},
					},
				},
			},
			args:    args{label: "test.label"},
			want:    true,
			wantErr: false,
		},
		{
			name: "FalseValue",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"test.label": "false",
						},
					},
				},
			},
			args:    args{label: "test.label"},
			want:    false,
			wantErr: false,
		},
		{
			name: "InvalidValue",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"test.label": "invalid",
						},
					},
				},
			},
			args:    args{label: "test.label"},
			want:    false,
			wantErr: true,
		},
		{
			name: "LabelNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			args:    args{label: "test.label"},
			want:    false,
			wantErr: true,
		},
		{
			name: "EmptyStringValue",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"test.label": "",
						},
					},
				},
			},
			args:    args{label: "test.label"},
			want:    false,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.c.getBoolLabelValue(tt.args.label)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainer_GetLifecycleUID(t *testing.T) {
	tests := []struct {
		name  string
		c     *Container
		want  int
		want1 bool
	}{
		{
			name: "UIDSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecycleUID: "1000",
						},
					},
				},
			},
			want:  1000,
			want1: true,
		},
		{
			name: "UIDNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want:  0,
			want1: false,
		},
		{
			name: "InvalidUID",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecycleUID: "invalid",
						},
					},
				},
			},
			want:  0,
			want1: false,
		},
		{
			name: "NegativeUID",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecycleUID: "-1",
						},
					},
				},
			},
			want:  0,
			want1: false,
		},
		{
			name: "TooLargeUID",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecycleUID: "3000000000",
						},
					},
				},
			},
			want:  0,
			want1: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.c.GetLifecycleUID()
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want1, got1)
		})
	}
}

func TestContainer_GetLifecycleGID(t *testing.T) {
	tests := []struct {
		name  string
		c     *Container
		want  int
		want1 bool
	}{
		{
			name: "GIDSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecycleGID: "1000",
						},
					},
				},
			},
			want:  1000,
			want1: true,
		},
		{
			name: "GIDNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want:  0,
			want1: false,
		},
		{
			name: "InvalidGID",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecycleGID: "invalid",
						},
					},
				},
			},
			want:  0,
			want1: false,
		},
		{
			name: "NegativeGID",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecycleGID: "-100",
						},
					},
				},
			},
			want:  0,
			want1: false,
		},
		{
			name: "TooLargeGID",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.LifecycleGID: "5000000000",
						},
					},
				},
			},
			want:  0,
			want1: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.c.GetLifecycleGID()
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want1, got1)
		})
	}
}

func TestContainer_getContainerOrGlobalBool(t *testing.T) {
	type args struct {
		globalVal      bool
		label          string
		contPrecedence bool
	}

	tests := []struct {
		name string
		c    *Container
		args args
		want bool
	}{
		{
			name: "LabelTruePrecedence",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"test.label": "true",
						},
					},
				},
			},
			args: args{
				globalVal:      false,
				label:          "test.label",
				contPrecedence: true,
			},
			want: true,
		},
		{
			name: "LabelFalseNoPrecedence",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"test.label": "false",
						},
					},
				},
			},
			args: args{
				globalVal:      true,
				label:          "test.label",
				contPrecedence: false,
			},
			want: true,
		},
		{
			name: "LabelNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			args: args{
				globalVal: true,
				label:     "test.label",
			},
			want: true,
		},
		{
			name: "InvalidLabel",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"test.label": "invalid",
						},
					},
				},
			},
			args: args{
				globalVal: false,
				label:     "test.label",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.getContainerOrGlobalBool(
				tt.args.globalVal,
				tt.args.label,
				tt.args.contPrecedence,
			)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainer_GetContainerChain(t *testing.T) {
	tests := []struct {
		name  string
		c     *Container
		want  string
		want1 bool
	}{
		{
			name: "ContainerChainSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.ContainerChain: "chain-value",
						},
					},
				},
			},
			want:  "chain-value",
			want1: true,
		},
		{
			name: "ContainerChainNotSet",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want:  "",
			want1: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.c.GetContainerChain()
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want1, got1)
		})
	}
}

func TestGetEffectiveScope(t *testing.T) {
	tests := []struct {
		name          string
		initialScope  string
		container     *Container
		expectedScope string
		expectedError bool
	}{
		{
			name:          "scope already set - should return initial scope",
			initialScope:  "preset",
			container:     nil,
			expectedScope: "preset",
			expectedError: false,
		},
		{
			name:         "container has no scope label - should return initial scope",
			initialScope: "",
			container: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			expectedScope: "",
			expectedError: false,
		},
		{
			name:         "container has empty scope label - should return initial scope",
			initialScope: "",
			container: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.Scope: "",
						},
					},
				},
			},
			expectedScope: "",
			expectedError: false,
		},
		{
			name:         "container has valid scope label - should return derived scope",
			initialScope: "",
			container: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.Scope: "production",
						},
					},
				},
			},
			expectedScope: "production",
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute function under test
			result, err := GetEffectiveScope(tt.container, tt.initialScope)

			// Assert results
			if tt.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expectedScope, result)
		})
	}
}

func TestGetEffectiveScope_NilContainer(t *testing.T) {
	_, err := GetEffectiveScope(nil, "")
	require.Error(t, err)
	assert.ErrorIs(t, err, errCurrentContainerNotCached)
}

// cooldownTestContainer creates a test Container with the given cooldown delay label value.
// Pass labelValue as nil to create a container with no cooldown delay label (empty labels map).
// Pass a pointer to a string to set the cooldown delay label to that value.
//
// Parameters:
//   - labelValue: Optional label value. nil omits the label entirely.
//
// Returns:
//   - *Container: A test container with the specified cooldown label configuration.
func cooldownTestContainer(labelValue *string) *Container {
	containerLabels := map[string]string{}

	if labelValue != nil {
		containerLabels[labels.CooldownDelay] = *labelValue
	}

	return &Container{
		containerInfo: &dockerContainer.InspectResponse{
			Name: "/test-container",
			Config: &dockerContainer.Config{
				Labels: containerLabels,
			},
		},
	}
}

func TestContainer_CooldownDelay(t *testing.T) {
	type args struct {
		params types.UpdateParams
	}

	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name string
		c    *Container
		args args
		want time.Duration
	}{
		{
			name: "NoLabelUsesGlobal",
			c:    cooldownTestContainer(nil),
			args: args{
				params: types.UpdateParams{
					CooldownDelay: 10 * time.Minute,
				},
			},
			want: 10 * time.Minute,
		},
		{
			name: "LabelOverridesGlobal",
			c:    cooldownTestContainer(strPtr("24h")),
			args: args{
				params: types.UpdateParams{
					CooldownDelay: 10 * time.Minute,
				},
			},
			want: 24 * time.Hour,
		},
		{
			name: "LabelZeroDisables",
			c:    cooldownTestContainer(strPtr("0")),
			args: args{
				params: types.UpdateParams{
					CooldownDelay: 10 * time.Minute,
				},
			},
			want: 0,
		},
		{
			name: "ExtendedUnitsDays",
			c:    cooldownTestContainer(strPtr("3d")),
			args: args{
				params: types.UpdateParams{
					CooldownDelay: 10 * time.Minute,
				},
			},
			want: 72 * time.Hour,
		},
		{
			name: "InvalidLabelFallsBackToGlobal",
			c:    cooldownTestContainer(strPtr("invalid")),
			args: args{
				params: types.UpdateParams{
					CooldownDelay: 10 * time.Minute,
				},
			},
			want: 10 * time.Minute,
		},
		{
			name: "EmptyLabelFallsBackToGlobal",
			c:    cooldownTestContainer(strPtr("")),
			args: args{
				params: types.UpdateParams{
					CooldownDelay: 10 * time.Minute,
				},
			},
			want: 10 * time.Minute,
		},
		{
			name: "NegativeLabelFallsBackToGlobal",
			c:    cooldownTestContainer(strPtr("-1h")),
			args: args{
				params: types.UpdateParams{
					CooldownDelay: 10 * time.Minute,
				},
			},
			want: 10 * time.Minute,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.CooldownDelay(tt.args.params)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetEffectiveScope_NoneScopeEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		initialScope  string
		container     *Container
		expectedScope string
		expectedError bool
		description   string
	}{
		{
			name:         "explicit_none_scope_inheritance",
			initialScope: "",
			container: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.Scope: "none",
						},
					},
				},
			},
			expectedScope: "none",
			expectedError: false,
			description:   "container with explicit 'none' scope should inherit it when initial scope is empty",
		},
		{
			name:         "explicit_none_scope_precedence",
			initialScope: "preset",
			container: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.Scope: "none",
						},
					},
				},
			},
			expectedScope: "preset",
			expectedError: false,
			description:   "explicit initial scope should take precedence over container's 'none' scope",
		},
		{
			name:         "implicit_unscoped_defaults_to_empty",
			initialScope: "",
			container: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			expectedScope: "",
			expectedError: false,
			description:   "truly unscoped container should return empty scope when no initial scope set",
		},
		{
			name:         "explicit_empty_scope_vs_none_scope",
			initialScope: "",
			container: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.Scope: "", // explicitly empty
						},
					},
				},
			},
			expectedScope: "",
			expectedError: false,
			description:   "explicitly empty scope label should not be inherited, returning initial empty scope",
		},
		{
			name:         "transition_from_scoped_to_none",
			initialScope: "",
			container: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.Scope: "none", // transitioning to none
						},
					},
				},
			},
			expectedScope: "none",
			expectedError: false,
			description:   "container transitioning from scoped to 'none' should inherit the 'none' scope",
		},
		{
			name:         "none_scope_with_initial_scope_set",
			initialScope: "production",
			container: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name: "/test-container",
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							labels.Scope: "none",
						},
					},
				},
			},
			expectedScope: "production",
			expectedError: false,
			description:   "initial scope takes precedence even when container has 'none' scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetEffectiveScope(tt.container, tt.initialScope)

			if tt.expectedError {
				require.Error(t, err, tt.description)
			} else {
				require.NoError(t, err, tt.description)
			}

			assert.Equal(t, tt.expectedScope, result, tt.description)
		})
	}
}

// TestContainer_IsWatchtower_DoesNotLog verifies that checking whether a
// container is Watchtower writes no log entries, even at debug level, since
// a scan checks every container many times.
func TestContainer_IsWatchtower_DoesNotLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{name: "label true", labels: map[string]string{labels.Watchtower: "true"}, want: true},
		{name: "label false", labels: map[string]string{labels.Watchtower: "false"}},
		{name: "label missing", labels: map[string]string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			log := zerolog.New(&out).Level(zerolog.DebugLevel)
			c := NewContainer(&log, &dockerContainer.InspectResponse{
				Name:   "/app",
				Config: &dockerContainer.Config{Labels: tt.labels},
			}, nil)

			// Measure only the IsWatchtower call, not the container's creation.
			out.Reset()

			assert.Equal(t, tt.want, c.IsWatchtower())
			assert.Empty(t, out.String())
		})
	}
}

func TestIsWatchtowerParent(t *testing.T) {
	t.Parallel()

	t.Run("should return false for empty chain", func(t *testing.T) {
		t.Parallel()

		result := IsWatchtowerParent(types.ContainerID("test-id"), "")
		assert.False(t, result)
	})

	t.Run("should return false for empty ID", func(t *testing.T) {
		t.Parallel()

		result := IsWatchtowerParent(types.ContainerID(""), "id1,id2")
		assert.False(t, result)
	})

	t.Run("should return true when ID is at start of chain", func(t *testing.T) {
		t.Parallel()

		result := IsWatchtowerParent(types.ContainerID("test-id"), "test-id,id2,id3")
		assert.True(t, result)
	})

	t.Run("should return true when ID is in middle of chain", func(t *testing.T) {
		t.Parallel()

		result := IsWatchtowerParent(types.ContainerID("id2"), "id1,id2,id3")
		assert.True(t, result)
	})

	t.Run("should return true when ID is at end of chain", func(t *testing.T) {
		t.Parallel()

		result := IsWatchtowerParent(types.ContainerID("id3"), "id1,id2,id3")
		assert.True(t, result)
	})

	t.Run("should return true for single ID chain", func(t *testing.T) {
		t.Parallel()

		result := IsWatchtowerParent(types.ContainerID("test-id"), "test-id")
		assert.True(t, result)
	})

	t.Run("should return false when ID is not in chain", func(t *testing.T) {
		t.Parallel()

		result := IsWatchtowerParent(types.ContainerID("test-id"), "id1,id2,id3")
		assert.False(t, result)
	})

	t.Run("should handle chains with spaces around commas", func(t *testing.T) {
		t.Parallel()

		result := IsWatchtowerParent(types.ContainerID("id2"), "id1, id2 , id3")
		assert.True(t, result)
	})
}

// TestContainer_SetLabel verifies that SetLabel writes and overwrites labels,
// creating the labels map when needed, and that DeleteLabel removes labels, with
// both doing nothing when the container info is missing.
func TestContainer_SetLabel(t *testing.T) {
	t.Parallel()

	t.Run("writes a config label", func(t *testing.T) {
		t.Parallel()

		c := MockContainer()
		c.SetLabel(labels.GitRepo, "https://github.com/org/app.git")

		value, ok := c.GetLabel(labels.GitRepo)
		assert.True(t, ok)
		assert.Equal(t, "https://github.com/org/app.git", value)
	})

	t.Run("overwrites an existing label", func(t *testing.T) {
		t.Parallel()

		c := MockContainer(containerfixture.WithLabels(map[string]string{labels.GitRepo: "old"}))
		c.SetLabel(labels.GitRepo, "new")

		value, ok := c.GetLabel(labels.GitRepo)
		assert.True(t, ok)
		assert.Equal(t, "new", value)
	})

	t.Run("creates the labels map when it is nil", func(t *testing.T) {
		t.Parallel()

		c := MockContainer()
		c.containerInfo.Config.Labels = nil
		c.SetLabel(labels.GitRef, "main")

		assert.Equal(t, "main", c.containerInfo.Config.Labels[labels.GitRef])
	})

	t.Run("is a no-op for a nil receiver or empty key", func(t *testing.T) {
		t.Parallel()

		var c *Container
		c.SetLabel(labels.GitRepo, "https://github.com/org/app.git")

		c = MockContainer()
		c.SetLabel("", "value")
		assert.NotContains(t, c.containerInfo.Config.Labels, "")
	})

	t.Run("removes an existing label", func(t *testing.T) {
		t.Parallel()

		c := MockContainer()
		c.SetLabel(labels.GitLastTag, "v1")
		c.DeleteLabel(labels.GitLastTag)
		_, ok := c.GetLabel(labels.GitLastTag)
		assert.False(t, ok)
	})

	t.Run("is a no-op when container info or config is missing", func(t *testing.T) {
		t.Parallel()

		c := MockContainer()
		c.containerInfo.Config = nil

		assert.NotPanics(t, func() { c.SetLabel(labels.GitRepo, "https://github.com/org/app.git") })

		c = MockContainer()
		c.containerInfo = nil

		assert.NotPanics(t, func() { c.SetLabel(labels.GitRepo, "https://github.com/org/app.git") })
	})
}

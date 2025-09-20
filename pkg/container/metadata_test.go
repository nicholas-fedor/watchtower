// Package container provides functionality for managing Docker containers within Watchtower.
// This file contains methods and helpers for accessing and interpreting container metadata,
// focusing on labels that configure Watchtower behavior and lifecycle hooks.
// These methods operate on the Container type defined in dockerContainer.go.
package container

import (
	"testing"

	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestContainer_GetLifecyclePreCheckCommand(t *testing.T) {
	tests := []struct {
		name string
		c    Container
		want string
	}{
		{
			name: "PreCheckLabelSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							preCheckLabel: "echo pre-check",
						},
					},
				},
			},
			want: "echo pre-check",
		},
		{
			name: "PreCheckLabelNotSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			if got := tt.c.GetLifecyclePreCheckCommand(); got != tt.want {
				t.Errorf("Container.GetLifecyclePreCheckCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainer_GetLifecyclePostCheckCommand(t *testing.T) {
	tests := []struct {
		name string
		c    Container
		want string
	}{
		{
			name: "PostCheckLabelSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							postCheckLabel: "echo post-check",
						},
					},
				},
			},
			want: "echo post-check",
		},
		{
			name: "PostCheckLabelNotSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			if got := tt.c.GetLifecyclePostCheckCommand(); got != tt.want {
				t.Errorf("Container.GetLifecyclePostCheckCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainer_GetLifecyclePreUpdateCommand(t *testing.T) {
	tests := []struct {
		name string
		c    Container
		want string
	}{
		{
			name: "PreUpdateLabelSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							preUpdateLabel: "echo pre-update",
						},
					},
				},
			},
			want: "echo pre-update",
		},
		{
			name: "PreUpdateLabelNotSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			if got := tt.c.GetLifecyclePreUpdateCommand(); got != tt.want {
				t.Errorf("Container.GetLifecyclePreUpdateCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainer_GetLifecyclePostUpdateCommand(t *testing.T) {
	tests := []struct {
		name string
		c    Container
		want string
	}{
		{
			name: "PostUpdateLabelSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							postUpdateLabel: "echo post-update",
						},
					},
				},
			},
			want: "echo post-update",
		},
		{
			name: "PostUpdateLabelNotSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			if got := tt.c.GetLifecyclePostUpdateCommand(); got != tt.want {
				t.Errorf("Container.GetLifecyclePostUpdateCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainer_PreUpdateTimeout(t *testing.T) {
	tests := []struct {
		name string
		c    Container
		want int
	}{
		{
			name: "ValidTimeout",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							preUpdateTimeoutLabel: "5",
						},
					},
				},
			},
			want: 5,
		},
		{
			name: "InvalidTimeout",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							preUpdateTimeoutLabel: "invalid",
						},
					},
				},
			},
			want: 1,
		},
		{
			name: "TimeoutNotSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			if got := tt.c.PreUpdateTimeout(); got != tt.want {
				t.Errorf("Container.PreUpdateTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainer_PostUpdateTimeout(t *testing.T) {
	tests := []struct {
		name string
		c    Container
		want int
	}{
		{
			name: "ValidTimeout",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							postUpdateTimeoutLabel: "10",
						},
					},
				},
			},
			want: 10,
		},
		{
			name: "InvalidTimeout",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							postUpdateTimeoutLabel: "invalid",
						},
					},
				},
			},
			want: 1,
		},
		{
			name: "TimeoutNotSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			if got := tt.c.PostUpdateTimeout(); got != tt.want {
				t.Errorf("Container.PostUpdateTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainer_Enabled(t *testing.T) {
	tests := []struct {
		name  string
		c     Container
		want  bool
		want1 bool
	}{
		{
			name: "EnabledTrue",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							enableLabel: "true",
						},
					},
				},
			},
			want:  true,
			want1: true,
		},
		{
			name: "EnabledFalse",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							enableLabel: "false",
						},
					},
				},
			},
			want:  false,
			want1: true,
		},
		{
			name: "LabelNotSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							enableLabel: "invalid",
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
			if got != tt.want {
				t.Errorf("Container.Enabled() got = %v, want %v", got, tt.want)
			}

			if got1 != tt.want1 {
				t.Errorf("Container.Enabled() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestContainer_IsMonitorOnly(t *testing.T) {
	type args struct {
		params types.UpdateParams
	}

	tests := []struct {
		name string
		c    Container
		args args
		want bool
	}{
		{
			name: "LabelTruePrecedence",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							monitorOnlyLabel: "true",
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
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							monitorOnlyLabel: "false",
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
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			if got := tt.c.IsMonitorOnly(tt.args.params); got != tt.want {
				t.Errorf("Container.IsMonitorOnly() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainer_IsNoPull(t *testing.T) {
	type args struct {
		params types.UpdateParams
	}

	tests := []struct {
		name string
		c    Container
		args args
		want bool
	}{
		{
			name: "LabelTruePrecedence",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							noPullLabel: "true",
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
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							noPullLabel: "false",
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
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			if got := tt.c.IsNoPull(tt.args.params); got != tt.want {
				t.Errorf("Container.IsNoPull() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainer_Scope(t *testing.T) {
	tests := []struct {
		name  string
		c     Container
		want  string
		want1 bool
	}{
		{
			name: "ScopeSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							scope: "test-scope",
						},
					},
				},
			},
			want:  "test-scope",
			want1: true,
		},
		{
			name: "ScopeNotSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			if got != tt.want {
				t.Errorf("Container.Scope() got = %v, want %v", got, tt.want)
			}

			if got1 != tt.want1 {
				t.Errorf("Container.Scope() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestContainer_IsWatchtower(t *testing.T) {
	tests := []struct {
		name string
		c    Container
		want bool
	}{
		{
			name: "IsWatchtowerTrue",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							watchtowerLabel: "true",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "IsWatchtowerFalse",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							watchtowerLabel: "false",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "LabelNotSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.IsWatchtower(); got != tt.want {
				t.Errorf("Container.IsWatchtower() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainer_StopSignal(t *testing.T) {
	tests := []struct {
		name string
		c    Container
		want string
	}{
		{
			name: "SignalSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							signalLabel: "SIGTERM",
						},
					},
				},
			},
			want: "SIGTERM",
		},
		{
			name: "SignalNotSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			if got := tt.c.StopSignal(); got != tt.want {
				t.Errorf("Container.StopSignal() = %v, want %v", got, tt.want)
			}
		})
	}
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
					watchtowerLabel: "true",
				},
			},
			want: true,
		},
		{
			name: "WatchtowerFalse",
			args: args{
				labels: map[string]string{
					watchtowerLabel: "false",
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainsWatchtowerLabel(tt.args.labels); got != tt.want {
				t.Errorf("ContainsWatchtowerLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainer_getLabelValueOrEmpty(t *testing.T) {
	type args struct {
		label string
	}

	tests := []struct {
		name string
		c    Container
		args args
		want string
	}{
		{
			name: "LabelSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			if got := tt.c.getLabelValueOrEmpty(tt.args.label); got != tt.want {
				t.Errorf("Container.getLabelValueOrEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainer_getLabelValue(t *testing.T) {
	type args struct {
		label string
	}

	tests := []struct {
		name  string
		c     Container
		args  args
		want  string
		want1 bool
	}{
		{
			name: "LabelSet",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			if got != tt.want {
				t.Errorf("Container.getLabelValue() got = %v, want %v", got, tt.want)
			}

			if got1 != tt.want1 {
				t.Errorf("Container.getLabelValue() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestContainer_getBoolLabelValue(t *testing.T) {
	type args struct {
		label string
	}

	tests := []struct {
		name    string
		c       Container
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "TrueValue",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
					Config: &dockerContainer.Config{
						Labels: map[string]string{},
					},
				},
			},
			args:    args{label: "test.label"},
			want:    false,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.c.getBoolLabelValue(tt.args.label)
			if (err != nil) != tt.wantErr {
				t.Errorf("Container.getBoolLabelValue() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if got != tt.want {
				t.Errorf("Container.getBoolLabelValue() = %v, want %v", got, tt.want)
			}
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
		c    Container
		args args
		want bool
	}{
		{
			name: "LabelTruePrecedence",
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			c: Container{
				containerInfo: &dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{
						Name: "/test-container",
					},
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
			if got := tt.c.getContainerOrGlobalBool(tt.args.globalVal, tt.args.label, tt.args.contPrecedence); got != tt.want {
				t.Errorf("Container.getContainerOrGlobalBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

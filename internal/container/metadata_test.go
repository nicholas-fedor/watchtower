package container

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math"
	"strconv"
	"strings"
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

// TestContainer_HookTimeouts verifies the lifecycle hook timeout labels. A
// whole number of 0 or more is used as given, where 0 runs the hook without a
// timeout. An unset label gives the 1 minute default. A non-numeric, empty or
// negative value also gives the default and logs a warning.
func TestContainer_HookTimeouts(t *testing.T) {
	t.Parallel()

	hooks := []struct {
		name    string
		label   string
		warning string
		timeout func(c *Container) int
	}{
		{
			name:    "pre-check",
			label:   labels.LifecyclePreCheckTimeout,
			warning: "Invalid pre-check timeout value, using default",
			timeout: (*Container).PreCheckTimeout,
		},
		{
			name:    "post-check",
			label:   labels.LifecyclePostCheckTimeout,
			warning: "Invalid post-check timeout value, using default",
			timeout: (*Container).PostCheckTimeout,
		},
		{
			name:    "pre-update",
			label:   labels.LifecyclePreUpdateTimeout,
			warning: "Invalid pre-update timeout value, using default",
			timeout: (*Container).PreUpdateTimeout,
		},
		{
			name:    "post-update",
			label:   labels.LifecyclePostUpdateTimeout,
			warning: "Invalid post-update timeout value, using default",
			timeout: (*Container).PostUpdateTimeout,
		},
	}

	tests := []struct {
		name     string
		value    *string
		want     int
		wantWarn bool
	}{
		{name: "valid", value: new("5"), want: 5},
		{name: "zero disables the timeout", value: new("0"), want: 0},
		{name: "not set", value: nil, want: 1},
		{name: "not a number", value: new("invalid"), want: 1, wantWarn: true},
		{name: "empty", value: new(""), want: 1, wantWarn: true},
		{name: "negative", value: new("-1"), want: 1, wantWarn: true},
	}

	for _, hook := range hooks {
		for _, tt := range tests {
			t.Run(hook.name+" "+tt.name, func(t *testing.T) {
				t.Parallel()

				containerLabels := map[string]string{}
				if tt.value != nil {
					containerLabels[hook.label] = *tt.value
				}

				var out bytes.Buffer

				log := zerolog.New(&out).Level(zerolog.WarnLevel)
				// A fresh ID keeps the record of reported labels from hiding
				// this case's warning behind another case or test run.
				c := &Container{
					containerInfo: &dockerContainer.InspectResponse{
						ID:     randomContainerID(t),
						Name:   "/test-container",
						Config: &dockerContainer.Config{Labels: containerLabels},
					},
					log: &log,
				}

				assert.Equal(t, tt.want, hook.timeout(c))

				if tt.wantWarn {
					assert.Contains(t, out.String(), hook.warning)
				} else {
					assert.Empty(t, out.String())
				}
			})
		}
	}
}

// Test_parseHookTimeout verifies that a hook timeout label value must be a
// whole number of minutes of 0 or more.
func Test_parseHookTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "positive", value: "10", want: 10},
		{name: "zero", value: "0", want: 0},
		{name: "negative", value: "-1", wantErr: true},
		{name: "fraction", value: "1.5", wantErr: true},
		{name: "empty", value: "", wantErr: true},
		{name: "longest duration", value: "153722867", want: 153722867},
		{name: "too long for a duration", value: "153722868", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseHookTimeout(tt.value)
			if tt.wantErr {
				require.ErrorIs(t, err, errInvalidHookTimeout)

				return
			}

			require.NoError(t, err)
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

// TestContainer_StopTimeout verifies that a stop timeout of 0 or more set on
// the container takes precedence, and that a missing timeout, missing inspect
// data, and a timeout of -1 (wait indefinitely) use the fallback. A stop
// timeout too long for a duration saturates instead of overflowing.
func TestContainer_StopTimeout(t *testing.T) {
	t.Parallel()

	const fallback = 45 * time.Second

	// largestStopTimeout is how long a stop waits for a container stop timeout
	// of the largest int. A 64-bit int of seconds is too long for a duration,
	// so it saturates. A 32-bit int of seconds fits.
	largestStopTimeout := time.Duration(math.MaxInt64)
	if strconv.IntSize == 32 {
		largestStopTimeout = math.MaxInt32 * time.Second
	}

	// withStopTimeout returns a container whose config sets the given stop
	// timeout in seconds.
	withStopTimeout := func(seconds int) *Container {
		return &Container{
			containerInfo: &dockerContainer.InspectResponse{
				Name: "/test-container",
				Config: &dockerContainer.Config{
					StopTimeout: &seconds,
					Labels:      map[string]string{},
				},
			},
		}
	}

	tests := []struct {
		name string
		c    *Container
		want time.Duration
	}{
		{name: "set", c: withStopTimeout(60), want: time.Minute},
		{name: "set to zero", c: withStopTimeout(0), want: 0},
		{name: "set to wait indefinitely", c: withStopTimeout(-1), want: fallback},
		{name: "set to the largest int", c: withStopTimeout(math.MaxInt), want: largestStopTimeout},
		{
			name: "not set",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{
					Name:   "/test-container",
					Config: &dockerContainer.Config{Labels: map[string]string{}},
				},
			},
			want: fallback,
		},
		{name: "no inspect data", c: &Container{containerInfo: nil}, want: fallback},
		{
			name: "no config",
			c: &Container{
				containerInfo: &dockerContainer.InspectResponse{Name: "/test-container", Config: nil},
			},
			want: fallback,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.c.StopTimeout(fallback))
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

// TestContainer_InvalidLabelsWarnOnce verifies that an invalid label value is
// logged as a warning, which notifications include, on the first read for a
// container, and at debug level on later reads. Each label is read on every
// scan, hook run or API request, so a lasting misconfiguration would otherwise
// notify every time. Another container with the same label warns again, and a
// problem is warned about once, not by two log lines.
func TestContainer_InvalidLabelsWarnOnce(t *testing.T) {
	t.Parallel()

	// aboveMaximumUID is the warning for a UID of 3000000000. A 64-bit int
	// holds it, so it fails the range check. It overflows a 32-bit int, so
	// parsing fails first there.
	aboveMaximumUID := "Invalid lifecycle UID value: exceeds maximum reasonable value"
	if strconv.IntSize == 32 {
		aboveMaximumUID = "Invalid lifecycle UID value: not a valid integer"
	}

	tests := []struct {
		name    string
		labels  map[string]string
		message string
		read    func(c *Container)
	}{
		{
			name:    "hook timeout",
			labels:  map[string]string{labels.LifecyclePreCheckTimeout: "1.5"},
			message: "Invalid pre-check timeout value, using default",
			read:    func(c *Container) { c.PreCheckTimeout() },
		},
		{
			name:    "lifecycle UID that is not a number",
			labels:  map[string]string{labels.LifecycleUID: "root"},
			message: "Invalid lifecycle UID value: not a valid integer",
			read:    func(c *Container) { c.GetLifecycleUID() },
		},
		{
			name:    "negative lifecycle GID",
			labels:  map[string]string{labels.LifecycleGID: "-1"},
			message: "Invalid lifecycle GID value: must be non-negative",
			read:    func(c *Container) { c.GetLifecycleGID() },
		},
		{
			name:    "lifecycle UID above the maximum",
			labels:  map[string]string{labels.LifecycleUID: "3000000000"},
			message: aboveMaximumUID,
			read:    func(c *Container) { c.GetLifecycleUID() },
		},
		{
			name:    "enable label",
			labels:  map[string]string{labels.Enable: "yes please"},
			message: "Invalid enable label value",
			read:    func(c *Container) { c.Enabled() },
		},
		{
			name:    "cooldown delay",
			labels:  map[string]string{labels.CooldownDelay: "soon"},
			message: "Failed to parse cooldown-delay label, using global value",
			read:    func(c *Container) { c.CooldownDelay(types.UpdateParams{}) },
		},
		{
			name:    "boolean label",
			labels:  map[string]string{labels.MonitorOnly: "maybe"},
			message: "Failed to parse label value",
			read:    func(c *Container) { c.IsMonitorOnly(types.UpdateParams{}) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Fresh IDs per run keep the process-wide record of reported
			// labels from carrying over between test runs.
			first := &Container{
				containerInfo: &dockerContainer.InspectResponse{
					ID:     randomContainerID(t),
					Name:   "/first",
					Config: &dockerContainer.Config{Labels: tt.labels},
				},
			}
			second := &Container{
				containerInfo: &dockerContainer.InspectResponse{
					ID:     randomContainerID(t),
					Name:   "/second",
					Config: &dockerContainer.Config{Labels: tt.labels},
				},
			}

			firstRead := readLabelLogs(first, tt.read)
			assert.Equal(t, []string{tt.message}, messagesAt(t, firstRead, "warn"))

			laterRead := readLabelLogs(first, tt.read)
			assert.Empty(t, messagesAt(t, laterRead, "warn"))
			assert.Contains(t, messagesAt(t, laterRead, "debug"), tt.message)

			otherContainer := readLabelLogs(second, tt.read)
			assert.Equal(t, []string{tt.message}, messagesAt(t, otherContainer, "warn"))
		})
	}
}

// TestContainer_InvalidLabelsWarnOncePerLabel verifies that one container
// warns about each of its invalid labels, since a reported label does not hide
// a different one. A label read again after its warning is logged at debug.
func TestContainer_InvalidLabelsWarnOncePerLabel(t *testing.T) {
	t.Parallel()

	const (
		uidWarning = "Invalid lifecycle UID value: not a valid integer"
		gidWarning = "Invalid lifecycle GID value: not a valid integer"
	)

	// A fresh ID per run keeps the process-wide record of reported labels
	// from carrying over between test runs.
	c := &Container{
		containerInfo: &dockerContainer.InspectResponse{
			ID:   randomContainerID(t),
			Name: "/app",
			Config: &dockerContainer.Config{Labels: map[string]string{
				labels.LifecycleUID: "root",
				labels.LifecycleGID: "wheel",
			}},
		},
	}
	readUID := func(c *Container) { c.GetLifecycleUID() }
	readGID := func(c *Container) { c.GetLifecycleGID() }

	assert.Equal(t, []string{uidWarning}, messagesAt(t, readLabelLogs(c, readUID), "warn"))
	assert.Equal(t, []string{gidWarning}, messagesAt(t, readLabelLogs(c, readGID), "warn"))

	laterRead := readLabelLogs(c, readUID)
	assert.Empty(t, messagesAt(t, laterRead, "warn"))
	assert.Contains(t, messagesAt(t, laterRead, "debug"), uidWarning)
}

// readLabelLogs runs read on c with a debug-level JSON logger and returns
// what it logged.
func readLabelLogs(c *Container, read func(c *Container)) string {
	var out bytes.Buffer

	log := zerolog.New(&out).Level(zerolog.DebugLevel)
	c.log = &log

	read(c)

	return out.String()
}

// messagesAt returns the messages of the JSON log lines at the given level, in
// the order they were logged.
func messagesAt(t *testing.T, logs, level string) []string {
	t.Helper()

	var messages []string

	for line := range strings.Lines(logs) {
		var record map[string]any

		require.NoError(t, json.Unmarshal([]byte(line), &record))

		if record["level"] == level {
			message, ok := record["message"].(string)
			require.True(t, ok)

			messages = append(messages, message)
		}
	}

	return messages
}

// randomContainerID returns 64 random hex characters, the form of a Docker
// container ID.
func randomContainerID(t *testing.T) string {
	t.Helper()

	id := make([]byte, 32)
	_, err := rand.Read(id)
	require.NoError(t, err)

	return hex.EncodeToString(id)
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

// TestIsSuperseded verifies which running Watchtower containers a self-update
// has replaced: a predecessor renamed with the "watchtower-old-" prefix, with
// or without the leading slash Docker reports, and a container whose chain
// label lists it as a parent.
func TestIsSuperseded(t *testing.T) {
	t.Parallel()

	const id = "test-container-id"

	withChain := func(chain string) containerfixture.Update {
		return containerfixture.WithLabels(map[string]string{labels.ContainerChain: chain})
	}

	tests := []struct {
		name      string
		container types.Container
		want      bool
	}{
		{name: "no container", container: nil, want: false},
		{
			name:      "current container",
			container: MockContainer(containerfixture.WithID(id), containerfixture.WithName("/watchtower")),
			want:      false,
		},
		{
			name: "chain without the container",
			container: MockContainer(containerfixture.WithID(id), containerfixture.WithName("/watchtower"),
				withChain("other-id")),
			want: false,
		},
		{
			name: "parent in the chain",
			container: MockContainer(containerfixture.WithID(id), containerfixture.WithName("/watchtower"),
				withChain(id+",parent-id")),
			want: true,
		},
		{
			name:      "renamed predecessor",
			container: MockContainer(containerfixture.WithID(id), containerfixture.WithName("watchtower-old-abc123")),
			want:      true,
		},
		{
			name:      "renamed predecessor with a leading slash",
			container: MockContainer(containerfixture.WithID(id), containerfixture.WithName("/watchtower-old-def456")),
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, IsSuperseded(tt.container))
		})
	}
}

// TestNormalizedScope verifies that an unset, empty, or "none" scope label is
// the scope "none", and that any other value is kept.
func TestNormalizedScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{name: "no scope label", labels: map[string]string{}, want: "none"},
		{name: "empty scope label", labels: map[string]string{labels.Scope: ""}, want: "none"},
		{name: "none scope label", labels: map[string]string{labels.Scope: "none"}, want: "none"},
		{name: "named scope", labels: map[string]string{labels.Scope: "prod"}, want: "prod"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := MockContainer(containerfixture.WithLabels(tt.labels))

			assert.Equal(t, tt.want, c.NormalizedScope())
		})
	}
}

// TestIsOrphanedReplacement verifies which containers count as replacements
// that an interrupted self-update left in the created state. Only a created
// Watchtower container in the current container's scope counts, unless it is
// the current container or a renamed predecessor. An unset scope label and an
// empty one are the same scope.
func TestIsOrphanedReplacement(t *testing.T) {
	t.Parallel()

	created := containerfixture.WithContainerState(dockerContainer.State{Status: dockerContainer.StateCreated})
	running := containerfixture.WithContainerState(dockerContainer.State{Status: dockerContainer.StateRunning, Running: true})
	watchtower := func(scope string) containerfixture.Update {
		containerLabels := map[string]string{labels.Watchtower: "true"}
		if scope != "" {
			containerLabels[labels.Scope] = scope
		}

		return containerfixture.WithLabels(containerLabels)
	}

	current := MockContainer(containerfixture.WithID("current"), containerfixture.WithName("/watchtower"), running,
		watchtower(""))

	tests := []struct {
		name      string
		candidate types.Container
		want      bool
	}{
		{
			name:      "created Watchtower container in the same scope",
			candidate: MockContainer(containerfixture.WithID("new"), containerfixture.WithName("/watchtower-new"), created, watchtower("")),
			want:      true,
		},
		{
			name: "empty scope label matches an unset one",
			candidate: MockContainer(containerfixture.WithID("new"), containerfixture.WithName("/watchtower-new"), created,
				containerfixture.WithLabels(map[string]string{labels.Watchtower: "true", labels.Scope: ""})),
			want: true,
		},
		{
			name:      "different scope",
			candidate: MockContainer(containerfixture.WithID("new"), containerfixture.WithName("/watchtower-new"), created, watchtower("prod")),
			want:      false,
		},
		{
			name:      "not created",
			candidate: MockContainer(containerfixture.WithID("new"), containerfixture.WithName("/watchtower-new"), running, watchtower("")),
			want:      false,
		},
		{
			name: "not a Watchtower container",
			candidate: MockContainer(containerfixture.WithID("new"), containerfixture.WithName("/app"), created,
				containerfixture.WithLabels(map[string]string{})),
			want: false,
		},
		{
			name: "renamed predecessor",
			candidate: MockContainer(containerfixture.WithID("old"), containerfixture.WithName("/watchtower-old-abc123"), created,
				watchtower("")),
			want: false,
		},
		{
			name:      "the current container",
			candidate: MockContainer(containerfixture.WithID("current"), containerfixture.WithName("/watchtower"), created, watchtower("")),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, IsOrphanedReplacement(tt.candidate, current))
		})
	}
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

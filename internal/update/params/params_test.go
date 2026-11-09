package params

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/observability/events"
	updateConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/update"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
)

// stopTimeoutContainer returns a container with its own stop timeout, or with
// none when own is nil.
//
// Parameters:
//   - own: The container's stop timeout, or nil.
//
// Returns:
//   - *container.Container: Container built from inspect data.
func stopTimeoutContainer(own *time.Duration) *container.Container {
	return modelfixture.Container(func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if own != nil {
			seconds := int(own.Seconds())
			c.Config.StopTimeout = &seconds
		}
	})
}

// TestParams_DetachedTimeout verifies that detached Docker calls are
// bounded by the stop timeout plus 30 seconds. A stop timeout of zero or below
// leaves 30 seconds, and a stop timeout near the longest duration saturates
// instead of overflowing.
func TestParams_DetachedTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{name: "zero timeout", timeout: 0, want: 30 * time.Second},
		{name: "negative timeout", timeout: -time.Second, want: 30 * time.Second},
		{name: "short timeout", timeout: time.Second, want: 31 * time.Second},
		{name: "default timeout", timeout: 30 * time.Second, want: time.Minute},
		{name: "long timeout", timeout: 10 * time.Minute, want: 10*time.Minute + 30*time.Second},
		{name: "longest timeout", timeout: math.MaxInt64, want: math.MaxInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Params{Timeout: tt.timeout}.DetachedTimeout())
		})
	}
}

// TestParams_DetachedStopTimeout verifies that the bound for detached
// calls that stop a container follows the longer of the configured stop timeout
// and the container's own, plus 30 seconds.
func TestParams_DetachedStopTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
		own     *time.Duration
		want    time.Duration
	}{
		{name: "no container stop timeout", timeout: 20 * time.Second, want: 50 * time.Second},
		{name: "longer container stop timeout", timeout: 20 * time.Second, own: new(2 * time.Minute), want: 150 * time.Second},
		{name: "shorter container stop timeout", timeout: 20 * time.Second, own: new(5 * time.Second), want: 50 * time.Second},
		{name: "container stop timeout without a configured one", timeout: 0, own: new(time.Minute), want: 90 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Params{Timeout: tt.timeout}.DetachedStopTimeout(stopTimeoutContainer(tt.own))
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestParams_ScanStartedData checks that each published setting reaches
// its own payload field. Flags the event does not publish leave the payload
// empty.
func TestParams_ScanStartedData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params Params
		want   events.ScanStartedData
	}{
		{name: "no settings", params: Params{}, want: events.ScanStartedData{}},
		{name: "cleanup", params: Params{Cleanup: true}, want: events.ScanStartedData{Cleanup: true}},
		{name: "no restart", params: Params{NoRestart: true}, want: events.ScanStartedData{NoRestart: true}},
		{
			name:   "monitor only",
			params: Params{Policy: updateConfig.Policy{MonitorOnly: true}},
			want:   events.ScanStartedData{MonitorOnly: true},
		},
		{
			name:   "lifecycle hooks",
			params: Params{LifecycleHooks: true},
			want:   events.ScanStartedData{LifecycleHooks: true},
		},
		{
			name:   "rolling restart",
			params: Params{RollingRestart: true},
			want:   events.ScanStartedData{RollingRestart: true},
		},
		{
			name:   "label precedence",
			params: Params{Policy: updateConfig.Policy{LabelPrecedence: true}},
			want:   events.ScanStartedData{LabelPrecedence: true},
		},
		{
			name:   "no pull",
			params: Params{Policy: updateConfig.Policy{NoPull: true}},
			want:   events.ScanStartedData{NoPull: true},
		},
		{name: "run once", params: Params{RunOnce: true}, want: events.ScanStartedData{RunOnce: true}},
		{
			name:   "compose depends on",
			params: Params{UseComposeDependsOn: true},
			want:   events.ScanStartedData{UseComposeDependsOn: true},
		},
		{
			name:   "skip self-update",
			params: Params{SkipSelfUpdate: true},
			want:   events.ScanStartedData{SkipSelfUpdate: true},
		},
		{
			name:   "ephemeral self-update",
			params: Params{EphemeralSelfUpdate: true},
			want:   events.ScanStartedData{EphemeralSelfUpdate: true},
		},
		{
			name:   "revive stopped",
			params: Params{ReviveStopped: true},
			want:   events.ScanStartedData{ReviveStopped: true},
		},
		{
			name: "unpublished flags",
			params: Params{
				Policy:          updateConfig.Policy{EnableChangelog: true},
				LabelEnable:     true,
				GitComposeStash: true,
			},
			want: events.ScanStartedData{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.params.ScanStartedData())
		})
	}
}

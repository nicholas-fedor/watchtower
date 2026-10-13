package scheduling

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/logging"
	"github.com/nicholas-fedor/watchtower/internal/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestValidateSpec(t *testing.T) {
	t.Parallel()

	valid := []string{
		"",
		"  ",
		"@every 6h",
		"@every 86400s",
		"0 0 4 * * *",
		"0 4 * * *",
		`"0 0 4 * * *"`,
		"@daily",
	}
	for _, spec := range valid {
		require.NoError(t, ValidateSpec(spec), "spec %q", spec)
	}

	invalid := []string{"never", "@every soon", "99 * * * *", "* * *"}
	for _, spec := range invalid {
		require.Error(t, ValidateSpec(spec), "spec %q", spec)
	}
}

// TestRunUpgradesOnSchedule_AppliesScheduleChanges starts with no schedule,
// then turns one on at run time and expects updates to begin.
func TestRunUpgradesOnSchedule_AppliesScheduleChanges(t *testing.T) {
	log := logging.NopLogger()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	changes := make(chan string, 1)
	ran := make(chan struct{}, 8)
	done := make(chan error, 1)

	go func() {
		done <- RunUpgradesOnSchedule(ctx, ScheduleDeps{
			Logger:          log,
			ScheduleSpec:    "",
			ScheduleChanges: changes,
			RunUpdate: func(context.Context, types.Filter, types.UpdateParams) *metrics.Metric {
				select {
				case ran <- struct{}{}:
				default:
				}

				return nil
			},
		})
	}()

	// Nothing is scheduled yet, so no update should run.
	select {
	case <-ran:
		t.Fatal("update ran before a schedule was set")
	case <-time.After(1200 * time.Millisecond):
	}

	changes <- "@every 1s"

	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("update did not run after the schedule was turned on")
	}

	// Turning the schedule off stops further runs.
	changes <- ""

	time.Sleep(300 * time.Millisecond)

	for len(ran) > 0 {
		<-ran
	}

	select {
	case <-ran:
		t.Fatal("update ran after the schedule was turned off")
	case <-time.After(1500 * time.Millisecond):
	}

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("scheduler did not stop")
	}
}

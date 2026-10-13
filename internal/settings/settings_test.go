package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var (
	errBadSchedule = errors.New("bad schedule")
	errLinked      = errors.New("containers are linked")
)

func changes(t *testing.T, body string) map[string]json.RawMessage {
	t.Helper()

	var parsed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(body), &parsed))

	return parsed
}

func rejectBadSchedule(spec string) error {
	if spec == "never" {
		return errBadSchedule
	}

	return nil
}

func TestOpenWithoutDataDir(t *testing.T) {
	t.Parallel()

	defaults := Values{Cleanup: true, Schedule: "@every 24h"}

	for _, dir := range []string{"", filepath.Join(t.TempDir(), "missing")} {
		store := Open(nil, dir, defaults, Validators{})

		assert.False(t, store.Persistent())
		assert.Equal(t, defaults, store.Effective())

		// Changes still apply for the life of the process.
		require.NoError(t, store.Update(changes(t, `{"monitor_only":true}`)))
		assert.True(t, store.Effective().MonitorOnly)
	}
}

func TestUpdateAppliesAndPersists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	defaults := Values{Cleanup: true, Schedule: "@every 24h"}

	store := Open(nil, dir, defaults, Validators{Schedule: rejectBadSchedule})
	require.True(t, store.Persistent())

	require.NoError(t, store.Update(changes(t, `{"monitor_only":true,"cleanup":false,"schedule":" 0 0 4 * * * "}`)))

	want := Values{MonitorOnly: true, Cleanup: false, Schedule: "0 0 4 * * *"}
	assert.Equal(t, want, store.Effective())
	assert.Equal(t, defaults, store.Defaults())
	assert.Equal(t, map[string]bool{
		KeyMonitorOnly:        true,
		KeyCleanup:            true,
		KeyRollingRestart:     false,
		KeySchedule:           true,
		KeyAutoRollback:       false,
		KeyContainerOverrides: false,
	}, store.Overridden())

	// A new store over the same directory sees the saved overrides.
	reopened := Open(nil, dir, defaults, Validators{Schedule: rejectBadSchedule})
	assert.Equal(t, want, reopened.Effective())
}

func TestUpdateContainerOverrides(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	defaults := Values{}

	store := Open(nil, dir, defaults, Validators{})
	require.NoError(t, store.Update(changes(t, `{"auto_rollback":true,"container_overrides":{"plex":"pinned","nextcloud":"monitor_only"}}`)))

	eff := store.Effective()
	assert.True(t, eff.AutoRollback)
	assert.Equal(t, map[string]string{"plex": "pinned", "nextcloud": "monitor_only"}, eff.ContainerOverrides)

	// Setting override to "auto" removes that container override
	require.NoError(t, store.Update(changes(t, `{"container_overrides":{"plex":"auto"}}`)))
	assert.Equal(t, map[string]string{"nextcloud": "monitor_only"}, store.Effective().ContainerOverrides)
}

func TestUpdateNullResetsToDefault(t *testing.T) {
	t.Parallel()

	store := Open(nil, t.TempDir(), Values{Cleanup: true}, Validators{})

	require.NoError(t, store.Update(changes(t, `{"cleanup":false}`)))
	require.False(t, store.Effective().Cleanup)

	require.NoError(t, store.Update(changes(t, `{"cleanup":null}`)))
	assert.True(t, store.Effective().Cleanup)
	assert.False(t, store.Overridden()[KeyCleanup])
}

func TestUpdateRejectsInvalidChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "unknown setting", body: `{"no_pull":true}`, want: ErrUnknownSetting},
		{name: "wrong type", body: `{"cleanup":"yes"}`, want: ErrInvalidValue},
		{name: "schedule wrong type", body: `{"schedule":5}`, want: ErrInvalidValue},
		{name: "schedule fails validation", body: `{"schedule":"never"}`, want: errBadSchedule},
		{
			name: "rolling restart with monitor only",
			body: `{"monitor_only":true,"rolling_restart":true}`,
			want: ErrIncompatible,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defaults := Values{Cleanup: true, Schedule: "@every 24h"}
			store := Open(nil, t.TempDir(), defaults, Validators{Schedule: rejectBadSchedule})

			err := store.Update(changes(t, tt.body))

			require.ErrorIs(t, err, tt.want)
			assert.Equal(t, defaults, store.Effective(), "a rejected update must change nothing")
		})
	}
}

func TestUpdateIsAtomic(t *testing.T) {
	t.Parallel()

	defaults := Values{Schedule: "@every 24h"}
	store := Open(nil, t.TempDir(), defaults, Validators{Schedule: rejectBadSchedule})

	err := store.Update(changes(t, `{"cleanup":true,"schedule":"never"}`))

	require.Error(t, err)
	assert.Equal(t, defaults, store.Effective())
}

func TestRollingRestartValidator(t *testing.T) {
	t.Parallel()

	calls := 0
	store := Open(nil, "", Values{}, Validators{RollingRestart: func() error {
		calls++

		return errLinked
	}})

	require.ErrorIs(t, store.Update(changes(t, `{"rolling_restart":true}`)), errLinked)
	assert.False(t, store.Effective().RollingRestart)

	// Unrelated changes do not consult the validator.
	require.NoError(t, store.Update(changes(t, `{"cleanup":true}`)))
	assert.Equal(t, 1, calls)
}

func TestScheduleChangesDeliversLatest(t *testing.T) {
	t.Parallel()

	store := Open(nil, "", Values{Schedule: "@every 24h"}, Validators{})

	require.NoError(t, store.Update(changes(t, `{"schedule":"@every 6h"}`)))
	require.NoError(t, store.Update(changes(t, `{"schedule":"@every 1h"}`)))
	// A change that leaves the schedule alone announces nothing.
	require.NoError(t, store.Update(changes(t, `{"cleanup":true}`)))

	select {
	case spec := <-store.ScheduleChanges():
		assert.Equal(t, "@every 1h", spec)
	default:
		t.Fatal("expected a schedule change")
	}

	select {
	case spec := <-store.ScheduleChanges():
		t.Fatalf("unexpected second schedule change %q", spec)
	default:
	}
}

func TestLoadDropsSettingsThatNoLongerApply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		saved string
	}{
		{name: "not JSON", saved: `{`},
		{name: "invalid schedule", saved: `{"schedule":"never"}`},
		{name: "incompatible", saved: `{"monitor_only":true,"rolling_restart":true}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, FileName), []byte(tt.saved), 0o600))

			defaults := Values{Schedule: "@every 24h"}
			store := Open(nil, dir, defaults, Validators{Schedule: rejectBadSchedule})

			assert.Equal(t, defaults, store.Effective())
		})
	}
}

func TestApplyTo(t *testing.T) {
	t.Parallel()

	store := Open(nil, "", Values{Cleanup: true}, Validators{})
	require.NoError(t, store.Update(changes(t, `{"monitor_only":true}`)))

	params := store.ApplyTo(types.UpdateParams{NoPull: true, RollingRestart: true})

	assert.True(t, params.MonitorOnly)
	assert.True(t, params.Cleanup)
	assert.False(t, params.RollingRestart)
	assert.True(t, params.NoPull, "settings the store does not own are left alone")

	var none *Store

	assert.Equal(t, types.UpdateParams{NoPull: true}, none.ApplyTo(types.UpdateParams{NoPull: true}))
}

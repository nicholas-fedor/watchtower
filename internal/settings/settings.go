package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	// FileName is the name of the overrides file inside the data directory.
	FileName = "settings.json"

	// KeyMonitorOnly is the setting that reports updates without applying them.
	KeyMonitorOnly = "monitor_only"
	// KeyCleanup is the setting that removes old images after an update.
	KeyCleanup = "cleanup"
	// KeyRollingRestart is the setting that restarts containers one at a time.
	KeyRollingRestart = "rolling_restart"
	// KeySchedule is the cron expression that decides when scans run.
	KeySchedule = "schedule"
)

var (
	// ErrUnknownSetting is returned when a change names a setting that does not exist.
	ErrUnknownSetting = errors.New("unknown setting")
	// ErrInvalidValue is returned when a change carries a value that cannot be used.
	ErrInvalidValue = errors.New("invalid value")
	// ErrIncompatible is returned when a change would leave settings that cannot work together.
	ErrIncompatible = errors.New("rolling restart cannot be used together with monitor only")
)

// Values is a complete set of runtime settings.
type Values struct {
	// MonitorOnly reports available updates without applying them.
	MonitorOnly bool
	// Cleanup removes the previous image after a container is updated.
	Cleanup bool
	// RollingRestart restarts containers one at a time.
	RollingRestart bool
	// Schedule is the cron expression for periodic scans. Empty disables them.
	Schedule string
}

// Validators are optional checks supplied by the composition root.
type Validators struct {
	// Schedule reports whether a cron expression can be scheduled.
	Schedule func(spec string) error
	// RollingRestart reports whether the watched containers allow rolling restarts.
	RollingRestart func() error
}

// overrides is the persisted form: only the settings changed at run time.
type overrides struct {
	MonitorOnly    *bool   `json:"monitor_only,omitempty"`
	Cleanup        *bool   `json:"cleanup,omitempty"`
	RollingRestart *bool   `json:"rolling_restart,omitempty"`
	Schedule       *string `json:"schedule,omitempty"`
}

// Store holds the defaults from flags and the overrides layered on top.
// It is safe for concurrent use.
type Store struct {
	log        *zerolog.Logger
	validators Validators

	mu        sync.RWMutex
	defaults  Values
	overrides overrides
	path      string
	// scheduleChanges carries the latest schedule to the scheduler.
	scheduleChanges chan string
}

// Open creates a store with the given defaults and loads saved overrides from
// dataDir. An empty, missing, or unwritable dataDir yields a working store
// whose changes are not saved. Saved overrides that no longer validate are
// dropped with a warning rather than stopping the process.
//
// Parameters:
//   - log: Logger for load and save problems. Nil disables logging.
//   - dataDir: Directory holding settings.json, or empty for none.
//   - defaults: Values resolved from flags and environment variables.
//   - validators: Optional checks applied to loaded and changed settings.
//
// Returns:
//   - *Store: Store ready for use.
func Open(log *zerolog.Logger, dataDir string, defaults Values, validators Validators) *Store {
	if log == nil {
		nop := zerolog.Nop()
		log = &nop
	}

	store := &Store{
		log:             log,
		validators:      validators,
		defaults:        defaults,
		scheduleChanges: make(chan string, 1),
	}

	if !util.UsableDir(dataDir) {
		return store
	}

	store.path = filepath.Join(dataDir, FileName)
	store.load()

	return store
}

// Persistent reports whether changes are saved across restarts.
func (s *Store) Persistent() bool {
	return s.path != ""
}

// Defaults returns the values resolved from flags and environment variables.
func (s *Store) Defaults() Values {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.defaults
}

// Effective returns the values in force: defaults with overrides applied.
func (s *Store) Effective() Values {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.overrides.apply(s.defaults)
}

// Overridden reports, by setting name, which settings were changed at run
// time and so no longer follow the flags.
func (s *Store) Overridden() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]bool{
		KeyMonitorOnly:    s.overrides.MonitorOnly != nil,
		KeyCleanup:        s.overrides.Cleanup != nil,
		KeyRollingRestart: s.overrides.RollingRestart != nil,
		KeySchedule:       s.overrides.Schedule != nil,
	}
}

// ApplyTo returns params with the effective runtime settings applied.
// A nil store returns params unchanged.
func (s *Store) ApplyTo(params types.UpdateParams) types.UpdateParams {
	if s == nil {
		return params
	}

	values := s.Effective()
	params.MonitorOnly = values.MonitorOnly
	params.Cleanup = values.Cleanup
	params.RollingRestart = values.RollingRestart

	return params
}

// ScheduleChanges delivers the new cron expression each time the schedule
// changes. Only the most recent change is kept if the receiver is slow.
func (s *Store) ScheduleChanges() <-chan string {
	return s.scheduleChanges
}

// Update applies a set of changes atomically. Each key is a setting name and
// each value is its new JSON value, or JSON null to return the setting to its
// default. Nothing changes when any part of the request is invalid.
//
// Parameters:
//   - changes: Setting names mapped to raw JSON values.
//
// Returns:
//   - error: Non-nil if a name is unknown, a value has the wrong type, or the
//     resulting settings fail validation.
func (s *Store) Update(changes map[string]json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.overrides

	for key, raw := range changes {
		err := next.set(key, raw)
		if err != nil {
			return err
		}
	}

	before := s.overrides.apply(s.defaults)
	after := next.apply(s.defaults)

	err := s.validate(before, after)
	if err != nil {
		return err
	}

	s.overrides = next
	s.save()

	if after.Schedule != before.Schedule {
		s.announceSchedule(after.Schedule)
	}

	return nil
}

// validate checks a proposed set of effective values. Checks that depend on
// the outside world only run for the settings that are actually changing.
func (s *Store) validate(before, after Values) error {
	if after.RollingRestart && after.MonitorOnly {
		return ErrIncompatible
	}

	if after.Schedule != before.Schedule && after.Schedule != "" && s.validators.Schedule != nil {
		err := s.validators.Schedule(after.Schedule)
		if err != nil {
			return fmt.Errorf("%w: schedule: %w", ErrInvalidValue, err)
		}
	}

	if after.RollingRestart && !before.RollingRestart && s.validators.RollingRestart != nil {
		err := s.validators.RollingRestart()
		if err != nil {
			return fmt.Errorf("rolling restart is not possible: %w", err)
		}
	}

	return nil
}

// announceSchedule hands the new schedule to the scheduler, replacing any
// change it has not picked up yet.
func (s *Store) announceSchedule(spec string) {
	select {
	case <-s.scheduleChanges:
	default:
	}

	s.scheduleChanges <- spec
}

// load reads saved overrides, keeping them only if they still validate.
func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.log.Warn().Err(err).Str("path", s.path).Msg("Could not read saved settings, using defaults")
		}

		return
	}

	var saved overrides

	err = json.Unmarshal(data, &saved)
	if err != nil {
		s.log.Warn().Err(err).Str("path", s.path).Msg("Saved settings are not valid JSON, using defaults")

		return
	}

	if saved.Schedule != nil {
		trimmed := strings.TrimSpace(*saved.Schedule)
		saved.Schedule = &trimmed
	}

	err = s.validate(s.defaults, saved.apply(s.defaults))
	if err != nil {
		s.log.Warn().Err(err).Str("path", s.path).Msg("Saved settings no longer apply, using defaults")

		return
	}

	s.overrides = saved

	s.log.Debug().Str("path", s.path).Msg("Loaded saved settings")
}

// save writes the overrides to disk when a data directory is available.
func (s *Store) save() {
	if s.path == "" {
		return
	}

	data, err := json.MarshalIndent(s.overrides, "", "  ")
	if err != nil {
		s.log.Error().Err(err).Msg("Could not encode settings")

		return
	}

	err = util.WriteFileAtomic(s.path, append(data, '\n'))
	if err != nil {
		s.log.Error().Err(err).Str("path", s.path).Msg("Could not save settings")
	}
}

// apply returns defaults with these overrides layered on top.
func (o *overrides) apply(defaults Values) Values {
	values := defaults

	if o.MonitorOnly != nil {
		values.MonitorOnly = *o.MonitorOnly
	}

	if o.Cleanup != nil {
		values.Cleanup = *o.Cleanup
	}

	if o.RollingRestart != nil {
		values.RollingRestart = *o.RollingRestart
	}

	if o.Schedule != nil {
		values.Schedule = *o.Schedule
	}

	return values
}

// set records one change. JSON null clears the override.
func (o *overrides) set(key string, raw json.RawMessage) error {
	switch key {
	case KeyMonitorOnly:
		return decode(key, raw, &o.MonitorOnly)
	case KeyCleanup:
		return decode(key, raw, &o.Cleanup)
	case KeyRollingRestart:
		return decode(key, raw, &o.RollingRestart)
	case KeySchedule:
		err := decode(key, raw, &o.Schedule)
		if err == nil && o.Schedule != nil {
			trimmed := strings.TrimSpace(*o.Schedule)
			o.Schedule = &trimmed
		}

		return err
	default:
		return fmt.Errorf("%w: %s", ErrUnknownSetting, key)
	}
}

// decode parses raw into target, leaving target nil for JSON null.
func decode[T any](key string, raw json.RawMessage, target **T) error {
	if strings.TrimSpace(string(raw)) == "null" {
		*target = nil

		return nil
	}

	var value T

	err := json.Unmarshal(raw, &value)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidValue, key)
	}

	*target = &value

	return nil
}

package percontainer

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTimers records the entries a registry keeps.
type fakeTimers struct {
	mu      sync.Mutex
	next    cron.EntryID
	entries map[cron.EntryID]string
	fail    map[string]bool
	added   int
}

// newFakeTimers returns timers with no entries.
func newFakeTimers() *fakeTimers {
	return &fakeTimers{entries: map[cron.EntryID]string{}, fail: map[string]bool{}}
}

// Add records an entry for spec, or refuses one marked to fail.
func (f *fakeTimers) Add(spec string) (cron.EntryID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail[spec] {
		return 0, errors.New("refused")
	}

	f.next++
	f.added++
	f.entries[f.next] = spec

	return f.next, nil
}

// Remove takes an entry out.
func (f *fakeTimers) Remove(id cron.EntryID) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.entries, id)
}

// registrations is how many entries have been added in all (a re-added
// schedule counts again: its next run started over).
func (f *fakeTimers) registrations() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.added
}

// specs returns the schedules with an entry.
func (f *fakeTimers) specs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	specs := make([]string, 0, len(f.entries))
	for _, spec := range f.entries {
		specs = append(specs, spec)
	}

	return specs
}

// TestRegistryOneEntryPerSchedule verifies one entry per schedule in use, removed with its last container.
func TestRegistryOneEntryPerSchedule(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	r.Set("web", "", "@every 10m")
	r.Set("online", "", "@every 10m")
	r.Set("db", "", "@daily")

	assert.ElementsMatch(t, []string{"@every 10m", "@daily"}, timers.specs())
	assert.Equal(t, []string{"@daily", "@every 10m"}, r.Schedules())

	r.Remove("web", "")
	assert.ElementsMatch(t, []string{"@every 10m", "@daily"}, timers.specs(), "one container is still on it")

	r.Remove("online", "")
	assert.ElementsMatch(t, []string{"@daily"}, timers.specs(), "the last container left it")
}

// TestRegistryMovesAContainer verifies a container moving between schedules and back to the global one.
func TestRegistryMovesAContainer(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	r.Set("web", "", "@every 10m")
	r.Set("web", "", "@hourly")
	assert.ElementsMatch(t, []string{"@hourly"}, timers.specs())

	r.Set("web", "", "")
	assert.Empty(t, timers.specs(), "no schedule: back on the global one")

	r.Set("web", "", "@hourly")

	added := timers.registrations()

	r.Set("web", "", " '@hourly' ")
	assert.ElementsMatch(t, []string{"@hourly"}, timers.specs(), "the same schedule written differently is no change")
	assert.Equal(t, added, timers.registrations(), "and its entry is kept, not added again")
}

// TestRegistryIgnoresInvalidSchedules verifies that a schedule that doesn't parse gets no entry.
func TestRegistryIgnoresInvalidSchedules(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	r.Set("web", "", "every ten minutes")
	assert.Empty(t, timers.specs())

	r.Set("web", "", "@every 10m")
	r.Set("web", "", "nonsense")
	assert.Empty(t, timers.specs(), "changing to an invalid schedule drops the old one")
}

// TestRegistryWhenAddFails verifies that a schedule the timers refuse is tried again on the next report.
func TestRegistryWhenAddFails(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	timers.fail["@hourly"] = true
	r := NewRegistry(nil, timers)

	r.Set("web", "", "@hourly")
	assert.Empty(t, r.Schedules())

	timers.fail["@hourly"] = false

	r.Set("web", "", "@hourly")
	assert.Equal(t, []string{"@hourly"}, r.Schedules(), "a later report tries again")
}

// TestRegistryReset verifies that a snapshot replaces what was known.
func TestRegistryReset(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	r.Set("gone", "", "@daily")
	r.Set("kept", "", "@every 10m")

	r.Reset(map[string]Labeled{"kept": {Spec: "@every 10m"}, "new": {Spec: "@hourly"}})

	assert.ElementsMatch(t, []string{"@every 10m", "@hourly"}, timers.specs())

	r.Reset(map[string]Labeled{})
	assert.Empty(t, timers.specs())
}

// TestRegistryConcurrentUse verifies the registry under concurrent reports.
func TestRegistryConcurrentUse(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	var wg sync.WaitGroup

	for i := range 50 {
		wg.Go(func() {
			name := string(rune('a' + i%10))
			r.Set(name, "", "@every 10m")
			r.Remove(name, "")
			r.Set(name, "", "@hourly")
		})
	}

	wg.Wait()

	require.ElementsMatch(t, []string{"@hourly"}, timers.specs())
}

// TestRegistryResetKeepsSchedulesInUse verifies that a refresh never drops and re-adds a schedule still in use.
func TestRegistryResetKeepsSchedulesInUse(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	r.Reset(map[string]Labeled{"web": {ID: "a", Spec: "@every 10m"}, "db": {ID: "b", Spec: "@daily"}})

	added := timers.registrations()

	// The same containers again (a refresh after every run): nothing changes
	r.Reset(map[string]Labeled{"web": {ID: "a", Spec: "@every 10m"}, "db": {ID: "b", Spec: "@daily"}})
	assert.Equal(t, added, timers.registrations(), "a refresh keeps every entry and its next run")

	// The only container on a schedule replaced by another on it: the
	// schedule stays in use throughout, so its entry is kept
	r.Reset(map[string]Labeled{"web-2": {ID: "c", Spec: "@every 10m"}, "db": {ID: "b", Spec: "@daily"}})
	assert.Equal(t, added, timers.registrations(), "a schedule still in use is never dropped and added again")
	assert.ElementsMatch(t, []string{"@every 10m", "@daily"}, timers.specs())
}

// TestRegistryTellsReplacementsApart verifies that news of a replaced container doesn't touch its replacement.
func TestRegistryTellsReplacementsApart(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	// An update replaces a container under the same name; a refresh lists the
	// replacement before the old one's queued events arrive
	r.Reset(map[string]Labeled{"web": {ID: "new", Spec: "@every 10m"}})

	added := timers.registrations()

	r.Remove("web", "old")
	assert.Equal(t, []string{"@every 10m"}, r.Schedules(), "the old container's removal leaves the replacement")

	r.Set("web", "new", "@every 10m")
	assert.Equal(t, added, timers.registrations(), "the replacement's own news keeps its entry and next run")

	r.Remove("web", "new")
	assert.Empty(t, r.Schedules(), "its own removal does remove it")
}

// TestRegistryWarnsOnceAboutAnInvalidLabel verifies one warning per invalid label, however often it's read.
func TestRegistryWarnsOnceAboutAnInvalidLabel(t *testing.T) {
	t.Parallel()

	var buf strings.Builder

	logger := zerolog.New(&buf)
	r := NewRegistry(&logger, newFakeTimers())

	for range 3 {
		r.Reset(map[string]Labeled{"web": {Spec: "every ten minutes"}})
	}

	assert.Equal(t, 1, strings.Count(buf.String(), "Ignoring an invalid schedule label"), "read on every refresh, warned about once")

	r.Set("web", "", "@hourly")
	r.Set("web", "", "nonsense")
	assert.Equal(t, 2, strings.Count(buf.String(), "Ignoring an invalid schedule label"), "a new invalid value warns again")
}

// TestRegistryKeepsTimersThroughASwap verifies that two containers swapping
// schedules in one snapshot keep both schedules' entries.
func TestRegistryKeepsTimersThroughASwap(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	r.Reset(map[string]Labeled{"web": {ID: "a", Spec: "@every 10m"}, "db": {ID: "b", Spec: "@daily"}})

	added := timers.registrations()

	r.Reset(map[string]Labeled{"web": {ID: "a", Spec: "@daily"}, "db": {ID: "b", Spec: "@every 10m"}})
	assert.Equal(t, added, timers.registrations(), "both schedules stayed in use: neither entry was dropped")
	assert.ElementsMatch(t, []string{"@every 10m", "@daily"}, timers.specs())
}

// TestRegistryKeepsATimerThroughARename verifies that a container alone on
// its schedule keeps the schedule's entry when renamed (the new name is
// recorded before the old is removed).
func TestRegistryKeepsATimerThroughARename(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	r.Set("web", "a", "@every 10m")

	added := timers.registrations()

	r.Set("web-2", "a", "@every 10m")
	r.Remove("web", "a")

	assert.Equal(t, added, timers.registrations())
	assert.Equal(t, []string{"@every 10m"}, r.Schedules())
}

// TestRegistryKeepsAReplacementThroughReplayedEvents verifies that events of
// the container a replacement replaced, arriving after a snapshot that has the
// replacement, neither remove it nor restart its schedule.
func TestRegistryKeepsAReplacementThroughReplayedEvents(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	r.Reset(map[string]Labeled{"web": {ID: "new", Spec: "@every 10m"}})

	added := timers.registrations()

	// replayed: the old container started, then was removed; then the new one's own news
	r.Set("web", "old", "@every 10m")
	r.Remove("web", "old")
	r.Set("web", "new", "@every 10m")

	assert.Equal(t, []string{"@every 10m"}, r.Schedules(), "the replacement keeps its schedule")
	assert.Equal(t, added, timers.registrations(), "and its entry and next run")

	r.Remove("web", "new")
	assert.Empty(t, r.Schedules(), "its own removal does remove it")
}

// TestRegistryWarnsAboutAReplacementsInvalidLabel verifies that a replacement
// carrying the same invalid label as the container it replaced is warned
// about too.
func TestRegistryWarnsAboutAReplacementsInvalidLabel(t *testing.T) {
	t.Parallel()

	var buf strings.Builder

	logger := zerolog.New(&buf)
	r := NewRegistry(&logger, newFakeTimers())

	r.Reset(map[string]Labeled{"web": {ID: "a", Spec: "every ten minutes"}})
	r.Reset(map[string]Labeled{"web": {ID: "b", Spec: "every ten minutes"}})

	assert.Equal(t, 2, strings.Count(buf.String(), "Ignoring an invalid schedule label"))
}

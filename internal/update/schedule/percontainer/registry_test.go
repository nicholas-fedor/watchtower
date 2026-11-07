package percontainer

import (
	"errors"
	"sync"
	"testing"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTimers records the entries a registry keeps.
type fakeTimers struct {
	mu      sync.Mutex
	next    cron.EntryID
	entries map[cron.EntryID]string
	fail    map[string]bool
}

func newFakeTimers() *fakeTimers {
	return &fakeTimers{entries: map[cron.EntryID]string{}, fail: map[string]bool{}}
}

func (f *fakeTimers) Add(spec string) (cron.EntryID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail[spec] {
		return 0, errors.New("refused")
	}

	f.next++
	f.entries[f.next] = spec

	return f.next, nil
}

func (f *fakeTimers) Remove(id cron.EntryID) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.entries, id)
}

func (f *fakeTimers) specs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	specs := make([]string, 0, len(f.entries))
	for _, spec := range f.entries {
		specs = append(specs, spec)
	}

	return specs
}

func TestRegistryOneEntryPerSchedule(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	r.Set("web", "@every 10m")
	r.Set("online", "@every 10m")
	r.Set("db", "@daily")

	assert.ElementsMatch(t, []string{"@every 10m", "@daily"}, timers.specs())
	assert.Equal(t, []string{"@daily", "@every 10m"}, r.Schedules())

	r.Remove("web")
	assert.ElementsMatch(t, []string{"@every 10m", "@daily"}, timers.specs(), "one container is still on it")

	r.Remove("online")
	assert.ElementsMatch(t, []string{"@daily"}, timers.specs(), "the last container left it")
}

func TestRegistryMovesAContainer(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	r.Set("web", "@every 10m")
	r.Set("web", "@hourly")
	assert.ElementsMatch(t, []string{"@hourly"}, timers.specs())

	r.Set("web", "")
	assert.Empty(t, timers.specs(), "no schedule: back on the global one")

	r.Set("web", "@hourly")
	r.Set("web", " '@hourly' ")
	assert.ElementsMatch(t, []string{"@hourly"}, timers.specs(), "the same schedule written differently is no change")
}

func TestRegistryIgnoresInvalidSchedules(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	r.Set("web", "every ten minutes")
	assert.Empty(t, timers.specs())

	r.Set("web", "@every 10m")
	r.Set("web", "nonsense")
	assert.Empty(t, timers.specs(), "changing to an invalid schedule drops the old one")
}

func TestRegistryWhenAddFails(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	timers.fail["@hourly"] = true
	r := NewRegistry(nil, timers)

	r.Set("web", "@hourly")
	assert.Empty(t, r.Schedules())

	timers.fail["@hourly"] = false

	r.Set("web", "@hourly")
	assert.Equal(t, []string{"@hourly"}, r.Schedules(), "a later report tries again")
}

func TestRegistryReset(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	r.Set("gone", "@daily")
	r.Set("kept", "@every 10m")

	r.Reset(map[string]string{"kept": "@every 10m", "new": "@hourly"})

	assert.ElementsMatch(t, []string{"@every 10m", "@hourly"}, timers.specs())

	r.Reset(map[string]string{})
	assert.Empty(t, timers.specs())
}

func TestRegistryConcurrentUse(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	r := NewRegistry(nil, timers)

	var wg sync.WaitGroup

	for i := range 50 {
		wg.Go(func() {
			name := string(rune('a' + i%10))
			r.Set(name, "@every 10m")
			r.Remove(name)
			r.Set(name, "@hourly")
		})
	}

	wg.Wait()

	require.ElementsMatch(t, []string{"@hourly"}, timers.specs())
}

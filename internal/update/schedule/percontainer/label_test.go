package percontainer

import (
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
)

// labeled returns a container with this name and these labels.
func labeled(name string, containerLabels map[string]string) filters.FilterableContainer {
	attrs := map[string]string{"name": name, "image": "example/" + name + ":latest"}
	maps.Copy(attrs, containerLabels)

	return eventContainer(name, "id-"+name, attrs)
}

// all is a filter accepting every container.
func all(filters.FilterableContainer) bool { return true }

// TestSpec verifies how a schedule label is read: normalized, and only when it parses.
func TestSpec(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		label *string
		want  string
		ok    bool
	}{
		{name: "no label", label: nil},
		{name: "cron", label: new("*/10 * * * *"), want: "*/10 * * * *", ok: true},
		{name: "descriptor", label: new("@every 10m"), want: "@every 10m", ok: true},
		{name: "with seconds", label: new("0 */10 * * * *"), want: "0 */10 * * * *", ok: true},
		{name: "quoted and padded", label: new(` "@hourly" `), want: "@hourly", ok: true},
		{name: "empty", label: new("")},
		{name: "invalid", label: new("every ten minutes")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			containerLabels := map[string]string{}
			if tc.label != nil {
				containerLabels[labels.Schedule] = *tc.label
			}

			got, ok := Spec(labeled("app", containerLabels))
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestWithoutOwnSchedule verifies that the global filter leaves out containers with their own schedule.
func TestWithoutOwnSchedule(t *testing.T) {
	t.Parallel()

	filter := WithoutOwnSchedule(all)

	assert.True(t, filter(labeled("plain", nil)), "a container without a schedule follows the global one")
	assert.False(t, filter(labeled("own", map[string]string{labels.Schedule: "@every 10m"})),
		"a container with its own schedule leaves the global one")
	assert.True(t, filter(labeled("broken", map[string]string{labels.Schedule: "sometimes"})),
		"a container whose schedule does not parse stays on the global one")

	none := WithoutOwnSchedule(func(filters.FilterableContainer) bool { return false })
	assert.False(t, none(labeled("plain", nil)), "the base filter still applies")
}

// TestOnSchedule verifies that a container schedule's filter covers only its containers.
func TestOnSchedule(t *testing.T) {
	t.Parallel()

	filter := OnSchedule("@every 10m", all)

	assert.True(t, filter(labeled("games", map[string]string{labels.Schedule: "@every 10m"})))
	assert.True(t, filter(labeled("quoted", map[string]string{labels.Schedule: `'@every 10m'`})))
	assert.False(t, filter(labeled("hourly", map[string]string{labels.Schedule: "@hourly"})))
	assert.False(t, filter(labeled("plain", nil)))

	none := OnSchedule("@every 10m", func(filters.FilterableContainer) bool { return false })
	assert.False(t, none(labeled("games", map[string]string{labels.Schedule: "@every 10m"})), "the base filter still applies")
}

// TestNext verifies the next run of a schedule.
func TestNext(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 7, 10, 3, 0, 0, time.UTC)

	assert.Equal(t, at.Add(10*time.Minute), Next("@every 10m", at))
	assert.Equal(t, time.Date(2026, 11, 7, 11, 0, 0, 0, time.UTC), Next(`"@hourly"`, at))
	assert.True(t, Next("nonsense", at).IsZero())
}

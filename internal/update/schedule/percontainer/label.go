package percontainer

import (
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
)

// ParseOptions is the cron syntax schedules are written in: the global
// --schedule's and the schedule label's alike.
const ParseOptions = cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor

var parser = cron.NewParser(ParseOptions)

// Normalize returns a schedule as it is matched: trimmed of spaces and of the
// quotes a compose file or shell can leave around it.
func Normalize(spec string) string {
	return strings.Trim(strings.TrimSpace(spec), `"'`)
}

// Valid reports whether a schedule parses.
func Valid(spec string) bool {
	if spec == "" {
		return false
	}

	_, err := parser.Parse(spec)

	return err == nil
}

// Next returns when a schedule next runs after a time (the zero time if it
// does not parse).
func Next(spec string, after time.Time) time.Time {
	schedule, err := parser.Parse(Normalize(spec))
	if err != nil {
		return time.Time{}
	}

	return schedule.Next(after)
}

// Spec returns the container's own schedule: its schedule label, normalized,
// when the label is set and valid. A container whose label does not parse
// follows the global schedule.
func Spec(c filters.FilterableContainer) (string, bool) {
	raw, ok := c.GetLabel(labels.Schedule)
	if !ok {
		return "", false
	}

	spec := Normalize(raw)
	if !Valid(spec) {
		return "", false
	}

	return spec, true
}

// WithoutOwnSchedule narrows a filter to the containers that follow the global
// schedule: those without a valid schedule label.
func WithoutOwnSchedule(base filters.Filter) filters.Filter {
	return func(c filters.FilterableContainer) bool {
		if _, own := Spec(c); own {
			return false
		}

		return base(c)
	}
}

// OnSchedule narrows a filter to the containers whose own schedule is spec.
func OnSchedule(spec string, base filters.Filter) filters.Filter {
	spec = Normalize(spec)

	return func(c filters.FilterableContainer) bool {
		own, ok := Spec(c)
		if !ok || own != spec {
			return false
		}

		return base(c)
	}
}

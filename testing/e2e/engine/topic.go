package engine

import (
	"fmt"
	"regexp"
	"slices"
)

// topicAndFilter is the max number of regexes CompileFilters returns (topic plus --filter).
const topicAndFilter = 2

// Topic is a named slice of the product for working on one Watchtower area.
type Topic struct {
	// Name is the --topic value.
	Name string
	// Summary is what this slice is for.
	Summary string
	// Filter is the regex applied to case IDs, factor names, and factor values.
	Filter string
}

// Topics returns the development slices in stable name order.
//
// Returns:
//   - []Topic: Named recipes for --topic.
func Topics() []Topic {
	topics := []Topic{
		{
			Name:    "ratelimit",
			Summary: "Hub and GHCR/LSCR 429 pacing and quota bodies",
			Filter:  `429-hub|429-ghcr|lscr`,
		},
		{
			Name:    "registry",
			Summary: "Fake Hub, GHCR, LSCR, and private registry personas",
			Filter:  `registry.persona`,
		},
		{
			Name:    "disk",
			Summary: "disk-space-max and disk-space-warn gates",
			Filter:  `disk-space`,
		},
		{
			Name:    "cleanup",
			Summary: "Image removal after updates, including self-update",
			Filter:  `flag.cleanup|ephemeral-self-update`,
		},
		{
			Name:    "self-update",
			Summary: "Watchtower replacing itself",
			Filter:  `ephemeral-self-update|self-update-orchestrator|^self$`,
		},
		{
			Name:    "filters",
			Summary: "Name, label, monitor, skip, and disable selection",
			Filter:  `filter.stack|skip-image|disable-containers|label-enable|monitor-image`,
		},
		{
			Name:    "http-api",
			Summary: "HTTP update API, endpoints, and tokens",
			Filter:  `http-update|api.endpoints|http-api`,
		},
		{
			Name:    "lifecycle",
			Summary: "Pre/post check and update hooks",
			Filter:  `lifecycle`,
		},
		{
			Name:    "stop",
			Summary: "Stop timeout, deaf SIGTERM, and custom stop signals",
			Filter:  `slow-term|deaf-term|custom-signal|stop-timeout`,
		},
		{
			Name:    "depends",
			Summary: "Depends-on chains, cycles, and rolling restart",
			Filter:  `graph|rolling-restart|compose-depends`,
		},
		{
			Name:    "notify",
			Summary: "Notification URLs and webhook sinks",
			Filter:  `notify.sink|notification`,
		},
		{
			Name:    "porcelain",
			Summary: "JSON porcelain session output",
			Filter:  `porcelain`,
		},
		{
			Name:    "secrets",
			Summary: "Secret files and token leak checks",
			Filter:  `secret-file|http-api-token`,
		},
		{
			Name:    "schedule",
			Summary: "Run-once, interval, cron, and HTTP poll shapes",
			Filter:  `process.shape`,
		},
	}

	slices.SortFunc(topics, func(left, right Topic) int {
		if left.Name < right.Name {
			return -1
		}

		if left.Name > right.Name {
			return 1
		}

		return 0
	})

	return topics
}

// LookupTopic finds a topic by name.
//
// Parameters:
//   - name: --topic value.
//
// Returns:
//   - Topic: Matching recipe.
//   - error: ErrUnknownTopic when name is empty or unknown.
func LookupTopic(name string) (Topic, error) {
	if name == "" {
		return Topic{}, ErrUnknownTopic
	}

	for _, topic := range Topics() {
		if topic.Name == name {
			return topic, nil
		}
	}

	return Topic{}, fmt.Errorf("%w: %s", ErrUnknownTopic, name)
}

// CompileFilters builds AND-ed regexes from an optional topic and optional extra filter.
//
// Parameters:
//   - topicName: --topic value, or empty.
//   - extra: --filter value, or empty.
//
// Returns:
//   - []*regexp.Regexp: Patterns a case must match (all of them).
//   - error: Unknown topic or invalid regex.
func CompileFilters(topicName, extra string) ([]*regexp.Regexp, error) {
	patterns := make([]string, 0, topicAndFilter)

	if topicName != "" {
		topic, err := LookupTopic(topicName)
		if err != nil {
			return nil, err
		}

		patterns = append(patterns, topic.Filter)
	}

	if extra != "" {
		patterns = append(patterns, extra)
	}

	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}

		compiled = append(compiled, re)
	}

	return compiled, nil
}

package filters

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func fuzzLog() *zerolog.Logger {
	n := zerolog.Nop()

	return &n
}

// fuzzableContainer is a minimal FilterableContainer implementation for fuzzing
// filter functions. It stores labels in a map and returns fixed values for
// other fields.
type fuzzableContainer struct {
	name       string
	imageName  string
	enabled    bool
	scope      string
	hasScope   bool
	labels     map[string]string
	watchtower bool
}

func newFuzzableContainer(name, imageName string, enabled bool, scope string, hasScope bool, containerLabels map[string]string) fuzzableContainer {
	if containerLabels == nil {
		containerLabels = make(map[string]string)
	}

	return fuzzableContainer{
		name:       name,
		imageName:  imageName,
		enabled:    enabled,
		scope:      scope,
		hasScope:   hasScope,
		labels:     containerLabels,
		watchtower: false,
	}
}

func (c fuzzableContainer) Name() string {
	return c.name
}

func (c fuzzableContainer) ImageName() string {
	return c.imageName
}

func (c fuzzableContainer) Enabled() (bool, bool) {
	return c.enabled, true
}

func (c fuzzableContainer) Scope() (string, bool) {
	return c.scope, c.hasScope
}

// NormalizedScope returns the scope, or "none" when it is unset or empty, as
// the container model does.
func (c fuzzableContainer) NormalizedScope() string {
	scope := ""
	if c.hasScope {
		scope = c.scope
	}

	return labels.NormalizeScope(scope)
}

func (c fuzzableContainer) GetLabel(key string) (string, bool) {
	value, ok := c.labels[key]

	return value, ok
}

func (c fuzzableContainer) IsWatchtower() bool {
	return c.watchtower
}

var _ types.FilterableContainer = fuzzableContainer{}

// labelMap turns parsed label pairs into a container's labels. A container
// holds one value per key, so a later pair replaces an earlier one.
func labelMap(pairs []labelPair) map[string]string {
	containerLabels := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		containerLabels[pair.key] = pair.value
	}

	return containerLabels
}

func FuzzParseLabelPairs(f *testing.F) {
	f.Add("key=value")
	f.Add("key=")
	f.Add("  key  =  value  ")
	f.Add("key=value=extra")
	f.Add("nokey")
	f.Add("=value")
	f.Add("")

	f.Fuzz(func(t *testing.T, input string) {
		pairs, err := parseLabelPairs([]string{input})
		if err != nil {
			return
		}

		for _, pair := range pairs {
			if pair.key == "" || pair.key != strings.TrimSpace(pair.key) {
				t.Fatalf("key %q from %q is empty or untrimmed", pair.key, input)
			}

			if len(pair.key)+len(pair.value) > maxLabelPairBytes {
				t.Fatalf("pair from %q exceeds %d bytes", input, maxLabelPairBytes)
			}
		}
	})
}

func FuzzFilterByEnabledLabels(f *testing.F) {
	f.Add("app", "nginx:latest", "com.example.enabled=true", "com.example.enabled=true")
	f.Add("web", "redis:alpine", "com.example.env=prod", "")
	f.Add("db", "postgres:15", "com.example.env=prod", "com.example.env=staging")
	f.Add("app", "nginx:latest", "", "com.example.enabled=true")

	f.Fuzz(func(t *testing.T, containerName, imageName, filterLabelPair, containerLabelPair string) {
		filterLabels, err := parseLabelPairs([]string{filterLabelPair})
		if err != nil {
			return
		}

		if len(filterLabels) == 0 {
			return
		}

		containerLabels, err := parseLabelPairs([]string{containerLabelPair})
		if err != nil {
			return
		}

		c := newFuzzableContainer(containerName, imageName, true, "", false, labelMap(containerLabels))
		filter := filterByEnabledLabels(fuzzLog(), filterLabels, NoFilter)
		_ = filter(c)
	})
}

func FuzzFilterByDisabledLabels(f *testing.F) {
	f.Add("app", "nginx:latest", "com.example.disabled=true", "com.example.disabled=true")
	f.Add("web", "redis:alpine", "com.example.env=prod", "")
	f.Add("db", "postgres:15", "com.example.env=prod", "com.example.env=staging")
	f.Add("app", "nginx:latest", "", "com.example.disabled=true")

	f.Fuzz(func(t *testing.T, containerName, imageName, filterLabelPair, containerLabelPair string) {
		filterLabels, err := parseLabelPairs([]string{filterLabelPair})
		if err != nil {
			return
		}

		if len(filterLabels) == 0 {
			return
		}

		containerLabels, err := parseLabelPairs([]string{containerLabelPair})
		if err != nil {
			return
		}

		c := newFuzzableContainer(containerName, imageName, true, "", false, labelMap(containerLabels))
		filter := filterByDisabledLabels(fuzzLog(), filterLabels, NoFilter)
		_ = filter(c)
	})
}

func FuzzMatchImageAndTag(f *testing.F) {
	f.Add("nginx:latest", "nginx")
	f.Add("redis:alpine", "redis:alpine")
	f.Add("myapp:1.0", "myapp:1.0")
	f.Add("registry:5000/app:v1", "registry:5000/app:v1")
	f.Add("postgres:15", "postgres")

	f.Fuzz(func(t *testing.T, containerImage, targetImage string) {
		_ = matchImageAndTag(containerImage, targetImage)
	})
}

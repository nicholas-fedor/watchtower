package filters

import (
	"testing"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

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

func newFuzzableContainer(name, imageName string, enabled bool, scope string, hasScope bool, labels map[string]string) fuzzableContainer {
	if labels == nil {
		labels = make(map[string]string)
	}

	return fuzzableContainer{
		name:       name,
		imageName:  imageName,
		enabled:    enabled,
		scope:      scope,
		hasScope:   hasScope,
		labels:     labels,
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

func (c fuzzableContainer) GetLabel(key string) (string, bool) {
	value, ok := c.labels[key]

	return value, ok
}

func (c fuzzableContainer) IsWatchtower() bool {
	return c.watchtower
}

var _ types.FilterableContainer = fuzzableContainer{}

func FuzzParseLabelPairs(f *testing.F) {
	f.Add("key=value")
	f.Add("key=")
	f.Add("  key  =  value  ")
	f.Add("key=value=extra")
	f.Add("nokey")
	f.Add("=value")
	f.Add("")

	f.Fuzz(func(t *testing.T, input string) {
		_, err := parseLabelPairs([]string{input})
		if err != nil {
			return
		}
	})
}

func FuzzMatchesName(f *testing.F) {
	f.Add("test-container", "test")
	f.Add("nginx-proxy-1", "nginx.*")
	f.Add("watchtower", "watchtower")
	f.Add("app", "^[a-z]+$")
	f.Add("container", "")

	f.Fuzz(func(t *testing.T, containerName, pattern string) {
		_ = matchesName(containerName, pattern)
	})
}

func FuzzMatchesImageName(f *testing.F) {
	f.Add("nginx:latest", "nginx:.*")
	f.Add("redis:alpine", "redis:.*")
	f.Add("myapp:1.0", "myapp")
	f.Add("registry:5000/app:v1", "registry:5000/app:.*")

	f.Fuzz(func(t *testing.T, imageName, pattern string) {
		_ = matchesImageName(imageName, pattern)
	})
}

func FuzzFilterByEnabledLabels(f *testing.F) {
	f.Add("app", "nginx:latest", "com.example.enabled=true")
	f.Add("web", "redis:alpine", "com.example.env=prod")
	f.Add("db", "postgres:15", "")

	f.Fuzz(func(t *testing.T, containerName, imageName, labelPair string) {
		labels, err := parseLabelPairs([]string{labelPair})
		if err != nil {
			return
		}

		if len(labels) == 0 {
			return
		}

		c := newFuzzableContainer(containerName, imageName, true, "", false, labels)
		filter := FilterByEnabledLabels(labels, NoFilter)
		_ = filter(c)
	})
}

func FuzzFilterByDisabledLabels(f *testing.F) {
	f.Add("app", "nginx:latest", "com.example.disabled=true")
	f.Add("web", "redis:alpine", "com.example.env=prod")
	f.Add("db", "postgres:15", "")

	f.Fuzz(func(t *testing.T, containerName, imageName, labelPair string) {
		labels, err := parseLabelPairs([]string{labelPair})
		if err != nil {
			return
		}

		if len(labels) == 0 {
			return
		}

		c := newFuzzableContainer(containerName, imageName, true, "", false, labels)
		filter := FilterByDisabledLabels(labels, NoFilter)
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

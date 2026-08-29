package engine

import (
	"fmt"
	"os"
	"time"

	"go.yaml.in/yaml/v3"
)

// fileCase is the YAML document shape for named regressions under testdata/cases/.
type fileCase struct {
	ID          string            `yaml:"id"`
	Packaging   Packaging         `yaml:"packaging"`
	Channel     ConfigChannel     `yaml:"channel"`
	Shape       ProcessShape      `yaml:"shape"`
	ImageSource string            `yaml:"image_source"`
	Watchtower  WatchtowerConfig  `yaml:"watchtower"`
	Topology    Topology          `yaml:"topology"`
	Expect      Expect            `yaml:"expect"`
	Factors     map[string]string `yaml:"factors"`
	Names       []string          `yaml:"names"`
}

// UnmarshalYAML decodes WatchtowerConfig, parsing duration strings that YAML cannot bind to time.Duration.
//
// Parameters:
//   - value: YAML mapping node.
//
// Returns:
//   - error: Decode failure.
func (c *WatchtowerConfig) UnmarshalYAML(value *yaml.Node) error {
	type plain WatchtowerConfig

	aux := struct {
		plain `yaml:",inline"`

		StopTimeout          *string `yaml:"stop_timeout"`
		HTTPAPICheckTimeout  *string `yaml:"http_api_check_timeout"`
		HTTPAPIUpdateTimeout *string `yaml:"http_api_update_timeout"`
	}{}

	decodeErr := value.Decode(&aux)
	if decodeErr != nil {
		return fmt.Errorf("watchtower yaml: %w", decodeErr)
	}

	*c = WatchtowerConfig(aux.plain)
	c.StopTimeout = parseOptionalDuration(aux.StopTimeout)
	c.HTTPAPICheckTimeout = parseOptionalDuration(aux.HTTPAPICheckTimeout)
	c.HTTPAPIUpdateTimeout = parseOptionalDuration(aux.HTTPAPIUpdateTimeout)

	return nil
}

// UnmarshalYAML decodes Topology, parsing duration strings for image age and net delay.
//
// Parameters:
//   - value: YAML mapping node.
//
// Returns:
//   - error: Decode failure.
func (t *Topology) UnmarshalYAML(value *yaml.Node) error {
	type plain Topology

	aux := struct {
		plain `yaml:",inline"`

		ImageCreatedAge string `yaml:"image_created_age"`
		NetDelay        string `yaml:"net_delay"`
	}{}

	decodeErr := value.Decode(&aux)
	if decodeErr != nil {
		return fmt.Errorf("topology yaml: %w", decodeErr)
	}

	*t = Topology(aux.plain)
	t.ImageCreatedAge = parseDurationValue(aux.ImageCreatedAge)
	t.NetDelay = parseDurationValue(aux.NetDelay)

	return nil
}

// UnmarshalYAML decodes Expect, parsing timeout_at_least as a Go duration string.
//
// Parameters:
//   - value: YAML mapping node.
//
// Returns:
//   - error: Decode failure.
func (e *Expect) UnmarshalYAML(value *yaml.Node) error {
	type plain Expect

	aux := struct {
		plain `yaml:",inline"`

		TimeoutAtLeast string `yaml:"timeout_at_least"`
	}{}

	decodeErr := value.Decode(&aux)
	if decodeErr != nil {
		return fmt.Errorf("expect yaml: %w", decodeErr)
	}

	*e = Expect(aux.plain)
	e.TimeoutAtLeast = parseDurationValue(aux.TimeoutAtLeast)

	return nil
}

// LoadFile reads named YAML cases. It does not replace Product.
//
// Parameters:
//   - path: Filesystem path to a YAML document or array of documents.
//
// Returns:
//   - []Case: Parsed cases with IDs assigned.
//   - error: Filesystem or YAML failure.
func LoadFile(path string) ([]Case, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read case file: %w", err)
	}

	var many []fileCase

	unmarshalErr := yaml.Unmarshal(raw, &many)
	if unmarshalErr != nil {
		var one fileCase

		oneErr := yaml.Unmarshal(raw, &one)
		if oneErr != nil {
			return nil, fmt.Errorf("parse case file: %w", unmarshalErr)
		}

		many = []fileCase{one}
	}

	out := make([]Case, 0, len(many))
	for _, parsed := range many {
		out = append(out, parsed.toCase())
	}

	return out, nil
}

// toCase converts YAML into a runtime Case, applying defaults and DeriveExpect.
//
// Returns:
//   - Case: Runtime vector.
func (parsed fileCase) toCase() Case {
	item := Case{
		Factors:     parsed.Factors,
		Packaging:   parsed.Packaging,
		Channel:     parsed.Channel,
		Shape:       parsed.Shape,
		ImageSource: parsed.ImageSource,
		Names:       parsed.Names,
		Watchtower:  parsed.Watchtower,
		Topology:    parsed.Topology,
	}
	if item.Factors == nil {
		item.Factors = map[string]string{}
	}

	if item.Packaging == "" {
		item.Packaging = PackagingContainer
	}

	if item.Channel == "" {
		item.Channel = ChannelEnv
	}

	if item.Shape == "" {
		item.Shape = ShapeRunOnce
	}

	item.Expect = DeriveExpect(item)
	if parsed.Expect.Outcome != "" {
		item.Expect = parsed.Expect
	}

	if parsed.ID != "" {
		item.id = parsed.ID
	} else {
		item.AssignID()
	}

	return item
}

// parseOptionalDuration parses a Go duration string pointer.
//
// Parameters:
//   - raw: Duration such as 30s, or nil.
//
// Returns:
//   - *time.Duration: Parsed duration, or nil.
func parseOptionalDuration(raw *string) *time.Duration {
	if raw == nil {
		return nil
	}

	parsed, err := time.ParseDuration(*raw)
	if err != nil {
		return nil
	}

	return new(parsed)
}

// parseDurationValue parses a Go duration string, or returns zero.
//
// Parameters:
//   - raw: Duration such as 24h, or empty.
//
// Returns:
//   - time.Duration: Parsed duration, or 0.
func parseDurationValue(raw string) time.Duration {
	if raw == "" {
		return 0
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0
	}

	return parsed
}

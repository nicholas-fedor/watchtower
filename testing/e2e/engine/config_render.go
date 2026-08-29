package engine

import (
	"fmt"
	"sort"
	"strings"
)

// Args renders set fields as CLI arguments. Unset fields are omitted.
//
// Returns:
//   - []string: Argv fragment (no binary name).
func (c *WatchtowerConfig) Args() []string {
	args := make([]string, 0)

	for _, spec := range specs() {
		set, value := spec.get(*c)
		if !set {
			continue
		}

		switch spec.kind {
		case renderBool:
			if value == "true" {
				args = append(args, "--"+spec.flag)

				continue
			}

			args = append(args, "--"+spec.flag+"=false")
		case renderSlice:
			args = append(args, "--"+spec.flag, value)
		case renderScalar:
			args = append(args, "--"+spec.flag, value)
		}
	}

	return args
}

// Env renders set fields as environment variables. Unset fields are omitted.
//
// Returns:
//   - map[string]string: Environment map using Watchtower / Docker env keys.
func (c *WatchtowerConfig) Env() map[string]string {
	env := make(map[string]string)
	for _, spec := range specs() {
		if spec.env == "" {
			continue
		}

		set, value := spec.get(*c)
		if !set {
			continue
		}

		env[spec.env] = value
	}

	return env
}

// Render returns argv and env according to the config channel.
//
// Mixed mode partitions by flag name: even-hash fields go to argv, odd to env.
// Secret-file mode leaves token-like fields as paths for the runner to fill.
//
// Parameters:
//   - channel: How to deliver the vector.
//
// Returns:
//   - []string: CLI arguments (possibly empty).
//   - map[string]string: Environment map (possibly empty).
func (c *WatchtowerConfig) Render(channel ConfigChannel) ([]string, map[string]string) {
	switch channel {
	case ChannelEnv, ChannelSecretFile:
		return nil, c.Env()
	case ChannelMixed:
		return c.renderMixed()
	case ChannelFlags:
		return c.Args(), nil
	default:
		return c.Args(), nil
	}
}

// ApplyObservability sets trace JSON logging unless the case already tests logs.
//
// This is harness instrumentation, not a Watchtower-under-test dimension.
func (c *WatchtowerConfig) ApplyObservability() {
	if c.LogFormat == nil {
		c.LogFormat = new("json")
	}

	if c.LogLevel == nil && (c.Debug == nil || !*c.Debug) && (c.Trace == nil || !*c.Trace) {
		c.LogLevel = new("trace")
	}

	if c.NoColor == nil {
		c.NoColor = new(true)
	}
}

// SecretValues returns configured tokens and passwords for leak assertions.
//
// Returns:
//   - []string: Non-empty secret strings from the vector.
func (c *WatchtowerConfig) SecretValues() []string {
	secrets := make([]string, 0)
	add := func(ptr *string) {
		if ptr != nil && *ptr != "" {
			secrets = append(secrets, *ptr)
		}
	}

	add(c.HTTPAPIToken)
	add(c.HTTPAPIEventsToken)
	add(c.NotificationEmailPassword)
	add(c.NotificationGotifyToken)
	add(c.NotificationSlackHookURL)

	if c.NotificationURL != nil {
		secrets = append(secrets, *c.NotificationURL...)
	}

	return secrets
}

// renderMixed splits fields across argv and env with a stable hash partition.
//
// Returns:
//   - []string: CLI arguments for even-partition fields.
//   - map[string]string: Environment for odd-partition fields.
func (c *WatchtowerConfig) renderMixed() ([]string, map[string]string) {
	args := make([]string, 0)
	env := make(map[string]string)

	for _, spec := range specs() {
		set, value := spec.get(*c)
		if !set {
			continue
		}

		useEnv := spec.env != "" && mixedToEnv(spec.flag)
		if useEnv {
			env[spec.env] = value

			continue
		}

		switch spec.kind {
		case renderBool:
			if value == "true" {
				args = append(args, "--"+spec.flag)

				continue
			}

			args = append(args, "--"+spec.flag+"=false")
		case renderSlice, renderScalar:
			args = append(args, "--"+spec.flag, value)
		}
	}

	return args, env
}

// mixedToEnv is true when a flag name's first byte is odd (stable 50/50 split).
//
// Parameters:
//   - flag: Long flag name.
//
// Returns:
//   - bool: True when the field should use environment in mixed channel.
func mixedToEnv(flag string) bool {
	if flag == "" {
		return false
	}

	return flag[0]%2 == 1
}

// FormatArgv joins argv for artifact meta.
//
// Parameters:
//   - args: CLI arguments.
//
// Returns:
//   - string: Space-joined argv.
func FormatArgv(args []string) string {
	return strings.Join(args, " ")
}

// FormatEnv formats an environment map as KEY=value lines.
//
// Parameters:
//   - env: Environment map.
//
// Returns:
//   - string: Stable sorted KEY=value block.
func FormatEnv(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", key, env[key]))
	}

	return strings.Join(lines, "\n")
}

package container

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"
	"time"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/settings/units"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for label and scope operations.
var (
	// errCurrentContainerNotCached indicates the current container is not cached for scope derivation.
	errCurrentContainerNotCached = errors.New("current container not cached for scope derivation")
	// errLabelNotFound indicates a requested label is not present in the container's metadata.
	errLabelNotFound = errors.New("label not found")
)

// GetLifecyclePreCheckCommand returns the pre-check command from labels.
//
// Returns:
//   - string: Pre-check command or empty if unset.
func (c *Container) GetLifecyclePreCheckCommand() string {
	return c.getLabelValueOrEmpty(labels.LifecyclePreCheck)
}

// GetLifecyclePostCheckCommand returns the post-check command from labels.
//
// Returns:
//   - string: Post-check command or empty if unset.
func (c *Container) GetLifecyclePostCheckCommand() string {
	return c.getLabelValueOrEmpty(labels.LifecyclePostCheck)
}

// GetLifecyclePreUpdateCommand returns the pre-update command from labels.
//
// Returns:
//   - string: Pre-update command or empty if unset.
func (c *Container) GetLifecyclePreUpdateCommand() string {
	return c.getLabelValueOrEmpty(labels.LifecyclePreUpdate)
}

// GetLifecyclePostUpdateCommand returns the post-update command from labels.
//
// Returns:
//   - string: Post-update command or empty if unset.
func (c *Container) GetLifecyclePostUpdateCommand() string {
	return c.getLabelValueOrEmpty(labels.LifecyclePostUpdate)
}

// PreUpdateTimeout returns the pre-update command timeout in minutes.
//
// It defaults to 1 minute if unset or invalid.
// 0 allows indefinite execution.
//
// Returns:
//   - int: Timeout in minutes.
func (c *Container) PreUpdateTimeout() int {
	clogVal := c.Logger().With().
		Str("container", c.Name()).
		Logger()
	clog := &clogVal
	val := c.getLabelValueOrEmpty(labels.LifecyclePreUpdateTimeout)

	// Use default if label is unset.
	if val == "" {
		clog.Debug().
			Str("label", labels.LifecyclePreUpdateTimeout).
			Msg("Pre-update timeout not set, using default")

		return 1
	}

	// Parse timeout value.
	minutes, err := strconv.Atoi(val)
	if err != nil {
		clog.Warn().
			Err(err).
			Str("label", labels.LifecyclePreUpdateTimeout).
			Str("value", val).
			Msg("Invalid pre-update timeout value, using default")

		return 1
	}

	clog.Debug().
		Str("label", labels.LifecyclePreUpdateTimeout).
		Int("minutes", minutes).
		Msg("Retrieved pre-update timeout")

	return minutes
}

// PostUpdateTimeout returns the post-update command timeout in minutes.
//
// It defaults to 1 minute if unset or invalid.
// 0 allows indefinite execution.
//
// Returns:
//   - int: Timeout in minutes.
func (c *Container) PostUpdateTimeout() int {
	clogVal := c.Logger().With().
		Str("container", c.Name()).
		Logger()
	clog := &clogVal
	val := c.getLabelValueOrEmpty(labels.LifecyclePostUpdateTimeout)

	// Use default if label is unset.
	if val == "" {
		clog.Debug().
			Str("label", labels.LifecyclePostUpdateTimeout).
			Msg("Post-update timeout not set, using default")

		return 1
	}

	// Parse timeout value.
	minutes, err := strconv.Atoi(val)
	if err != nil {
		clog.Warn().
			Err(err).
			Str("label", labels.LifecyclePostUpdateTimeout).
			Str("value", val).
			Msg("Invalid post-update timeout value, using default")

		return 1
	}

	clog.Debug().
		Str("label", labels.LifecyclePostUpdateTimeout).
		Int("minutes", minutes).
		Msg("Retrieved post-update timeout")

	return minutes
}

// getLifecycleID parses and validates a lifecycle ID (UID or GID) from labels.
//
// Parameters:
//   - label: The label key to retrieve the value from.
//   - idType: The type of ID ("UID" or "GID") for logging purposes.
//
// Returns:
//   - int: ID value if set and valid.
//   - bool: True if label is present and valid, false otherwise.
func (c *Container) getLifecycleID(label, idType string) (int, bool) {
	clogVal := c.Logger().With().
		Str("container", c.Name()).
		Logger()
	clog := &clogVal
	rawString, ok := c.getLabelValue(label)

	if !ok {
		clog.Debug().
			Str("label", label).
			Msg(fmt.Sprintf("Lifecycle %s label not set", idType))

		return 0, false
	}

	// Parse ID value.
	parsedID, err := strconv.Atoi(rawString)
	if err != nil {
		clog.Warn().
			Err(err).
			Str("label", label).
			Str("value", rawString).
			Msg(fmt.Sprintf("Invalid lifecycle %s value: not a valid integer", idType))

		return 0, false
	}

	// Validate ID range (must be non-negative and within reasonable bounds).
	if parsedID < 0 {
		clog.Warn().
			Str("label", label).
			Str("value", rawString).
			Str("id_type", idType).
			Int("id", parsedID).
			Msg(fmt.Sprintf("Invalid lifecycle %s value: must be non-negative", idType))

		return 0, false
	}

	// Check for unreasonably large ID values (greater than 2^31-1).
	const maxReasonableID = 2147483647 // 2^31-1
	if parsedID > maxReasonableID {
		clog.Warn().
			Str("label", label).
			Str("value", rawString).
			Str("id_type", idType).
			Int("id", parsedID).
			Int("max", maxReasonableID).
			Msg(fmt.Sprintf("Invalid lifecycle %s value: exceeds maximum reasonable value", idType))

		return 0, false
	}

	clog.Debug().
		Str("label", label).
		Str("id_type", idType).
		Int("id", parsedID).
		Msg("Retrieved lifecycle " + idType)

	return parsedID, true
}

// GetLifecycleUID returns the UID for lifecycle hooks from labels.
//
// Returns:
//   - int: UID value if set and valid.
//   - bool: True if label is present and valid, false otherwise.
func (c *Container) GetLifecycleUID() (int, bool) {
	return c.getLifecycleID(labels.LifecycleUID, "UID")
}

// GetLifecycleGID returns the GID for lifecycle hooks from labels.
//
// Returns:
//   - int: GID value if set and valid.
//   - bool: True if label is present and valid, false otherwise.
func (c *Container) GetLifecycleGID() (int, bool) {
	return c.getLifecycleID(labels.LifecycleGID, "GID")
}

// Enabled checks if Watchtower should manage the container.
//
// Returns:
//   - bool: True if enabled, false otherwise.
//   - bool: True if label is set and valid, false if absent or invalid.
func (c *Container) Enabled() (bool, bool) {
	clogVal := c.Logger().With().
		Str("container", c.Name()).
		Logger()
	clog := &clogVal
	rawBool, ok := c.getLabelValue(labels.Enable)

	// Label not set, return default.
	if !ok {
		clog.Debug().
			Str("label", labels.Enable).
			Msg("Enable label not set")

		return false, false
	}

	// Parse enable label value.
	parsedBool, err := strconv.ParseBool(rawBool)
	if err != nil {
		clog.Warn().
			Err(err).
			Str("label", labels.Enable).
			Str("value", rawBool).
			Msg("Invalid enable label value")

		return false, false
	}

	clog.Debug().
		Str("label", labels.Enable).
		Bool("value", parsedBool).
		Msg("Retrieved enable status")

	return parsedBool, true
}

// GetLabel retrieves the value of an arbitrary container label.
//
// It returns the label value and true if the label is present, or an empty
// string and false if the label is absent.
//
// Parameters:
//   - label: Docker label key to look up.
//
// Returns:
//   - string: Label value if present.
//   - bool: True if label exists, false otherwise.
func (c *Container) GetLabel(label string) (string, bool) {
	return c.getLabelValue(label)
}

// IsMonitorOnly determines if the container is monitor-only.
//
// It uses UpdateParams.MonitorOnly and label precedence.
//
// Parameters:
//   - params: Update parameters from types.UpdateParams.
//
// Returns:
//   - bool: True if monitor-only, false otherwise.
func (c *Container) IsMonitorOnly(params types.UpdateParams) bool {
	return c.getContainerOrGlobalBool(params.MonitorOnly, labels.MonitorOnly, params.LabelPrecedence)
}

// IsNoPull determines if image pulls should be skipped.
//
// It uses UpdateParams.NoPull and label precedence.
//
// Parameters:
//   - params: Update parameters from types.UpdateParams.
//
// Returns:
//   - bool: True if no-pull, false otherwise.
func (c *Container) IsNoPull(params types.UpdateParams) bool {
	return c.getContainerOrGlobalBool(params.NoPull, labels.NoPull, params.LabelPrecedence)
}

// IsChangelogEnabled reports whether the changelog notification line and the
// versioned release link are enabled for this container.
//
// It uses UpdateParams.EnableChangelog and label precedence, with the same two
// sources no-pull uses. A changelog-url label is a value, not a switch, so it
// never enables the feature on its own.
// A user who authorizes the extra
// registry request must say so explicitly.
//
// Parameters:
//   - params: Update parameters from types.UpdateParams.
//
// Returns:
//   - bool: True when the changelog feature is enabled.
func (c *Container) IsChangelogEnabled(params types.UpdateParams) bool {
	return c.getContainerOrGlobalBool(
		params.EnableChangelog,
		labels.EnableChangelog,
		params.LabelPrecedence,
	)
}

// CooldownDelay returns the effective cooldown delay for this container.
//
// If the container has the cooldown-delay label set, its value is used (parsed
// as a duration string). Otherwise, the global CooldownDelay from UpdateParams
// is used. Setting the label to "0" disables cooldown for this container.
//
// Parameters:
//   - params: Update parameters from types.UpdateParams.
//
// Returns:
//   - time.Duration: Effective cooldown delay for this container.
func (c *Container) CooldownDelay(params types.UpdateParams) time.Duration {
	labelVal, ok := c.getLabelValue(labels.CooldownDelay)
	if !ok {
		// No label set — use global value.
		return params.CooldownDelay
	}

	if labelVal == "" {
		// Label present but empty — use global value.
		return params.CooldownDelay
	}

	parsed, err := units.ParseDuration(labelVal)
	if err != nil {
		c.Logger().Warn().
			Err(err).
			Str("container", c.Name()).
			Str("label", labels.CooldownDelay).
			Str("value", labelVal).
			Msg("Failed to parse cooldown-delay label, using global value")

		return params.CooldownDelay
	}

	c.Logger().Debug().
		Str("container", c.Name()).
		Str("label", labels.CooldownDelay).
		Str("value", labelVal).
		Dur("duration", parsed).
		Msg("Parsed cooldown-delay label")

	return parsed
}

// Scope retrieves the monitoring scope from labels.
//
// Returns:
//   - string: Scope value if set, empty otherwise.
//   - bool: True if label is set, false if absent.
func (c *Container) Scope() (string, bool) {
	clogVal := c.Logger().With().
		Str("container", c.Name()).
		Logger()
	clog := &clogVal
	rawString, ok := c.getLabelValue(labels.Scope)

	if !ok {
		clog.Debug().
			Str("label", labels.Scope).
			Msg("Scope label not set")

		return "", false
	}

	clog.Debug().
		Str("label", labels.Scope).
		Str("value", rawString).
		Msg("Retrieved scope")

	return rawString, true
}

// NormalizedScope returns the container's scope, or [labels.NoScope] when its
// scope label is unset or empty.
//
// Returns:
//   - string: The container's scope.
func (c *Container) NormalizedScope() string {
	scope, ok := c.Scope()
	if !ok || scope == "" {
		return labels.NoScope
	}

	return scope
}

// GetEffectiveScope determines the effective operational scope by prioritizing explicit scope
// over scope derived from the container's label. This is crucial for self-update scenarios where
// a new Watchtower instance needs to inherit the same scope as the instance being replaced
// to maintain proper isolation and prevent cross-scope interference during the update process.
//
// Parameters:
//   - container: The current Watchtower container.
//   - currentScope: The explicit scope value (takes priority if non-empty).
//
// Returns:
//   - string: The effective scope.
//   - error: Non-nil if derivation fails.
func GetEffectiveScope(container types.Container, currentScope string) (string, error) {
	// Skip derivation if scope is already explicitly set.
	if currentScope != "" {
		return currentScope, nil
	}

	// Container not available.
	if container == nil {
		return "", fmt.Errorf("%w", errCurrentContainerNotCached)
	}

	// Extract the scope label from the container.
	derivedScope, ok := container.Scope()
	if ok && derivedScope != "" {
		nopLog().Debug().
			Str("derived_scope", derivedScope).
			Msg("Derived operational scope from current container's scope label")

		return derivedScope, nil
	}

	return currentScope, nil
}

// GetContainerChain returns the container chain label value.
//
// Returns:
//   - string: The label value or empty if absent.
//   - bool: True if the label is present, false otherwise.
func (c *Container) GetContainerChain() (string, bool) {
	return c.getLabelValue(labels.ContainerChain)
}

// IsWatchtower identifies if this is the Watchtower container.
//
// Returns:
//   - bool: True if watchtower label is "true", false otherwise.
func (c *Container) IsWatchtower() bool {
	info := c.ContainerInfo()
	if info == nil || info.Config == nil {
		return false
	}

	return containsWatchtowerLabel(info.Config.Labels)
}

// StopSignal returns the custom stop signal from labels or HostConfig.
//
// Returns:
//   - string: Signal value, defaulting to "SIGTERM" if unset.
func (c *Container) StopSignal() string {
	info := c.ContainerInfo()

	clogVal := c.Logger().With().
		Str("container", c.Name()).
		Logger()
	clog := &clogVal

	// Check label first
	signal := c.getLabelValueOrEmpty(labels.StopSignal)
	if signal != "" {
		clog.Debug().
			Str("label", labels.StopSignal).
			Str("signal", signal).
			Msg("Retrieved stop signal from label")

		return signal
	}

	// Check Config
	if info != nil && info.Config != nil &&
		info.Config.StopSignal != "" {
		signal = info.Config.StopSignal
		clog.Debug().
			Str("signal", signal).
			Msg("Retrieved stop signal from Config")

		return signal
	}

	// Default to SIGTERM
	clog.Debug().Msg("Stop signal not set, using default SIGTERM")

	return "SIGTERM"
}

// StopTimeout returns how long a stop waits for the container to exit before
// killing it.
//
// A stop timeout of 0 or more set on the container itself, such as with
// docker run --stop-timeout, takes precedence. Otherwise the stop waits for
// fallback. A container stop timeout of -1, which tells Docker to wait
// indefinitely, also uses fallback, so a stop always ends. A container stop
// timeout too long for a [time.Duration] saturates at the longest duration.
//
// Parameters:
//   - fallback: How long to wait when the container sets no usable stop timeout.
//
// Returns:
//   - time.Duration: How long the stop waits.
func (c *Container) StopTimeout(fallback time.Duration) time.Duration {
	info := c.ContainerInfo()

	clogVal := c.Logger().With().
		Str("container", c.Name()).
		Logger()
	clog := &clogVal

	if info == nil || info.Config == nil || info.Config.StopTimeout == nil {
		clog.Debug().Msg("Stop timeout not set in container config")

		return fallback
	}

	timeout := *info.Config.StopTimeout
	clog.Debug().
		Int("timeout", timeout).
		Msg("Retrieved stop timeout from Config")

	if timeout < 0 {
		return fallback
	}

	if int64(timeout) > math.MaxInt64/int64(time.Second) {
		return math.MaxInt64
	}

	return time.Duration(timeout) * time.Second
}

// containsWatchtowerLabel checks if the container is Watchtower.
//
// Parameters:
//   - containerLabels: Label map to check.
//
// Returns:
//   - bool: True if watchtower label is "true", false otherwise.
func containsWatchtowerLabel(containerLabels map[string]string) bool {
	if containerLabels == nil {
		return false
	}

	val, ok := containerLabels[labels.Watchtower]

	return ok && val == "true"
}

// getRawLabelValue retrieves a raw label value.
//
// Parameters:
//   - label: The label key to retrieve the value from.
//
// Returns:
//   - string: Label value if present.
//   - bool: True if label exists and is accessible, false otherwise.
func (c *Container) getRawLabelValue(label string) (string, bool) {
	return labelValue(c.ContainerInfo(), label)
}

// labelValue retrieves a raw label value from inspect data.
//
// Parameters:
//   - info: Inspect data, or nil.
//   - label: The label key to retrieve the value from.
//
// Returns:
//   - string: Label value if present.
//   - bool: True if label exists and is accessible, false otherwise.
func labelValue(info *dockerContainer.InspectResponse, label string) (string, bool) {
	if info == nil || info.Config == nil || info.Config.Labels == nil {
		return "", false
	}

	val, ok := info.Config.Labels[label]

	return val, ok
}

// getLabelValueOrEmpty retrieves a label's value or empty string.
//
// Returns:
//   - string: Label value or empty if absent.
func (c *Container) getLabelValueOrEmpty(label string) string {
	val, ok := c.getRawLabelValue(label)
	if !ok {
		return ""
	}

	return val
}

// getLabelValue fetches a label's value and presence.
//
// Returns:
//   - string: Label value if present.
//   - bool: True if label exists, false otherwise.
func (c *Container) getLabelValue(label string) (string, bool) {
	val, ok := c.getRawLabelValue(label)
	if !ok {
		return "", false
	}

	return val, true
}

// getBoolLabelValue parses a label as a boolean.
//
// Returns:
//   - bool: Parsed value if valid.
//   - error: Non-nil if parsing fails or label is absent, nil on success.
func (c *Container) getBoolLabelValue(label string) (bool, error) {
	strVal, ok := c.getRawLabelValue(label)
	if !ok {
		return false, errLabelNotFound
	}

	// Treat empty string as false to handle cases where label is explicitly set to empty
	if strVal == "" {
		return false, nil
	}

	value, err := strconv.ParseBool(strVal)
	if err != nil {
		c.Logger().Warn().
			Err(err).
			Str("container", c.Name()).
			Str("label", label).
			Str("value", strVal).
			Msg("Failed to parse boolean label value")

		return false, fmt.Errorf("%w: %s=%q", err, label, strVal)
	}

	return value, nil
}

// getContainerOrGlobalBool resolves a boolean from label or global setting.
//
// It respects label precedence if set.
//
// Parameters:
//   - globalVal: Global boolean value.
//   - label: Label to check.
//   - contPrecedence: Whether container label takes precedence.
//
// Returns:
//   - bool: Resolved boolean value.
func (c *Container) getContainerOrGlobalBool(
	globalVal bool,
	label string,
	contPrecedence bool,
) bool {
	clogVal := c.Logger().With().
		Str("container", c.Name()).
		Logger()
	clog := &clogVal

	// Fetch container-specific value.
	contVal, err := c.getBoolLabelValue(label)
	if err != nil {
		if !errors.Is(err, errLabelNotFound) {
			clog.Warn().
				Err(err).
				Str("label", label).
				Msg("Failed to parse label value")
		}

		clog.Debug().
			Str("label", label).
			Bool("global_val", globalVal).
			Msg("Using global value due to label absence or error")

		return globalVal
	}

	// Apply container precedence if set.
	if contPrecedence {
		clog.Debug().
			Str("label", label).
			Bool("cont_val", contVal).
			Str("precedence", "container").
			Msg("Using container label value with precedence")

		return contVal
	}

	// Combine values if no precedence.
	result := contVal || globalVal
	clog.Debug().
		Str("label", label).
		Bool("cont_val", contVal).
		Bool("global_val", globalVal).
		Bool("result", result).
		Msg("Combined container and global values")

	return result
}

// SetLabel sets a container config label used on the next create.
//
// Parameters:
//   - key: Label key.
//   - value: Label value.
//
// Returns:
//   - none.
func (c *Container) SetLabel(key, value string) {
	if c == nil || key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.containerInfo == nil || c.containerInfo.Config == nil {
		return
	}

	c.replaceInspectLocked(func(info *dockerContainer.InspectResponse) {
		labels := maps.Clone(info.Config.Labels)
		if labels == nil {
			labels = make(map[string]string, 1)
		}

		labels[key] = value
		info.Config.Labels = labels
	})
}

// DeleteLabel removes a container config label used on the next create.
//
// Parameters:
//   - key: Label key.
//
// Returns:
//   - none.
func (c *Container) DeleteLabel(key string) {
	if c == nil || key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.containerInfo == nil || c.containerInfo.Config == nil {
		return
	}

	if _, ok := c.containerInfo.Config.Labels[key]; !ok {
		return
	}

	c.replaceInspectLocked(func(info *dockerContainer.InspectResponse) {
		labels := maps.Clone(info.Config.Labels)
		delete(labels, key)
		info.Config.Labels = labels
	})
}

// IsWatchtowerParent checks if the current container ID exists in the comma-separated container-chain label values.
//
// It handles edge cases like empty chain or invalid IDs by returning false appropriately.
// The chain values are trimmed of whitespace before comparison.
//
// Parameters:
//   - currentID: The container ID to check for in the chain.
//   - chain: Comma-separated string of container IDs from the container-chain label.
//
// Returns:
//   - bool: True if currentID is found in the chain, false otherwise.
func IsWatchtowerParent(currentID types.ContainerID, chain string) bool {
	if currentID == "" || chain == "" {
		return false
	}

	ids := strings.SplitSeq(chain, ",")
	for id := range ids {
		if strings.TrimSpace(id) == string(currentID) {
			return true
		}
	}

	return false
}

// IsSuperseded reports whether a container is a Watchtower instance that a
// self-update has replaced. That is the case for a predecessor renamed with
// the "watchtower-old-" prefix, and for a container whose chain label lists it
// as the parent of a newer instance.
//
// Parameters:
//   - container: The running Watchtower container, or nil when it was not inspected.
//
// Returns:
//   - bool: True when the container has been replaced, false otherwise or for nil.
func IsSuperseded(container types.Container) bool {
	if container == nil {
		return false
	}

	if IsOldContainer(container.Name()) {
		return true
	}

	chain, present := container.GetContainerChain()

	return present && IsWatchtowerParent(container.ID(), chain)
}

// IsOrphanedReplacement reports whether a container counts as a replacement
// that an interrupted self-update left in the created state.
//
// Any Watchtower container in the created state and in the current container's
// scope counts, unless it is the current container or a renamed predecessor.
// One scope runs one Watchtower instance, so such a container is never a separate
// instance to keep.
//
// Parameters:
//   - candidate: The container to check.
//   - current: The running Watchtower container.
//
// Returns:
//   - bool: True when the candidate is an orphaned replacement.
func IsOrphanedReplacement(candidate, current types.Container) bool {
	return candidate.IsWatchtower() &&
		candidate.ID() != current.ID() &&
		!IsOldContainer(candidate.Name()) &&
		candidate.IsCreated() &&
		candidate.NormalizedScope() == current.NormalizedScope()
}

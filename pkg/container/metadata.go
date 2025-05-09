package container

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Watchtower-specific labels identify containers managed by Watchtower and their configurations.
const (
	// watchtowerLabel marks a container as the Watchtower instance itself when set to "true".
	watchtowerLabel = "com.centurylinklabs.watchtower"
	// signalLabel specifies a custom stop signal for the container (e.g., "SIGTERM").
	signalLabel = "com.centurylinklabs.watchtower.stop-signal"
	// enableLabel indicates whether Watchtower should manage this container (true/false).
	enableLabel = "com.centurylinklabs.watchtower.enable"
	// monitorOnlyLabel flags the container for monitoring only, without updates (true/false).
	monitorOnlyLabel = "com.centurylinklabs.watchtower.monitor-only"
	// noPullLabel prevents Watchtower from pulling a new image for this container (true/false).
	noPullLabel = "com.centurylinklabs.watchtower.no-pull"
	// dependsOnLabel lists container names this container depends on, comma-separated.
	dependsOnLabel = "com.centurylinklabs.watchtower.depends-on"
	// zodiacLabel stores the original image name for Zodiac compatibility.
	zodiacLabel = "com.centurylinklabs.zodiac.original-image"
	// scope defines a unique monitoring scope for this Watchtower instance.
	scope = "com.centurylinklabs.watchtower.scope"
)

// Lifecycle hook labels configure commands executed during container update phases.
const (
	// preCheckLabel specifies a command to run before checking for updates.
	preCheckLabel = "com.centurylinklabs.watchtower.lifecycle.pre-check"
	// postCheckLabel specifies a command to run after checking for updates.
	postCheckLabel = "com.centurylinklabs.watchtower.lifecycle.post-check"
	// preUpdateLabel specifies a command to run before updating the container.
	preUpdateLabel = "com.centurylinklabs.watchtower.lifecycle.pre-update"
	// postUpdateLabel specifies a command to run after updating the container.
	postUpdateLabel = "com.centurylinklabs.watchtower.lifecycle.post-update"
	// preUpdateTimeoutLabel sets the timeout (in minutes) for the pre-update command.
	preUpdateTimeoutLabel = "com.centurylinklabs.watchtower.lifecycle.pre-update-timeout"
	// postUpdateTimeoutLabel sets the timeout (in minutes) for the post-update command.
	postUpdateTimeoutLabel = "com.centurylinklabs.watchtower.lifecycle.post-update-timeout"
)

// GetLifecyclePreCheckCommand returns the pre-check command from labels.
//
// Returns:
//   - string: Pre-check command or empty if unset.
func (c Container) GetLifecyclePreCheckCommand() string {
	return c.getLabelValueOrEmpty(preCheckLabel)
}

// GetLifecyclePostCheckCommand returns the post-check command from labels.
//
// Returns:
//   - string: Post-check command or empty if unset.
func (c Container) GetLifecyclePostCheckCommand() string {
	return c.getLabelValueOrEmpty(postCheckLabel)
}

// GetLifecyclePreUpdateCommand returns the pre-update command from labels.
//
// Returns:
//   - string: Pre-update command or empty if unset.
func (c Container) GetLifecyclePreUpdateCommand() string {
	return c.getLabelValueOrEmpty(preUpdateLabel)
}

// GetLifecyclePostUpdateCommand returns the post-update command from labels.
//
// Returns:
//   - string: Post-update command or empty if unset.
func (c Container) GetLifecyclePostUpdateCommand() string {
	return c.getLabelValueOrEmpty(postUpdateLabel)
}

// PreUpdateTimeout returns the pre-update command timeout in minutes.
//
// It defaults to 1 minute if unset or invalid; 0 allows indefinite execution.
//
// Returns:
//   - int: Timeout in minutes.
func (c Container) PreUpdateTimeout() int {
	clog := logrus.WithField("container", c.Name())
	val := c.getLabelValueOrEmpty(preUpdateTimeoutLabel)

	// Use default if label is unset.
	if val == "" {
		clog.WithField("label", preUpdateTimeoutLabel).
			Debug("Pre-update timeout not set, using default")

		return 1
	}

	// Parse timeout value.
	minutes, err := strconv.Atoi(val)
	if err != nil {
		clog.WithError(err).WithFields(logrus.Fields{
			"label": preUpdateTimeoutLabel,
			"value": val,
		}).Warn("Invalid pre-update timeout value, using default")

		return 1
	}

	clog.WithFields(logrus.Fields{
		"label":   preUpdateTimeoutLabel,
		"minutes": minutes,
	}).Debug("Retrieved pre-update timeout")

	return minutes
}

// PostUpdateTimeout returns the post-update command timeout in minutes.
//
// It defaults to 1 minute if unset or invalid; 0 allows indefinite execution.
//
// Returns:
//   - int: Timeout in minutes.
func (c Container) PostUpdateTimeout() int {
	clog := logrus.WithField("container", c.Name())
	val := c.getLabelValueOrEmpty(postUpdateTimeoutLabel)

	// Use default if label is unset.
	if val == "" {
		clog.WithField("label", postUpdateTimeoutLabel).
			Debug("Post-update timeout not set, using default")

		return 1
	}

	// Parse timeout value.
	minutes, err := strconv.Atoi(val)
	if err != nil {
		clog.WithError(err).WithFields(logrus.Fields{
			"label": postUpdateTimeoutLabel,
			"value": val,
		}).Warn("Invalid post-update timeout value, using default")

		return 1
	}

	clog.WithFields(logrus.Fields{
		"label":   postUpdateTimeoutLabel,
		"minutes": minutes,
	}).Debug("Retrieved post-update timeout")

	return minutes
}

// Enabled checks if Watchtower should manage the container.
//
// Returns:
//   - bool: True if enabled, false otherwise.
//   - bool: True if label is set, false if absent/invalid.
func (c Container) Enabled() (bool, bool) {
	clog := logrus.WithField("container", c.Name())
	rawBool, ok := c.getLabelValue(enableLabel)

	// Label not set, return default.
	if !ok {
		clog.WithField("label", enableLabel).Debug("Enable label not set")

		return false, false
	}

	// Parse enable label value.
	parsedBool, err := strconv.ParseBool(rawBool)
	if err != nil {
		clog.WithError(err).WithFields(logrus.Fields{
			"label": enableLabel,
			"value": rawBool,
		}).Warn("Invalid enable label value")

		return false, false
	}

	clog.WithFields(logrus.Fields{
		"label": enableLabel,
		"value": parsedBool,
	}).Debug("Retrieved enable status")

	return parsedBool, true
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
func (c Container) IsMonitorOnly(params types.UpdateParams) bool {
	return c.getContainerOrGlobalBool(params.MonitorOnly, monitorOnlyLabel, params.LabelPrecedence)
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
func (c Container) IsNoPull(params types.UpdateParams) bool {
	return c.getContainerOrGlobalBool(params.NoPull, noPullLabel, params.LabelPrecedence)
}

// Scope retrieves the monitoring scope from labels.
//
// Returns:
//   - string: Scope value if set, empty otherwise.
//   - bool: True if label is set, false if absent.
func (c Container) Scope() (string, bool) {
	clog := logrus.WithField("container", c.Name())
	rawString, ok := c.getLabelValue(scope)

	if !ok {
		clog.WithField("label", scope).Debug("Scope label not set")

		return "", false
	}

	clog.WithFields(logrus.Fields{
		"label": scope,
		"value": rawString,
	}).Debug("Retrieved scope")

	return rawString, true
}

// IsWatchtower identifies if this is the Watchtower container.
//
// Returns:
//   - bool: True if watchtower label is "true", false otherwise.
func (c Container) IsWatchtower() bool {
	clog := logrus.WithField("container", c.Name())
	isWatchtower := ContainsWatchtowerLabel(c.containerInfo.Config.Labels)
	clog.WithField("is_watchtower", isWatchtower).Debug("Checked if container is Watchtower")

	return isWatchtower
}

// StopSignal returns the custom stop signal from labels.
//
// Returns:
//   - string: Signal value or empty if unset.
func (c Container) StopSignal() string {
	clog := logrus.WithField("container", c.Name())
	signal := c.getLabelValueOrEmpty(signalLabel)

	if signal == "" {
		clog.WithField("label", signalLabel).Debug("Stop signal not set")
	} else {
		clog.WithFields(logrus.Fields{
			"label":  signalLabel,
			"signal": signal,
		}).Debug("Retrieved stop signal")
	}

	return signal
}

// ContainsWatchtowerLabel checks if the container is Watchtower.
//
// Parameters:
//   - labels: Label map to check.
//
// Returns:
//   - bool: True if watchtower label is "true", false otherwise.
func ContainsWatchtowerLabel(labels map[string]string) bool {
	val, ok := labels[watchtowerLabel]

	return ok && val == "true"
}

// getLabelValueOrEmpty retrieves a label’s value or empty string.
//
// Returns:
//   - string: Label value or empty if absent.
func (c Container) getLabelValueOrEmpty(label string) string {
	var clog *logrus.Entry
	if c.containerInfo == nil || c.containerInfo.Config == nil {
		clog = logrus.WithField("container", "<unknown>")
	} else {
		clog = logrus.WithField("container", c.Name())
	}

	// Check for nil metadata.
	if c.containerInfo == nil || c.containerInfo.Config == nil ||
		c.containerInfo.Config.Labels == nil {
		clog.WithField("label", label).Debug("No labels available")

		return ""
	}

	// Return label value if present.
	if val, ok := c.containerInfo.Config.Labels[label]; ok {
		return val
	}

	clog.WithField("label", label).Debug("Label not found")

	return ""
}

// getLabelValue fetches a label’s value and presence.
//
// Returns:
//   - string: Label value if present.
//   - bool: True if label exists, false otherwise.
func (c Container) getLabelValue(label string) (string, bool) {
	clog := logrus.WithField("container", c.Name())

	// Check for nil metadata.
	if c.containerInfo == nil || c.containerInfo.Config == nil ||
		c.containerInfo.Config.Labels == nil {
		clog.WithField("label", label).Debug("No labels available")

		return "", false
	}

	// Return value and presence.
	if val, ok := c.containerInfo.Config.Labels[label]; ok {
		clog.WithFields(logrus.Fields{
			"label": label,
			"value": val,
		}).Debug("Retrieved label value")

		return val, true
	}

	clog.WithField("label", label).Debug("Label not found")

	return "", false
}

// getBoolLabelValue parses a label as a boolean.
//
// Returns:
//   - bool: Parsed value if valid.
//   - error: Non-nil if parsing fails or label is absent, nil on success.
func (c Container) getBoolLabelValue(label string) (bool, error) {
	clog := logrus.WithField("container", c.Name())

	// Check for nil metadata.
	if c.containerInfo == nil || c.containerInfo.Config == nil ||
		c.containerInfo.Config.Labels == nil {
		clog.WithField("label", label).Debug("No labels available")

		return false, errLabelNotFound
	}

	// Fetch label value.
	strVal, ok := c.containerInfo.Config.Labels[label]
	if !ok {
		clog.WithField("label", label).Debug("Label not found")

		return false, errLabelNotFound
	}

	// Parse as boolean.
	value, err := strconv.ParseBool(strVal)
	if err != nil {
		clog.WithError(err).WithFields(logrus.Fields{
			"label": label,
			"value": strVal,
		}).Warn("Failed to parse boolean label value")

		return false, fmt.Errorf("%w: %s=%q", err, label, strVal)
	}

	clog.WithFields(logrus.Fields{
		"label": label,
		"value": value,
	}).Debug("Parsed boolean label value")

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
func (c Container) getContainerOrGlobalBool(
	globalVal bool,
	label string,
	contPrecedence bool,
) bool {
	clog := logrus.WithField("container", c.Name())

	// Fetch container-specific value.
	contVal, err := c.getBoolLabelValue(label)
	if err != nil {
		if !errors.Is(err, errLabelNotFound) {
			clog.WithError(err).
				WithField("label", label).
				Warn("Failed to parse label value")
		}

		clog.WithFields(logrus.Fields{
			"label":      label,
			"global_val": globalVal,
		}).Debug("Using global value due to label absence or error")

		return globalVal
	}

	// Apply container precedence if set.
	if contPrecedence {
		clog.WithFields(logrus.Fields{
			"label":      label,
			"cont_val":   contVal,
			"precedence": "container",
		}).Debug("Using container label value with precedence")

		return contVal
	}

	// Combine values if no precedence.
	result := contVal || globalVal
	clog.WithFields(logrus.Fields{
		"label":      label,
		"cont_val":   contVal,
		"global_val": globalVal,
		"result":     result,
	}).Debug("Combined container and global values")

	return result
}

// Package container provides functionality for managing Docker containers within Watchtower.
// This file contains methods and helpers for accessing and interpreting container metadata,
// focusing on labels that configure Watchtower behavior and lifecycle hooks.
// These methods operate on the Container type defined in container.go.
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

// Lifecycle Hook Methods
// These methods retrieve commands and timeouts associated with lifecycle hooks from container labels.

// GetLifecyclePreCheckCommand returns the pre-check command from the container’s metadata.
// It retrieves the command specified by the pre-check label, returning an empty string if not set.
func (c Container) GetLifecyclePreCheckCommand() string {
	return c.getLabelValueOrEmpty(preCheckLabel)
}

// GetLifecyclePostCheckCommand returns the post-check command from the container’s metadata.
// It retrieves the command specified by the post-check label, returning an empty string if not set.
func (c Container) GetLifecyclePostCheckCommand() string {
	return c.getLabelValueOrEmpty(postCheckLabel)
}

// GetLifecyclePreUpdateCommand returns the pre-update command from the container’s metadata.
// It retrieves the command specified by the pre-update label, returning an empty string if not set.
func (c Container) GetLifecyclePreUpdateCommand() string {
	return c.getLabelValueOrEmpty(preUpdateLabel)
}

// GetLifecyclePostUpdateCommand returns the post-update command from the container’s metadata.
// It retrieves the command specified by the post-update label, returning an empty string if not set.
func (c Container) GetLifecyclePostUpdateCommand() string {
	return c.getLabelValueOrEmpty(postUpdateLabel)
}

// PreUpdateTimeout returns the timeout (in minutes) for the pre-update command.
// It parses the pre-update timeout label, defaulting to 1 minute if unset or invalid.
// A value of 0 allows indefinite execution, which users should use cautiously to avoid hangs.
func (c Container) PreUpdateTimeout() int {
	var minutes int

	var err error

	val := c.getLabelValueOrEmpty(preUpdateTimeoutLabel)
	minutes, err = strconv.Atoi(val)

	if err != nil || val == "" {
		return 1
	}

	return minutes
}

// PostUpdateTimeout returns the timeout (in minutes) for the post-update command.
// It parses the post-update timeout label, defaulting to 1 minute if unset or invalid.
// A value of 0 allows indefinite execution, which users should use cautiously to avoid hangs.
func (c Container) PostUpdateTimeout() int {
	var minutes int

	var err error

	val := c.getLabelValueOrEmpty(postUpdateTimeoutLabel)
	minutes, err = strconv.Atoi(val)

	if err != nil || val == "" {
		return 1
	}

	return minutes
}

// Label-Based Configuration Methods
// These methods interpret container labels to determine Watchtower behavior.

// Enabled checks if the container is enabled for Watchtower management.
// It returns the parsed boolean value of the enable label and true if set,
// or false and false if the label is absent or invalid.
func (c Container) Enabled() (bool, bool) {
	rawBool, ok := c.getLabelValue(enableLabel)
	if !ok {
		return false, false
	}

	parsedBool, err := strconv.ParseBool(rawBool)
	if err != nil {
		return false, false
	}

	return parsedBool, true
}

// IsMonitorOnly determines if the container should only be monitored without updates.
// It considers the global MonitorOnly parameter, the monitor-only label, and label precedence,
// returning true if either the label or global setting (depending on precedence) indicates monitoring only.
func (c Container) IsMonitorOnly(params types.UpdateParams) bool {
	return c.getContainerOrGlobalBool(params.MonitorOnly, monitorOnlyLabel, params.LabelPrecedence)
}

// IsNoPull determines if the container should skip image pulls.
// It considers the global NoPull parameter, the no-pull label, and label precedence,
// returning true if either the label or global setting (depending on precedence) indicates no pull.
func (c Container) IsNoPull(params types.UpdateParams) bool {
	return c.getContainerOrGlobalBool(params.NoPull, noPullLabel, params.LabelPrecedence)
}

// Scope retrieves the monitoring scope for the container.
// It returns the scope label value and true if set, or an empty string and false if not.
func (c Container) Scope() (string, bool) {
	rawString, ok := c.getLabelValue(scope)
	if !ok {
		return "", false
	}

	return rawString, true
}

// IsWatchtower identifies if this is the Watchtower container itself.
// It returns true if the watchtower label is present and set to "true".
func (c Container) IsWatchtower() bool {
	return ContainsWatchtowerLabel(c.containerInfo.Config.Labels)
}

// StopSignal returns the custom stop signal for the container.
// It retrieves the signal label value, returning an empty string if not set.
func (c Container) StopSignal() string {
	return c.getLabelValueOrEmpty(signalLabel)
}

// General Label Helpers
// These functions provide utility methods for accessing and interpreting container labels.

// ContainsWatchtowerLabel checks if a container’s labels indicate it is a Watchtower instance.
// It examines the provided label map for the watchtower label, returning true if set to "true".
func ContainsWatchtowerLabel(labels map[string]string) bool {
	val, ok := labels[watchtowerLabel]

	return ok && val == "true"
}

// getLabelValueOrEmpty retrieves a label’s value from the container’s metadata.
// It returns the value associated with the specified label, or an empty string if the label is not present.
func (c Container) getLabelValueOrEmpty(label string) string {
	if val, ok := c.containerInfo.Config.Labels[label]; ok {
		return val
	}

	return ""
}

// getLabelValue fetches a label’s value and its presence from the container’s metadata.
// It returns the value and a boolean indicating whether the label exists in the container’s labels.
func (c Container) getLabelValue(label string) (string, bool) {
	val, ok := c.containerInfo.Config.Labels[label]

	return val, ok
}

// getBoolLabelValue parses a label’s value as a boolean from the container’s metadata.
// It returns the parsed boolean value and nil if the label exists and is valid,
// or false and an error if parsing fails or the label is not found (errLabelNotFound).
func (c Container) getBoolLabelValue(label string) (bool, error) {
	if strVal, ok := c.containerInfo.Config.Labels[label]; ok {
		value, err := strconv.ParseBool(strVal)
		if err != nil {
			return false, fmt.Errorf(
				"failed to parse boolean value for label %s=%q: %w",
				label,
				strVal,
				err,
			)
		}

		return value, nil
	}

	return false, errLabelNotFound
}

// getContainerOrGlobalBool resolves a boolean value from a label or global parameter.
// It prefers the label value if precedence is set, otherwise combines it with the global value,
// logging warnings for parsing errors and defaulting to the global value if the label is absent.
func (c Container) getContainerOrGlobalBool(
	globalVal bool,
	label string,
	contPrecedence bool,
) bool {
	contVal, err := c.getBoolLabelValue(label)
	if err != nil {
		if !errors.Is(err, errLabelNotFound) {
			logrus.WithField("error", err).
				WithField("label", label).
				Warn("Failed to parse label value")
		}

		return globalVal
	}

	if contPrecedence {
		return contVal
	}

	return contVal || globalVal
}

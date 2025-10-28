// Package mocks provides mock implementations for testing Watchtower components.
package mocks

import (
	"errors"

	"github.com/nicholas-fedor/watchtower/pkg/session"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// errMockSkipped is a static error indicating a mock container was skipped.
// It is used in CreateMockProgressReport for consistent error reporting.
var errMockSkipped = errors.New("unpossible")

// errMockFailed is a static error indicating a mock container update failed.
// It is used in CreateMockProgressReport for consistent error reporting.
var errMockFailed = errors.New("accidentally the whole container")

// State ID prefixes for generating unique mock container IDs.
const (
	skippedIDPrefix = 41 // Prefix for SkippedState containers (e.g., "c7941...").
	freshIDPrefix   = 31 // Prefix for FreshState containers (e.g., "c7931...").
	updatedIDPrefix = 11 // Prefix for UpdatedState containers (e.g., "c7911...").
	failedIDPrefix  = 21 // Prefix for FailedState containers (e.g., "c7921...").
)

// CreateMockProgressReport generates a mock report from a given set of container states.
// It assigns each container a unique ID and name based on its state and index,
// simulating various update outcomes for testing session progress reporting.
func CreateMockProgressReport(states ...session.State) types.Report {
	// Track the number of occurrences for each state to ensure unique IDs and names.
	stateNums := make(map[session.State]int)
	progress := session.Progress{}
	failed := make(map[types.ContainerID]error)

	for _, state := range states {
		index := stateNums[state]

		switch state {
		case session.SkippedState:
			c, _ := CreateContainerForProgress(index, skippedIDPrefix, "skip%d")
			progress.AddSkipped(c, errMockSkipped, types.UpdateParams{})
		case session.FreshState:
			c, _ := CreateContainerForProgress(index, freshIDPrefix, "frsh%d")
			progress.AddScanned(c, c.ImageID(), types.UpdateParams{})
		case session.UpdatedState:
			c, newImage := CreateContainerForProgress(index, updatedIDPrefix, "updt%d")
			progress.AddScanned(c, newImage, types.UpdateParams{})
			progress.MarkForUpdate(c.ID())
		case session.FailedState:
			c, newImage := CreateContainerForProgress(index, failedIDPrefix, "fail%d")
			progress.AddScanned(c, newImage, types.UpdateParams{})

			failed[c.ID()] = errMockFailed
		case session.UnknownState, session.ScannedState, session.StaleState:
			// These states are not explicitly handled in this mock as they’re intermediate or unused here.
			continue
		}

		stateNums[state] = index + 1
	}

	progress.UpdateFailed(failed)

	return progress.Report()
}

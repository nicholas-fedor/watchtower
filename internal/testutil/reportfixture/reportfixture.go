package reportfixture

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"
	dockerNetwork "github.com/moby/moby/api/types/network"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// containerKind is how the containers of one state are numbered and named.
type containerKind struct {
	nameFormat string // Name format with one %d verb for the one-based position.
	idPrefix   int    // Digits that start the container's ID after "c79".
}

// Container ID prefixes of each state, ten apart so each state's containers
// keep distinct IDs, such as "c7941..." for the first skipped container.
const (
	// updatedIDPrefix prefixes updated containers.
	updatedIDPrefix = 11
	// failedIDPrefix prefixes failed containers.
	failedIDPrefix = 21
	// freshIDPrefix prefixes fresh containers.
	freshIDPrefix = 31
	// skippedIDPrefix prefixes skipped containers.
	skippedIDPrefix = 41
	// restartedIDPrefix prefixes restarted containers.
	restartedIDPrefix = 51
)

// idLength is the length of a full container or image ID.
const idLength = 64

// maxContainersPerState is the number of containers of one state that keep
// unique IDs. The ID prefixes of the states are ten apart, so the eleventh
// updated container would reuse the first failed container's ID.
const maxContainersPerState = 10

// containerKinds gives the ID prefix and name of each state's containers.
var containerKinds = map[state.State]containerKind{
	state.UpdatedState:   {idPrefix: updatedIDPrefix, nameFormat: "updt%d"},
	state.FailedState:    {idPrefix: failedIDPrefix, nameFormat: "fail%d"},
	state.FreshState:     {idPrefix: freshIDPrefix, nameFormat: "frsh%d"},
	state.SkippedState:   {idPrefix: skippedIDPrefix, nameFormat: "skip%d"},
	state.RestartedState: {idPrefix: restartedIDPrefix, nameFormat: "rstr%d"},
}

// Error values recorded on the skipped and failed containers of a report.
var (
	// errSkipped is the reason recorded for a skipped container.
	errSkipped = errors.New("unpossible")
	// errFailed is the error recorded for a failed container.
	errFailed = errors.New("accidentally the whole container")
)

// Report builds a report with one container for each given state, numbering
// repeated states so every container has a unique ID and name. A skipped
// container records "unpossible" as its reason. A failed container records
// "accidentally the whole container" as its error.
//
// Parameters:
//   - states: Outcome of each container, in order.
//
// Returns:
//   - types.Report: Report built through state.Progress.
func Report(states ...state.State) types.Report {
	log := new(zerolog.Nop())
	// Count each state's containers so their IDs and names stay unique.
	stateNums := make(map[state.State]int)
	progress := state.Progress{}
	failed := make(map[types.ContainerID]error)

	for _, outcome := range states {
		index := stateNums[outcome]

		switch outcome {
		case state.SkippedState:
			c, _ := Container(index, outcome)
			progress.AddSkipped(log, c, errSkipped, types.UpdateParams{})
		case state.FreshState:
			c, _ := Container(index, outcome)
			progress.AddScanned(log, c, c.ImageID(), types.UpdateParams{})
		case state.UpdatedState:
			c, newImage := Container(index, outcome)
			progress.AddScanned(log, c, newImage, types.UpdateParams{})
			progress.MarkForUpdate(log, c.ID())
		case state.FailedState:
			c, newImage := Container(index, outcome)
			progress.AddScanned(log, c, newImage, types.UpdateParams{})

			failed[c.ID()] = errFailed
		case state.RestartedState:
			c, _ := Container(index, outcome)
			progress.AddScanned(log, c, c.ImageID(), types.UpdateParams{})
			progress.MarkRestarted(log, c.ID())
		case state.UnknownState, state.ScannedState, state.StaleState:
			// Reports never end in these intermediate states.
			continue
		}

		stateNums[outcome] = index + 1
	}

	progress.UpdateFailed(log, failed)

	return progress.Report(log)
}

// Container builds a running container for a report. Its ID, image IDs and
// name come from its state and index, such as "updt1" for the first updated
// container. It panics for a state that no report ends in. It also panics when
// index is 10 or more, since that would reuse another state's IDs.
//
// Parameters:
//   - index: Zero-based position of the container among its state's containers.
//   - outcome: State the container ends the report in.
//
// Returns:
//   - types.Container: Container running image mock/<name>:latest.
//   - types.ImageID: ID of the newer image the container can move to.
func Container(index int, outcome state.State) (types.Container, types.ImageID) {
	kind, ok := containerKinds[outcome]
	if !ok {
		panic(fmt.Sprintf("reportfixture.Container: no report ends in state %v", outcome))
	}

	if index >= maxContainersPerState {
		panic(fmt.Sprintf("reportfixture.Container: index %d would reuse another state's container ID", index))
	}

	idPrefix, nameFormat := kind.idPrefix, kind.nameFormat

	indexStr := strconv.Itoa(idPrefix + index)
	// Pad the ID with zeros to full length, after the "c79" prefix.
	idSuffix := indexStr + strings.Repeat("0", idLength-3-len(indexStr))
	name := fmt.Sprintf(nameFormat, index+1)
	oldImageID := "01d" + idSuffix
	imageName := fmt.Sprintf("mock/%s:latest", name)

	inspect := dockerContainer.InspectResponse{
		ID:      "c79" + idSuffix,
		Image:   oldImageID,
		Name:    name,
		State:   &dockerContainer.State{Running: true},
		Created: time.Now().Format(time.RFC3339Nano),
		HostConfig: &dockerContainer.HostConfig{
			PortBindings: dockerNetwork.PortMap{},
		},
		Config: &dockerContainer.Config{Image: imageName},
	}
	imageInfo := dockerImage.InspectResponse{
		ID: oldImageID,
		RepoDigests: []string{
			fmt.Sprintf("%s@sha256:%s", oldImageID, strings.ReplaceAll(oldImageID, ":", "_")),
		},
	}

	return container.NewContainer(nil, &inspect, &imageInfo), types.ImageID("d0a" + idSuffix)
}

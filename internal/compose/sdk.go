package compose

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"

	composeTypes "github.com/compose-spec/compose-go/v2/types"
	dockerFlags "github.com/docker/cli/cli/flags"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// SDK applies a Compose project through docker/compose v5.
type SDK struct {
	mu      sync.Mutex
	service api.Compose
}

// NewSDK returns an applier that talks to the Docker daemon on first Apply.
//
// Returns:
//   - *SDK: Lazy Compose service.
func NewSDK() *SDK {
	return &SDK{}
}

// Apply loads the project and runs compose up, or compose build when BuildOnly is set.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - req: Project, services, labels, and build-only flag.
//
// Returns:
//   - []Container: Service instances after apply.
//   - error: Non-nil when load or apply fails.
func (a *SDK) Apply(ctx context.Context, req Request) ([]Container, error) {
	if req.Ref.Dir == "" {
		return nil, errEmptyProjectDir
	}

	if len(req.Services) == 0 {
		return nil, errNoComposeServices
	}

	project, err := Load(ctx, req.Ref)
	if err != nil {
		return nil, err
	}

	project, err = InjectLabels(project, req.Labels)
	if err != nil {
		return nil, fmt.Errorf("inject compose labels: %w", err)
	}

	svc, err := a.composeService()
	if err != nil {
		return nil, err
	}

	if req.BuildOnly {
		err = svc.Build(ctx, project, api.BuildOptions{
			Services: req.Services,
			Quiet:    true,
			Progress: "quiet",
		})
		if err != nil {
			return nil, fmt.Errorf("compose build: %w", err)
		}

		return listApplied(ctx, svc, project, req.Services)
	}

	err = svc.Up(ctx, project, api.UpOptions{
		Create: api.CreateOptions{
			Build: &api.BuildOptions{
				Services: req.Services,
				Quiet:    true,
				Progress: "quiet",
			},
			Services:             req.Services,
			Recreate:             api.RecreateForce,
			RecreateDependencies: api.RecreateNever,
			Inherit:              true,
		},
		Start: api.StartOptions{
			Project:  project,
			Services: req.Services,
			Wait:     true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("compose up: %w", err)
	}

	return listApplied(ctx, svc, project, req.Services)
}

// composeService returns the docker/compose service, creating it on first use.
//
// Returns:
//   - api.Compose: Compose service.
//   - error: Non-nil when the Docker CLI cannot be initialized.
func (a *SDK) composeService() (api.Compose, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.service != nil {
		return a.service, nil
	}

	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		return nil, fmt.Errorf("docker cli: %w", err)
	}

	err = dockerCLI.Initialize(dockerFlags.NewClientOptions())
	if err != nil {
		return nil, fmt.Errorf("docker cli init: %w", err)
	}

	svc, err := compose.NewComposeService(
		dockerCLI,
		compose.WithPrompt(compose.AlwaysOkPrompt()),
		compose.WithStreams(io.Discard, io.Discard, bytes.NewReader(nil)),
	)
	if err != nil {
		return nil, fmt.Errorf("compose service: %w", err)
	}

	a.service = svc

	return svc, nil
}

// listApplied returns running service containers after compose up or build.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - svc: Compose service.
//   - project: Loaded project.
//   - services: Service names that were applied.
//
// Returns:
//   - []Container: Service instances.
//   - error: Non-nil when compose ps fails.
func listApplied(
	ctx context.Context,
	svc api.Compose,
	project *composeTypes.Project,
	services []string,
) ([]Container, error) {
	summaries, err := svc.Ps(ctx, project.Name, api.PsOptions{
		Project:  project,
		All:      true,
		Services: services,
	})
	if err != nil {
		return nil, fmt.Errorf("compose ps: %w", err)
	}

	images, err := svc.Images(ctx, project.Name, api.ImagesOptions{Services: services})
	if err != nil {
		images = nil
	}

	out := make([]Container, 0, len(summaries))

	for _, summary := range summaries {
		out = append(out, Container{
			Service: summary.Service,
			Name:    summary.Name,
			ID:      types.ContainerID(summary.ID),
			ImageID: appliedImageID(summary, images),
		})
	}

	return out, nil
}

// appliedImageID prefers the inspected image ID from compose images over the Ps name.
//
// Parameters:
//   - summary: Compose ps row.
//   - images: compose images keyed by container name.
//
// Returns:
//   - types.ImageID: Inspected ID, a sha256: reference, or empty.
func appliedImageID(summary api.ContainerSummary, images map[string]api.ImageSummary) types.ImageID {
	if images != nil {
		if img, ok := images[summary.Name]; ok && img.ID != "" {
			return types.ImageID(img.ID)
		}
	}

	if strings.HasPrefix(summary.Image, "sha256:") {
		return types.ImageID(summary.Image)
	}

	return ""
}

package compose

import (
	"context"
	"fmt"

	"github.com/compose-spec/compose-go/v2/cli"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// Load parses a Compose project from an on-disk directory.
//
// When ConfigFiles is empty, compose.yaml / docker-compose.yaml in Dir is used.
// A project .env file is interpolated. The Watchtower process environment is
// not, so tokens and notification URLs cannot leak into the checked-out project.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - ref: Project directory, optional name, and optional compose files.
//
// Returns:
//   - *composetypes.Project: Loaded project.
//   - error: Non-nil when the directory or compose files cannot be loaded.
func Load(ctx context.Context, ref ProjectRef) (*composetypes.Project, error) {
	if ref.Dir == "" {
		return nil, errEmptyProjectDir
	}

	opts := []cli.ProjectOptionsFn{
		cli.WithWorkingDirectory(ref.Dir),
		cli.WithEnvFiles(),
		cli.WithDotEnv,
	}

	if ref.Name != "" {
		opts = append(opts, cli.WithName(ref.Name))
	}

	if len(ref.ConfigFiles) == 0 {
		opts = append(opts, cli.WithDefaultConfigPath)
	}

	options, err := cli.NewProjectOptions(ref.ConfigFiles, opts...)
	if err != nil {
		return nil, fmt.Errorf("compose project options: %w", err)
	}

	project, err := options.LoadProject(ctx)
	if err != nil {
		return nil, fmt.Errorf("load compose project: %w", err)
	}

	return project, nil
}

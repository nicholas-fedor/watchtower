package project

import (
	"context"
	"fmt"

	"github.com/compose-spec/compose-go/v2/cli"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// Load parses a Compose project from an on-disk directory.
//
// When ConfigFiles is empty, compose-go selects the default Compose file in Dir,
// together with its default override file, such as compose.override.yaml.
// Every selected file must stay inside Dir. compose-go searches the parent
// directories when Dir has no Compose file, and a file found there is rejected.
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
		return nil, ErrEmptyProjectDir
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

	err = rejectEscapingConfig(ref.Dir, options.ConfigPaths)
	if err != nil {
		return nil, err
	}

	project, err := options.LoadProject(ctx)
	if err != nil {
		return nil, fmt.Errorf("parse compose files: %w", err)
	}

	return project, nil
}

// rejectEscapingConfig checks the compose files compose-go selected after
// checkout.
//
// A repository can contain a symlink that lexical path checks accepted before
// the files existed, and compose-go can select a file from a parent directory
// when dir has none.
//
// Parameters:
//   - dir: Project directory.
//   - files: Compose files compose-go will load.
//
// Returns:
//   - error: errConfigFile when a file resolves outside dir.
func rejectEscapingConfig(dir string, files []string) error {
	for _, path := range files {
		err := symlinkEscapes(dir, path)
		if err != nil {
			return err
		}
	}

	return nil
}

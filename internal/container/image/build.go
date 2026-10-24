package image

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"

	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for image builds.
var (
	// errImageBuildFailed indicates the daemon reported a build error, or wraps a build error with redacted credentials.
	errImageBuildFailed = errors.New("image build failed")
	// errGitDockerfileEscape indicates a Dockerfile path left the build context.
	errGitDockerfileEscape = errors.New("git dockerfile path escapes context")
	// errBuildImageIDMissing indicates a successful-looking build returned no image ID.
	errBuildImageIDMissing = errors.New("image build did not return an image ID")
	// errGitRemoteEmpty indicates no clone URL was associated.
	errGitRemoteEmpty = errors.New("git remote is empty")
)

// BuildFromGit builds an image from a Git URL context.
//
// The Docker daemon clones the repository. Watchtower does not.
//
// Parameters:
//   - log: Logger for the build.
//   - ctx: Cancellation and timeout.
//   - api: Docker client that runs the build.
//   - remote: Docker Git context URL.
//   - dockerfile: Dockerfile path relative to that context. Empty means Dockerfile.
//   - tags: Image tags to apply.
//
// Returns:
//   - types.ImageID: Built image ID.
//   - error: Non-nil when the build or inspect fails.
func BuildFromGit(
	log *zerolog.Logger,
	ctx context.Context,
	api dockerClient.APIClient,
	remote, dockerfile string,
	tags []string,
) (types.ImageID, error) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", errGitRemoteEmpty
	}

	dockerfile, err := confinedDockerfile(dockerfile)
	if err != nil {
		return "", err
	}

	clogVal := log.With().
		Str("remote", redact.Remote(remote)).
		Str("dockerfile", dockerfile).
		Strs("tags", tags).
		Logger()
	clog := &clogVal

	result, err := api.ImageBuild(ctx, http.NoBody, dockerClient.ImageBuildOptions{
		RemoteContext: remote,
		Tags:          tags,
		Remove:        true,
		Dockerfile:    dockerfile,
	})
	if err != nil {
		return "", fmt.Errorf("image build: %w", redactBuildError(err, remote))
	}
	defer result.Body.Close()

	imageID, err := consumeBuildStream(result.Body)
	if err != nil {
		return "", redactBuildError(err, remote)
	}

	if imageID == "" && len(tags) > 0 {
		inspected, inspectErr := api.ImageInspect(ctx, tags[0])
		if inspectErr != nil {
			return "", fmt.Errorf("inspect built image: %w", inspectErr)
		}

		imageID = types.ImageID(inspected.ID)
	}

	if imageID == "" {
		return "", errBuildImageIDMissing
	}

	clog.Debug().
		Str("image_id", imageID.ShortID()).
		Msg("Built image from Git URL context")

	return imageID, nil
}

// confinedDockerfile rejects Dockerfile paths that leave the build context.
//
// Parameters:
//   - dockerfile: Path relative to the context. Empty means Dockerfile.
//
// Returns:
//   - string: Clean slash-separated relative path.
//   - error: Non-nil when the path is absolute or walks above the context.
func confinedDockerfile(dockerfile string) (string, error) {
	dockerfile = strings.TrimSpace(dockerfile)
	if dockerfile == "" {
		return provenance.DefaultDockerfile, nil
	}

	cleaned := path.Clean(filepath.ToSlash(dockerfile))
	if path.IsAbs(cleaned) || !filepath.IsLocal(filepath.FromSlash(cleaned)) {
		return "", fmt.Errorf("%w: %s", errGitDockerfileEscape, dockerfile)
	}

	return cleaned, nil
}

// consumeBuildStream reads a Docker build stream and returns the last image ID.
//
// Parameters:
//   - body: Build response body.
//
// Returns:
//   - types.ImageID: Last aux ID, or empty.
//   - error: Non-nil when the stream reports a build error.
func consumeBuildStream(body io.Reader) (types.ImageID, error) {
	decoder := json.NewDecoder(body)

	var lastID string

	for {
		var line struct {
			Stream string          `json:"stream"`
			Error  string          `json:"error"`
			Aux    json.RawMessage `json:"aux"`
		}

		err := decoder.Decode(&line)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return "", fmt.Errorf("read build stream: %w", err)
		}

		if line.Error != "" {
			return "", fmt.Errorf("%w: %s", errImageBuildFailed, line.Error)
		}

		if id := auxImageID(line.Aux); id != "" {
			lastID = id
		}
	}

	return types.ImageID(lastID), nil
}

// auxImageID returns the image ID from a Docker build aux payload.
//
// BuildKit may send a string trace instead of an object. Those are ignored.
//
// Parameters:
//   - aux: Raw aux JSON value.
//
// Returns:
//   - string: Image ID, or empty.
func auxImageID(aux json.RawMessage) string {
	aux = bytes.TrimSpace(aux)
	if len(aux) == 0 || aux[0] != '{' {
		return ""
	}

	var payload struct {
		ID string `json:"ID"`
	}

	err := json.Unmarshal(aux, &payload)
	if err != nil {
		return ""
	}

	return payload.ID
}

// redactBuildError removes Git credentials from a build error.
//
// The Docker daemon receives the credentialed remote and may echo it.
// The original error is returned unchanged when it contains no userinfo.
//
// Parameters:
//   - err: Build client or stream error.
//   - remote: Credentialed Git context URL sent to the daemon.
//
// Returns:
//   - error: err, or a new error with userinfo replaced.
func redactBuildError(err error, remote string) error {
	if err == nil {
		return nil
	}

	msg := redact.Userinfo(err.Error(), remote)
	if msg == err.Error() {
		return err
	}

	return fmt.Errorf("%w: %s", errImageBuildFailed, msg)
}

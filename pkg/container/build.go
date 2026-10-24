package container

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for image builds.
var (
	// errImageBuildFailed indicates the daemon reported a build error, or wraps a build error with redacted credentials.
	errImageBuildFailed = errors.New("image build failed")
	// errGitDockerfileEscape indicates a Dockerfile path left the build context.
	errGitDockerfileEscape = errors.New("git dockerfile path escapes context")
)

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
		return DefaultGitDockerfile, nil
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

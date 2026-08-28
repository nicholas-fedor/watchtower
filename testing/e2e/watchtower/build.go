package watchtower

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nicholas-fedor/watchtower/testing/e2e/docker"
)

const (
	// ImageSourceThin wraps the built binary in scratch.
	ImageSourceThin = "thin"
	// ImageSourceSelfLocal builds build/docker/Dockerfile.self-local from source.
	ImageSourceSelfLocal = "self-local"
	// permDir is the mode for artifacts/<run-id>.
	permDir = 0o750
)

// Artifacts is the per-run build output.
type Artifacts struct {
	// RunID is artifacts/<run-id>.
	RunID string
	// Dir is the absolute artifacts directory for this sitting.
	Dir string
	// Binary is the host-built Watchtower path.
	Binary string
	// SubjectBinary is the host-built subject path.
	SubjectBinary string
	// PersonaBinary is the linux e2e CLI used as the in-DinD registry mock.
	PersonaBinary string
	// ImageSource is thin or self-local.
	ImageSource string
}

// Prepare builds Watchtower and the subject binary once per sitting.
//
// Parameters:
//   - ctx: Cancellation.
//   - moduleRoot: testing/e2e directory.
//   - source: Watchtower repository root.
//   - runID: Artifact directory name.
//   - imageSource: thin or self-local.
//
// Returns:
//   - Artifacts: Built paths.
//   - error: Compile failure.
func Prepare(ctx context.Context, moduleRoot, source, runID, imageSource string) (Artifacts, error) {
	dir := filepath.Join(moduleRoot, "artifacts", runID)

	mkdirErr := os.MkdirAll(dir, permDir)
	if mkdirErr != nil {
		return Artifacts{}, fmt.Errorf("artifacts dir: %w", mkdirErr)
	}

	binary := filepath.Join(dir, "watchtower")

	buildErr := docker.BuildWatchtowerBinary(ctx, source, binary)
	if buildErr != nil {
		return Artifacts{}, fmt.Errorf("watchtower binary: %w", buildErr)
	}

	subject := filepath.Join(dir, "subject")

	subjectErr := docker.BuildSubjectBinary(ctx, moduleRoot, subject)
	if subjectErr != nil {
		return Artifacts{}, fmt.Errorf("subject binary: %w", subjectErr)
	}

	persona := filepath.Join(dir, "persona")

	personaErr := docker.BuildPersonaBinary(ctx, moduleRoot, persona)
	if personaErr != nil {
		return Artifacts{}, fmt.Errorf("persona binary: %w", personaErr)
	}

	if imageSource == "" {
		imageSource = ImageSourceThin
	}

	return Artifacts{
		RunID:         runID,
		Dir:           dir,
		Binary:        binary,
		SubjectBinary: subject,
		PersonaBinary: persona,
		ImageSource:   imageSource,
	}, nil
}

// WatchtowerSource resolves WATCHTOWER_SOURCE or the in-repo parent.
//
// Returns:
//   - string: Absolute source path.
func WatchtowerSource() string {
	return docker.DefaultWatchtowerSource()
}

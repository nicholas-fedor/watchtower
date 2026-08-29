package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/moby/moby/client"
)

const (
	// scratchDockerfile is the subject image recipe. TAG, REV, and SUBJECT_KIND are filled in.
	scratchDockerfile = "FROM scratch\nCOPY subject /subject\nENV TAG=%s REV=%s SUBJECT_KIND=%s\nEXPOSE 8080\nENTRYPOINT [\"/subject\"]\n"
	// watchtowerDockerfile is the thin Watchtower image recipe.
	watchtowerDockerfile = "FROM scratch\nCOPY watchtower /watchtower\nENTRYPOINT [\"/watchtower\"]\n"
	// anonymousRegistryAuth is base64("{}"). The Engine API requires X-Registry-Auth even for a local registry.
	anonymousRegistryAuth = "e30="
)

// SubjectImage is a tagged dummy image loaded into a worker.
type SubjectImage struct {
	// Repository is the image name without a tag.
	Repository string
	// Tag is the image tag.
	Tag string
	// Kind is the SUBJECT_KIND baked into the image.
	Kind string
}

// Ref returns repository:tag.
//
// Returns:
//   - string: Image reference.
func (s SubjectImage) Ref() string {
	return s.Repository + ":" + s.Tag
}

// BuildSubjectBinary compiles testdata/subjects for linux/amd64 with CGO off.
//
// Parameters:
//   - ctx: Cancellation.
//   - moduleRoot: testing/e2e directory.
//   - output: Destination path for the static binary.
//
// Returns:
//   - error: Compile failure.
func BuildSubjectBinary(ctx context.Context, moduleRoot, output string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", output, "./testdata/subjects")
	cmd.Dir = moduleRoot

	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")

	raw, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build subject: %w: %s", err, raw)
	}

	return nil
}

// BuildWatchtowerBinary compiles Watchtower from source.
//
// Parameters:
//   - ctx: Cancellation.
//   - source: Watchtower repository root.
//   - output: Destination path.
//
// Returns:
//   - error: Compile failure.
func BuildWatchtowerBinary(ctx context.Context, source, output string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", output, ".")
	cmd.Dir = source

	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	raw, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build watchtower: %w: %s", err, raw)
	}

	return nil
}

// ContextTar builds a Docker build context tarball.
//
// Parameters:
//   - dockerfile: Dockerfile contents.
//   - binaryName: Name of the file inside the tar (subject or watchtower).
//   - binaryPath: Host path of the static binary.
//
// Returns:
//   - io.Reader: tar stream.
//   - error: Read or tar error.
func ContextTar(dockerfile, binaryName, binaryPath string) (io.Reader, error) {
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("read binary: %w", err)
	}

	var buf bytes.Buffer

	writer := tar.NewWriter(&buf)

	dockerHeader := &tar.Header{
		Name: "Dockerfile",
		Mode: permTarFile,
		Size: int64(len(dockerfile)),
	}

	headerErr := writer.WriteHeader(dockerHeader)
	if headerErr != nil {
		return nil, fmt.Errorf("tar dockerfile header: %w", headerErr)
	}

	_, writeErr := writer.Write([]byte(dockerfile))
	if writeErr != nil {
		return nil, fmt.Errorf("tar dockerfile: %w", writeErr)
	}

	binHeader := &tar.Header{
		Name: binaryName,
		Mode: permTarExec,
		Size: int64(len(binary)),
	}

	binHeaderErr := writer.WriteHeader(binHeader)
	if binHeaderErr != nil {
		return nil, fmt.Errorf("tar binary header: %w", binHeaderErr)
	}

	_, binWriteErr := writer.Write(binary)
	if binWriteErr != nil {
		return nil, fmt.Errorf("tar binary: %w", binWriteErr)
	}

	closeErr := writer.Close()
	if closeErr != nil {
		return nil, fmt.Errorf("tar close: %w", closeErr)
	}

	return &buf, nil
}

// SubjectDockerfile renders a scratch Dockerfile for one tag/kind.
//
// Parameters:
//   - tag: Image tag printed by the HTTP handler.
//   - rev: Revision string.
//   - kind: SUBJECT_KIND.
//
// Returns:
//   - string: Dockerfile contents.
func SubjectDockerfile(tag, rev, kind string) string {
	return fmt.Sprintf(scratchDockerfile, tag, rev, kind)
}

// WatchtowerDockerfile is the thin scratch wrap around a built binary.
//
// Returns:
//   - string: Dockerfile contents.
func WatchtowerDockerfile() string {
	return watchtowerDockerfile
}

// ModuleRoot walks from cwd or this file's tree to testing/e2e.
//
// Returns:
//   - string: Absolute module directory.
//   - error: When go.mod is not found.
func ModuleRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("workdir: %w", err)
	}

	dir := cwd
	for {
		_, statErr := os.Stat(filepath.Join(dir, "go.mod"))
		if statErr == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%w: %s", ErrModuleRootNotFound, cwd)
		}

		dir = parent
	}
}

// DefaultWatchtowerSource returns ../.. from the e2e module, or WATCHTOWER_SOURCE.
//
// Returns:
//   - string: Absolute Watchtower repo root.
func DefaultWatchtowerSource() string {
	if env := os.Getenv("WATCHTOWER_SOURCE"); env != "" {
		return env
	}

	root, err := ModuleRoot()
	if err != nil {
		return "../.."
	}

	return filepath.Clean(filepath.Join(root, "..", ".."))
}

// StampRunID builds artifacts/<run-id> using UTC time and optional git sha.
//
// Parameters:
//   - now: Clock.
//   - gitSHA: Short sha or "dirty".
//
// Returns:
//   - string: Run identifier.
func StampRunID(now time.Time, gitSHA string) string {
	if gitSHA == "" {
		gitSHA = "dirty"
	}

	return now.UTC().Format("20060102T150405Z") + "-" + gitSHA
}

// PushSubject publishes a locally built e2e/app:latest to the inner registry.
//
// Watchtower pulls through the inner dockerd, so the name must be
// 127.0.0.1:5000/e2e/app:latest.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - localRef: Built image name, usually e2e/app:latest.
//
// Returns:
//   - string: Registry pull reference.
//   - error: Tag or push failure.
func PushSubject(ctx context.Context, cli *client.Client, localRef string) (string, error) {
	pullRef := SubjectPullRef()

	_, tagErr := cli.ImageTag(ctx, client.ImageTagOptions{Source: localRef, Target: pullRef})
	if tagErr != nil {
		return "", fmt.Errorf("tag %s as %s: %w", localRef, pullRef, tagErr)
	}

	pushed, pushErr := cli.ImagePush(ctx, pullRef, client.ImagePushOptions{
		RegistryAuth: anonymousRegistryAuth,
	})
	if pushErr != nil {
		return "", fmt.Errorf("push %s: %w", pullRef, pushErr)
	}
	defer pushed.Close()

	waitErr := pushed.Wait(ctx)
	if waitErr != nil {
		return "", fmt.Errorf("wait push %s: %w", pullRef, waitErr)
	}

	return pullRef, nil
}

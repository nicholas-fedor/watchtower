package run

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/testing/e2e/docker"
)

// publishSubject builds a scratch subject image inside DinD.
//
// Parameters:
//   - ctx: Cancellation.
//   - opts: Sitting resources including the subject binary.
//   - kind: SUBJECT_KIND baked into the image.
//   - generation: TAG and REV baked into the image (r1 then r2). The Docker tag stays latest.
//
// Returns:
//   - error: Build failure.
func publishSubject(ctx context.Context, opts Options, kind, generation string) error {
	dockerfile := docker.SubjectDockerfile(generation, generation, kind)

	tarStream, err := docker.ContextTar(dockerfile, "subject", opts.Artifacts.SubjectBinary)
	if err != nil {
		return fmt.Errorf("subject context tar: %w", err)
	}

	ref := subjectRepo + ":" + subjectTag

	build, buildErr := opts.Daemon.Client().ImageBuild(ctx, tarStream, client.ImageBuildOptions{
		Tags:       []string{ref},
		Dockerfile: "Dockerfile",
		Remove:     true,
	})
	if buildErr != nil {
		return fmt.Errorf("build subject %s: %w", ref, buildErr)
	}
	defer build.Body.Close()

	_, _ = io.Copy(io.Discard, build.Body)

	_, pushErr := docker.PushSubject(ctx, opts.Daemon.Client(), ref)
	if pushErr != nil {
		return fmt.Errorf("push subject: %w", pushErr)
	}

	return nil
}

// readLogs concatenates Watchtower stdout and stderr from a case directory.
//
// Parameters:
//   - caseDir: Artifact directory.
//
// Returns:
//   - string: Combined log text.
func readLogs(caseDir string) string {
	stdout, _ := os.ReadFile(filepath.Join(caseDir, "watchtower.stdout.jsonl"))
	stderr, _ := os.ReadFile(filepath.Join(caseDir, "watchtower.stderr.jsonl"))

	return string(stdout) + string(stderr)
}

// findRecreated looks up a container by name after Watchtower recreated it.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - name: Container name without a leading slash.
//
// Returns:
//   - string: Container ID.
//   - error: List failure or missing container.
func findRecreated(ctx context.Context, cli *client.Client, name string) (string, error) {
	list, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return "", fmt.Errorf("list after recreate: %w", err)
	}

	for _, item := range list.Items {
		for _, n := range item.Names {
			if strings.TrimPrefix(n, "/") == name {
				return item.ID, nil
			}
		}
	}

	return "", fmt.Errorf("%w: %s", ErrSubjectMissing, name)
}

// assertImageGone fails when imageID is still present on the inner daemon.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - imageID: Inspect Image ID from before the update.
//
// Returns:
//   - error: List failure or leftover image.
func assertImageGone(ctx context.Context, cli *client.Client, imageID string) error {
	if imageID == "" {
		return nil
	}

	listed, err := cli.ImageList(ctx, client.ImageListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list images: %w", err)
	}

	for _, img := range listed.Items {
		if img.ID == imageID || strings.HasPrefix(imageID, img.ID) || strings.HasPrefix(img.ID, imageID) {
			return errStaleImageLeft
		}
	}

	return nil
}

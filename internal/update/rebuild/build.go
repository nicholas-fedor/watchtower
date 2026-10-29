package rebuild

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container"
	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/container/oci"
	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/git/apply"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// rebuildTimeout bounds one remote image build or one Compose apply. Both run
// Dockerfile steps and pulls that can take minutes, so they are not held to the
// Git timeout, which covers Git network operations. The bound still ends a
// build or apply that the daemon never finishes.
const rebuildTimeout = 30 * time.Minute

// Errors for rebuilding images from Git.
var (
	// errGitNotAssociated indicates a Git rebuild was requested without an association.
	errGitNotAssociated = errors.New("container is not associated with a Git repository")
)

// build creates a new image for one container from its Git URL context and
// stamps the container with the commit and tag the image was built from.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Cancellation and timeout.
//   - docker: Docker client.
//   - c: Stale associated container.
//   - result: Monitor result with the target commit.
//   - params: Update parameters.
//   - progress: Session progress, or nil.
//
// Returns:
//   - error: Non-nil when association, remote URL, or the host build fails.
func (s *Session) build(
	log *zerolog.Logger,
	ctx context.Context,
	docker containerClient.Client,
	c types.Container,
	result git.CheckResult,
	params types.UpdateParams,
	progress *state.Progress,
) error {
	assoc, associated := provenance.ResolveAssociation(c, params)
	if !associated {
		return errGitNotAssociated
	}

	dockerfile, err := apply.SafeRelPath(assoc.Dockerfile)
	if err != nil {
		return fmt.Errorf("git dockerfile: %w", err)
	}

	if dockerfile == "" {
		dockerfile = provenance.DefaultDockerfile
	}

	remote, err := apply.RemoteContext(assoc.Repo, result.Commit, assoc.Context)
	if err != nil {
		return fmt.Errorf("git remote context: %w", err)
	}

	user, token := s.client.BuildAuth(assoc.Repo)

	remote, err = apply.WithAuth(remote, user, token)
	if err != nil {
		return fmt.Errorf("git remote auth: %w", err)
	}

	originalName := c.ImageName()
	tag := gitImageTag(originalName, result.Commit)
	tags := []string{tag}

	if originalName != "" && originalName != tag && validDockerTag(originalName) {
		tags = append(tags, originalName)
	}

	logFoundGitRevision(log, c, params, result.Tag, result.Commit)

	buildCtx, cancel := context.WithTimeout(ctx, rebuildTimeout)
	defer cancel()

	imageID, err := docker.BuildRemoteImage(buildCtx, remote, dockerfile, tags)
	if err != nil {
		return fmt.Errorf("build git image: %w", err)
	}

	if concrete, ok := c.(*container.Container); ok {
		provenance.ApplyGitAssociation(concrete, assoc, provenance.PersistWatch(c))

		concrete.SetImageName(tag)
		provenance.ApplyGitStamp(concrete, result.Commit, result.Tag)
	}

	s.mu.Lock()
	s.built[c.ID()] = imageID
	s.mu.Unlock()

	if progress != nil {
		progress.SetLatestImage(log, c.ID(), imageID)
		// A Git rebuild has no pulled image, so no latest image annotations are
		// passed and LatestImageVersion stays empty. The resolved Git tag is the
		// only new version available for this path.
		meta := progress.SetLatestImageMeta(log, c, params, provenance.ChangelogVars{
			Tag:    result.Tag,
			Commit: result.Commit,
		}, oci.Annotations{})

		provenance.LogChangelog(log, c, params, meta)
	}

	withGitMeta(log.Info().
		Str("container", c.Name()).
		Str("image", familiarImage(tag)).
		Str("image_id", imageID.ShortID()),
		c, params, result.Tag, result.Commit).
		Msg("Built image from Git URL context")

	return nil
}

// validDockerTag reports whether an image name is a valid Docker reference without a digest.
//
// Parameters:
//   - imageName: Docker image reference to validate.
//
// Returns:
//   - bool: True when the reference is valid and does not use a digest.
func validDockerTag(imageName string) bool {
	ref, err := reference.ParseDockerRef(imageName)
	if err != nil {
		return false
	}

	_, isDigest := ref.(reference.Digested)

	return !isDigest
}

// gitImageTag builds name:git-<shortsha> from the current image name.
//
// Parameters:
//   - imageName: Current image reference.
//   - commit: Full commit SHA.
//
// Returns:
//   - string: Tag applied to the built image.
func gitImageTag(imageName, commit string) string {
	short := shortGitSHA(commit)

	named, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		base := imageName
		if i := strings.LastIndex(base, ":"); i > 0 && !strings.Contains(base[i:], "/") {
			base = base[:i]
		}

		return base + ":git-" + short
	}

	return named.Name() + ":git-" + short
}

package staleness

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/container/oci"
	"github.com/nicholas-fedor/watchtower/internal/update/changelog"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// newDigest is the manifest digest of the new image in these tests.
const newDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

// panickingFetcher fails the test if it is ever called.
//
// The default configuration must never reach the registry, so the assertion is
// stronger than counting calls: any invocation fails outright.
type panickingFetcher struct {
	t *testing.T
}

func (f panickingFetcher) FetchDigestForTag(
	_ *zerolog.Logger,
	_ context.Context,
	_ types.Container,
	_ string,
	_ string,
	_ ...string,
) (string, error) {
	f.t.Fatal("the release tag lookup must not run without the changelog feature enabled")

	return "", nil
}

// stubFetcher returns a fixed tag for every candidate.
type stubFetcher struct {
	tag   string
	calls []string
}

func (f *stubFetcher) FetchDigestForTag(
	_ *zerolog.Logger,
	_ context.Context,
	_ types.Container,
	_ string,
	tag string,
	_ ...string,
) (string, error) {
	f.calls = append(f.calls, tag)

	return newDigest, nil
}

// newProgressWithContainer returns a progress map holding c as updated, built
// the way an update session builds it.
func newProgressWithContainer(t *testing.T, c *container.Container) *state.Progress {
	t.Helper()

	progress := state.Progress{}
	progress.AddScanned(newTestLogger(t), c, c.ImageID(), types.UpdateParams{})
	progress.MarkForUpdate(newTestLogger(t), c.ID())

	return &progress
}

// newReportContainer builds a real container so the changelog gate, which reads
// container labels, is exercised rather than bypassed.
func newReportContainer(t *testing.T, labels, imageLabels map[string]string) *container.Container {
	t.Helper()

	const name = "app"

	const image = "org/app:latest"

	imageConfig := &dockerspec.DockerOCIImageConfig{}
	imageConfig.Labels = imageLabels

	c := container.NewContainer(nil, &dockerContainer.InspectResponse{
		ID:   name,
		Name: "/" + name,
		Config: &dockerContainer.Config{
			Image:  image,
			Labels: labels,
		},
	}, &dockerImage.InspectResponse{
		ID:       "sha256:currentimage",
		Config:   imageConfig,
		RepoTags: []string{image},
	})

	require.Equal(t, types.ContainerID(name), c.ID())

	return c
}

func TestApplyLatestImageMetadataRecordsVersionWithoutEnabledFeature(t *testing.T) {
	t.Parallel()

	c := newReportContainer(t, map[string]string{
		labels.GitRepo: "https://github.com/org/app.git",
	}, map[string]string{
		oci.VersionLabel: "1.2.2",
	})

	progress := newProgressWithContainer(t, c)
	client := stubClient{annotations: oci.Annotations{Version: "1.2.3", Revision: "newrev"}}

	vars, anns := ResolveLatestImageMeta(
		newTestLogger(t),
		context.Background(),
		changelog.NewResolver(panickingFetcher{t}),
		c,
		types.UpdateParams{},
		LatestImage{Annotations: client.annotations, Digest: newDigest},
	)
	ApplyLatestImageMeta(newTestLogger(t), progress, c, types.UpdateParams{}, vars, anns)

	status := (*progress)[c.ID()]
	assert.Equal(t, "1.2.2", status.CurrentImageVersion())
	assert.Equal(t, "1.2.3", status.LatestImageVersion())
	assert.Equal(t, "newrev", status.LatestImageRevision())
	// Without the feature the changelog stays on the unversioned release index.
	assert.Equal(t, "https://github.com/org/app/releases", status.Changelog())
}

// A changelog-url label is a value, not a switch. It must not authorize a lookup.
func TestApplyLatestImageMetadataChangelogLabelDoesNotEnableProbe(t *testing.T) {
	t.Parallel()

	c := newReportContainer(t, map[string]string{
		labels.GitRepo:      "https://github.com/org/app.git",
		labels.ChangelogURL: "https://example.com/notes/{tag}",
	}, nil)

	progress := newProgressWithContainer(t, c)
	client := stubClient{annotations: oci.Annotations{Version: "1.2.3"}}

	vars, anns := ResolveLatestImageMeta(
		newTestLogger(t),
		context.Background(),
		changelog.NewResolver(panickingFetcher{t}),
		c,
		types.UpdateParams{},
		LatestImage{Annotations: client.annotations, Digest: newDigest},
	)
	ApplyLatestImageMeta(newTestLogger(t), progress, c, types.UpdateParams{}, vars, anns)

	status := (*progress)[c.ID()]
	// The label still populates the changelog. An unfilled {tag} placeholder is
	// left in place, and the redaction pass percent-encodes its braces.
	assert.Equal(t, "https://example.com/notes/%7Btag%7D", status.Changelog())
}

func TestApplyLatestImageMetadataResolvesVersionedChangelogWhenEnabled(t *testing.T) {
	t.Parallel()

	c := newReportContainer(t, map[string]string{
		labels.GitRepo: "https://github.com/org/app.git",
	}, nil)

	progress := newProgressWithContainer(t, c)
	fetcher := &stubFetcher{tag: "v1.2.3"}
	client := stubClient{annotations: oci.Annotations{Version: "1.2.3"}}

	vars, anns := ResolveLatestImageMeta(
		newTestLogger(t),
		context.Background(),
		changelog.NewResolver(fetcher),
		c,
		types.UpdateParams{EnableChangelog: true},
		LatestImage{Annotations: client.annotations, Digest: newDigest},
	)
	ApplyLatestImageMeta(newTestLogger(t), progress, c, types.UpdateParams{EnableChangelog: true}, vars, anns)

	status := (*progress)[c.ID()]
	assert.Equal(t, "https://github.com/org/app/releases/tag/v1.2.3", status.Changelog())
	assert.Equal(t, "1.2.3", status.LatestImageVersion())
}

// The per-container label enables the feature without the global flag.
func TestApplyLatestImageMetadataEnabledByLabel(t *testing.T) {
	t.Parallel()

	c := newReportContainer(t, map[string]string{
		labels.GitRepo:         "https://github.com/org/app.git",
		labels.EnableChangelog: "true",
	}, nil)

	progress := newProgressWithContainer(t, c)
	fetcher := &stubFetcher{tag: "v1.2.3"}
	client := stubClient{annotations: oci.Annotations{Version: "1.2.3"}}

	vars, anns := ResolveLatestImageMeta(
		newTestLogger(t),
		context.Background(),
		changelog.NewResolver(fetcher),
		c,
		types.UpdateParams{},
		LatestImage{Annotations: client.annotations, Digest: newDigest},
	)
	ApplyLatestImageMeta(newTestLogger(t), progress, c, types.UpdateParams{}, vars, anns)

	assert.Equal(
		t,
		"https://github.com/org/app/releases/tag/v1.2.3",
		(*progress)[c.ID()].Changelog(),
	)
}

// A probe that finds nothing must leave the unversioned link in place.
func TestApplyLatestImageMetadataFallsBackWhenProbeMisses(t *testing.T) {
	t.Parallel()

	c := newReportContainer(t, map[string]string{
		labels.GitRepo: "https://github.com/org/app.git",
	}, nil)

	progress := newProgressWithContainer(t, c)
	client := stubClient{annotations: oci.Annotations{Version: "1.2.3"}}

	vars, anns := ResolveLatestImageMeta(
		newTestLogger(t),
		context.Background(),
		changelog.NewResolver(&missFetcher{}),
		c,
		types.UpdateParams{EnableChangelog: true},
		LatestImage{Annotations: client.annotations, Digest: newDigest},
	)
	ApplyLatestImageMeta(newTestLogger(t), progress, c, types.UpdateParams{EnableChangelog: true}, vars, anns)

	assert.Equal(t, "https://github.com/org/app/releases", (*progress)[c.ID()].Changelog())
}

// TestRegistryMetadataWouldDuplicateGitChangelog shows why the update session
// resolves latest image metadata only for a container that is not Git-watched.
// A Git rebuild already logs one Changelog entry resolved from the Git tag. The
// same container driven through the registry helpers emits a second,
// unversioned entry.
func TestRegistryMetadataWouldDuplicateGitChangelog(t *testing.T) {
	t.Parallel()

	c := newReportContainer(t, map[string]string{
		labels.GitRepo: "https://github.com/org/app.git",
		labels.GitRef:  "main",
	}, nil)

	params := types.UpdateParams{EnableGitMonitoring: true, EnableChangelog: true}
	progress := newProgressWithContainer(t, c)

	var buf bytes.Buffer

	log := zerolog.New(&buf)

	vars, anns := ResolveLatestImageMeta(
		&log,
		t.Context(),
		changelog.NewResolver(panickingFetcher{t}),
		c,
		params,
		LatestImage{Annotations: oci.Annotations{Version: "10.11.6"}},
	)
	ApplyLatestImageMeta(&log, progress, c, params, vars, anns)

	assert.Equal(t, []string{"https://github.com/org/app/releases"}, changelogEntries(buf.Bytes()),
		"registry path would duplicate: %s", buf.String())
	assert.Equal(t, "https://github.com/org/app/releases", (*progress)[c.ID()].Changelog())
}

// changelogEntries returns the changelog URL of each Changelog notification
// entry in the captured log output, in order.
func changelogEntries(output []byte) []string {
	entries := []string{}

	for line := range bytes.Lines(output) {
		if len(line) == 0 {
			continue
		}

		var entry map[string]any
		if json.Unmarshal(line, &entry) != nil {
			continue
		}

		if entry["message"] == "Changelog" {
			url, _ := entry["changelog"].(string)
			entries = append(entries, url)
		}
	}

	return entries
}

// stubClient returns fixed annotations for any image reference.
type stubClient struct {
	annotations oci.Annotations
}

func (c stubClient) GetImageAnnotations(_ context.Context, _ string) oci.Annotations {
	return c.annotations
}

// missFetcher reports no tag for any candidate.
type missFetcher struct{}

func (f *missFetcher) FetchDigestForTag(
	_ *zerolog.Logger,
	_ context.Context,
	_ types.Container,
	_ string,
	_ string,
	_ ...string,
) (string, error) {
	return "", nil
}

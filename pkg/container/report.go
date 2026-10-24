package container

import (
	"github.com/nicholas-fedor/watchtower/pkg/container/git"
	"github.com/nicholas-fedor/watchtower/pkg/container/oci"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// ReportMeta holds Git and OCI fields exposed on session reports.
//
// Project-scoped fields resolve from the latest known image so a notification
// describes what the container is moving to.
type ReportMeta struct {
	GitRepo         string
	GitRef          string
	Changelog       string
	Source          string
	ImageURL        string
	Documentation   string
	CurrentVersion  string
	LatestVersion   string
	CurrentRevision string
	LatestRevision  string
}

// ChangelogVars substitutes placeholders in an explicit changelog template.
type ChangelogVars = git.ChangelogVars

// ResolveReportMeta builds notification fields from Git association and OCI annotations.
//
// Watcher association is not inferred from OCI source. The latest annotations
// describe a newly pulled image. An empty latest falls back to the running
// image, so callers that know of no update see the current image's metadata.
//
// Parameters:
//   - c: Container to inspect.
//   - params: Update parameters with image mappings and host classifications.
//   - vars: Optional new-version values for changelog placeholders.
//   - latest: OCI annotations from a newly pulled image, or empty for none.
//
// Returns:
//   - ReportMeta: Resolved metadata. Empty strings when unknown.
func ResolveReportMeta(
	c types.Container,
	params types.UpdateParams,
	vars ChangelogVars,
	latest oci.Annotations,
) ReportMeta {
	if c == nil {
		return ReportMeta{}
	}

	cur := oci.Read(c)
	anns := preferAnnotations(latest, cur)
	mapping := types.GitImage{}

	if params.GitImages != nil {
		mapping = params.GitImages[c.ImageName()]
	}

	assoc, associated := git.ResolveAssociation(c, params)

	// Report repo may fall back to OCI source. That does not enable the watcher.
	repo := redactGitURL(assoc.Repo)
	if repo == "" {
		repo = redactGitURL(mapping.Repo)
	}

	if repo == "" {
		repo = redactMetadataURL(anns.Source)
	}

	// The ref is the configured Git ref only. An OCI version is reported
	// separately, so a version is never mistaken for a ref.
	ref := ""
	if associated {
		ref = assoc.Ref
	} else if mapping.Ref != "" {
		ref = mapping.Ref
	}

	changelog := redactMetadataURL(git.Changelog(c, repo, vars))
	if changelog == "" {
		changelog = redactMetadataURL(anns.URL)
	}

	if changelog == "" {
		changelog = redactMetadataURL(anns.Documentation)
	}

	return ReportMeta{
		GitRepo:         repo,
		GitRef:          ref,
		Changelog:       changelog,
		Source:          redactMetadataURL(anns.Source),
		ImageURL:        redactMetadataURL(anns.URL),
		Documentation:   redactMetadataURL(anns.Documentation),
		CurrentVersion:  cur.Version,
		LatestVersion:   latest.Version,
		CurrentRevision: cur.Revision,
		LatestRevision:  latest.Revision,
	}
}

// preferAnnotations returns the latest image annotations, falling back to the
// current image for each field the newer image does not carry.
//
// The two images are compared per field because a maintained image may set a
// new version while a rebuilt one drops an annotation the old one still had.
//
// Parameters:
//   - latest: OCI annotations from a newly pulled image, possibly empty.
//   - cur: OCI annotations from the running image.
//
// Returns:
//   - oci.Annotations: Per-field newest known values.
func preferAnnotations(latest, cur oci.Annotations) oci.Annotations {
	return oci.Annotations{
		Source:        firstNonEmpty(latest.Source, cur.Source),
		URL:           firstNonEmpty(latest.URL, cur.URL),
		Documentation: firstNonEmpty(latest.Documentation, cur.Documentation),
		Revision:      firstNonEmpty(latest.Revision, cur.Revision),
		Version:       firstNonEmpty(latest.Version, cur.Version),
	}
}

// firstNonEmpty returns value when it is non-empty, otherwise fallback.
//
// Parameters:
//   - value: Preferred value.
//   - fallback: Value used when value is empty.
//
// Returns:
//   - string: First non-empty argument.
func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}

	return fallback
}

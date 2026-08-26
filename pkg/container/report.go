package container

import (
	"github.com/nicholas-fedor/watchtower/pkg/container/git"
	"github.com/nicholas-fedor/watchtower/pkg/container/oci"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// ReportMeta holds Git and OCI fields exposed on session reports.
type ReportMeta struct {
	GitRepo       string
	GitRef        string
	Changelog     string
	Source        string
	ImageURL      string
	Documentation string
	Revision      string
}

// ChangelogVars substitutes placeholders in an explicit changelog template.
type ChangelogVars = git.ChangelogVars

// ResolveReportMeta builds notification fields from Git association and OCI annotations.
//
// Watcher association is not inferred from OCI source.
//
// Parameters:
//   - c: Container to inspect.
//   - params: Update parameters with image mappings and host classifications.
//   - vars: Optional new-version values for changelog placeholders.
//
// Returns:
//   - ReportMeta: Resolved metadata. Empty strings when unknown.
func ResolveReportMeta(c types.Container, params types.UpdateParams, vars ChangelogVars) ReportMeta {
	if c == nil {
		return ReportMeta{}
	}

	anns := oci.Read(c)
	mapping := types.GitImage{}

	if params.GitImages != nil {
		mapping = params.GitImages[c.ImageName()]
	}

	assoc, associated := git.ResolveAssociation(c, params)

	// Report repo may fall back to OCI source. That does not enable the watcher.
	repo := assoc.Repo
	if repo == "" {
		repo = mapping.Repo
	}

	if repo == "" {
		repo = anns.Source
	}

	ref := ""
	if associated {
		ref = assoc.Ref
	} else if mapping.Ref != "" {
		ref = mapping.Ref
	}

	// A semver OCI version is useful as a display ref when Git is not associated.
	if ref == "" && oci.LooksLikeTag(anns.Version) {
		ref = anns.Version
	}

	changelog := git.Changelog(c, repo, vars)
	if changelog == "" {
		changelog = anns.URL
	}

	if changelog == "" {
		changelog = anns.Documentation
	}

	return ReportMeta{
		GitRepo:       repo,
		GitRef:        ref,
		Changelog:     changelog,
		Source:        anns.Source,
		ImageURL:      anns.URL,
		Documentation: anns.Documentation,
		Revision:      anns.Revision,
	}
}

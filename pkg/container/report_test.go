package container

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/nicholas-fedor/watchtower/pkg/container/git"
	"github.com/nicholas-fedor/watchtower/pkg/container/oci"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("ResolveReportMeta", func() {
	ginkgo.It("returns empty metadata for a nil container", func() {
		gomega.Expect(ResolveReportMeta(nil, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{})).To(gomega.Equal(ReportMeta{}))
	})

	ginkgo.It("prefers a git-repo label over mapping and OCI source", func() {
		c := MockContainer(
			WithImageName("myapp:latest"),
			WithLabels(map[string]string{
				git.RepoLabel:         "https://github.com/org/from-label.git",
				git.RefLabel:          "release",
				git.ChangelogURLLabel: "https://example.com/changelog/{tag}",
			}),
			WithImageLabels(map[string]string{
				oci.SourceLabel: "https://github.com/org/from-oci.git",
			}),
		)

		meta := ResolveReportMeta(c, types.UpdateParams{
			GitImages: map[string]types.GitImage{
				"myapp:latest": {
					Repo: "https://github.com/org/from-map.git",
					Ref:  "mapped",
				},
			},
		}, ChangelogVars{Tag: "v1.2.3"}, oci.Annotations{})

		gomega.Expect(meta.GitRepo).To(gomega.Equal("https://github.com/org/from-label.git"))
		gomega.Expect(meta.GitRef).To(gomega.Equal("release"))
		gomega.Expect(meta.Changelog).To(gomega.Equal("https://example.com/changelog/v1.2.3"))
		gomega.Expect(meta.Source).To(gomega.Equal("https://github.com/org/from-oci.git"))
	})

	ginkgo.It("preserves bare local Git repository paths", func() {
		c := MockContainer(
			WithImageName("app:latest"),
			WithLabels(map[string]string{
				git.RepoLabel: "../repos/app?release=1",
			}),
		)

		meta := ResolveReportMeta(c, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{})
		gomega.Expect(meta.GitRepo).To(gomega.Equal("../repos/app?release=1"))
	})

	ginkgo.It("redacts credentials and query tokens from Git metadata", func() {
		c := MockContainer(
			WithImageName("app:latest"),
			WithLabels(map[string]string{
				git.RepoLabel:         "https://user:secret@github.com/org/app.git?access_token=query#main",
				git.ChangelogURLLabel: "https://example.com/notes?version=2#install",
			}),
			WithImageLabels(map[string]string{
				oci.SourceLabel:        "https://source:source-secret@example.com/org/source.git?token=source-query",
				oci.URLLabel:           "https://example.com/image?format=json&author=team#download",
				oci.DocumentationLabel: "https://example.com/docs?lang=en#install",
			}),
		)

		meta := ResolveReportMeta(c, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{})
		gomega.Expect(meta.GitRepo).To(gomega.Equal("https://github.com/org/app.git#main"))
		gomega.Expect(meta.GitRepo).NotTo(gomega.ContainSubstring("secret"))
		gomega.Expect(meta.GitRepo).NotTo(gomega.ContainSubstring("query"))
		gomega.Expect(meta.Changelog).To(gomega.Equal("https://example.com/notes?version=2#install"))
		gomega.Expect(meta.Source).To(gomega.Equal("https://example.com/org/source.git"))
		gomega.Expect(meta.ImageURL).To(gomega.Equal("https://example.com/image?format=json&author=team#download"))
		gomega.Expect(meta.Documentation).To(gomega.Equal("https://example.com/docs?lang=en#install"))
	})

	ginkgo.It("uses an image mapping then OCI source when labels are absent", func() {
		c := MockContainer(
			WithImageName("myapp:latest"),
			WithImageLabels(map[string]string{
				oci.SourceLabel:        "https://github.com/org/from-oci.git",
				oci.URLLabel:           "https://example.com/image",
				oci.DocumentationLabel: "https://example.com/docs",
				oci.RevisionLabel:      "abc123",
				oci.VersionLabel:       "v2.0.0",
			}),
		)

		mapped := ResolveReportMeta(c, types.UpdateParams{
			GitImages: map[string]types.GitImage{
				"myapp:latest": {Repo: "https://github.com/org/from-map.git", Ref: "mapped"},
			},
		}, ChangelogVars{}, oci.Annotations{})
		gomega.Expect(mapped.GitRepo).To(gomega.Equal("https://github.com/org/from-map.git"))
		gomega.Expect(mapped.GitRef).To(gomega.Equal("mapped"))
		gomega.Expect(mapped.Changelog).To(gomega.Equal("https://github.com/org/from-map/releases"))

		ociOnly := ResolveReportMeta(c, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{})
		gomega.Expect(ociOnly.GitRepo).To(gomega.Equal("https://github.com/org/from-oci.git"))
		gomega.Expect(ociOnly.GitRef).To(gomega.BeEmpty())
		gomega.Expect(ociOnly.Changelog).To(gomega.Equal("https://github.com/org/from-oci/releases"))
		gomega.Expect(ociOnly.Source).To(gomega.Equal("https://github.com/org/from-oci.git"))
		gomega.Expect(ociOnly.ImageURL).To(gomega.Equal("https://example.com/image"))
		gomega.Expect(ociOnly.Documentation).To(gomega.Equal("https://example.com/docs"))
		gomega.Expect(ociOnly.CurrentRevision).To(gomega.Equal("abc123"))
		gomega.Expect(ociOnly.LatestRevision).To(gomega.BeEmpty())
		gomega.Expect(ociOnly.CurrentVersion).To(gomega.Equal("v2.0.0"))
		gomega.Expect(ociOnly.LatestVersion).To(gomega.BeEmpty())
	})

	ginkgo.It("reports current and latest versions from separate images", func() {
		c := MockContainer(
			WithImageName("app:latest"),
			WithImageLabels(map[string]string{
				oci.RevisionLabel: "old123",
				oci.VersionLabel:  "10.11.5",
			}),
		)

		meta := ResolveReportMeta(c, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{
			Revision: "new456",
			Version:  "10.11.6",
		})

		gomega.Expect(meta.CurrentVersion).To(gomega.Equal("10.11.5"))
		gomega.Expect(meta.LatestVersion).To(gomega.Equal("10.11.6"))
		gomega.Expect(meta.CurrentRevision).To(gomega.Equal("old123"))
		gomega.Expect(meta.LatestRevision).To(gomega.Equal("new456"))
	})

	ginkgo.It("resolves project-scoped fields from the latest image", func() {
		c := MockContainer(
			WithImageName("app:latest"),
			WithImageLabels(map[string]string{
				oci.SourceLabel:        "https://github.com/org/old.git",
				oci.URLLabel:           "https://example.com/old",
				oci.DocumentationLabel: "https://example.com/old-docs",
			}),
		)

		meta := ResolveReportMeta(c, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{
			Source:        "https://github.com/org/new.git",
			URL:           "https://example.com/new",
			Documentation: "https://example.com/new-docs",
			Version:       "1.0.0",
		})

		gomega.Expect(meta.GitRepo).To(gomega.Equal("https://github.com/org/new.git"))
		gomega.Expect(meta.Source).To(gomega.Equal("https://github.com/org/new.git"))
		gomega.Expect(meta.ImageURL).To(gomega.Equal("https://example.com/new"))
		gomega.Expect(meta.Documentation).To(gomega.Equal("https://example.com/new-docs"))
		gomega.Expect(meta.Changelog).To(gomega.Equal("https://github.com/org/new/releases"))
	})

	ginkgo.It("falls back per field when the latest image omits an annotation", func() {
		c := MockContainer(
			WithImageName("app:latest"),
			WithImageLabels(map[string]string{
				oci.SourceLabel: "https://github.com/org/app.git",
				oci.URLLabel:    "https://example.com/image",
			}),
		)

		meta := ResolveReportMeta(c, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{Version: "1.0.0"})

		gomega.Expect(meta.Source).To(gomega.Equal("https://github.com/org/app.git"))
		gomega.Expect(meta.ImageURL).To(gomega.Equal("https://example.com/image"))
		gomega.Expect(meta.LatestVersion).To(gomega.Equal("1.0.0"))
	})

	ginkgo.It("derives a versioned changelog when a release tag is known", func() {
		c := MockContainer(
			WithImageName("app:latest"),
			WithImageLabels(map[string]string{
				oci.SourceLabel:  "https://github.com/org/app.git",
				oci.VersionLabel: "10.11.6",
			}),
		)

		meta := ResolveReportMeta(c, types.UpdateParams{}, ChangelogVars{Tag: "v10.11.6"}, oci.Annotations{
			Version: "10.11.6",
		})
		gomega.Expect(meta.Changelog).To(gomega.Equal("https://github.com/org/app/releases/tag/v10.11.6"))
	})

	ginkgo.It("derives changelog URLs for known and mapped hosts", func() {
		gitlab := MockContainer(WithImageName("app:latest"), WithLabels(map[string]string{
			git.RepoLabel: "https://gitlab.com/org/app.git",
		}))
		gomega.Expect(ResolveReportMeta(gitlab, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{}).Changelog).
			To(gomega.Equal("https://gitlab.com/org/app/-/releases"))

		codeberg := MockContainer(WithImageName("app:latest"), WithLabels(map[string]string{
			git.RepoLabel: "https://codeberg.org/org/app.git",
		}))
		gomega.Expect(ResolveReportMeta(codeberg, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{}).Changelog).
			To(gomega.Equal("https://codeberg.org/org/app/releases"))

		mappedHost := MockContainer(WithImageName("app:latest"), WithLabels(map[string]string{
			git.RepoLabel: "git@git.example.com:org/app.git",
			git.HostLabel: "https://git.example.com:3000/gitea",
		}))
		gomega.Expect(ResolveReportMeta(mappedHost, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{}).Changelog).
			To(gomega.Equal("https://git.example.com:3000/gitea/org/app/releases"))

		ghe := MockContainer(WithImageName("app:latest"), WithLabels(map[string]string{
			git.RepoLabel: "https://github.company.com/org/app.git",
			git.HostLabel: "https://github.company.com/github",
		}))
		gomega.Expect(ResolveReportMeta(ghe, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{}).Changelog).
			To(gomega.Equal("https://github.company.com/github/org/app/releases"))

		nested := MockContainer(WithImageName("app:latest"), WithLabels(map[string]string{
			git.RepoLabel: "https://gitlab.com/group/sub/repo.git",
		}))
		gomega.Expect(ResolveReportMeta(nested, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{}).Changelog).
			To(gomega.Equal("https://gitlab.com/group/sub/repo/-/releases"))
	})

	ginkgo.It("redacts credentials from the new image's annotations", func() {
		c := MockContainer(
			WithImageName("app:latest"),
			WithImageLabels(map[string]string{
				oci.SourceLabel: "https://github.com/org/old.git",
			}),
		)

		meta := ResolveReportMeta(c, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{
			Source:        "https://newuser:newscret@github.com/org/new.git?token=query#main",
			URL:           "https://example.com/image?signature=abc&format=json",
			Documentation: "https://example.com/docs?lang=en#install",
		})

		// The new image's values are the ones reported, so redaction must reach
		// them. Userinfo goes and sensitive query keys go, benign ones stay.
		gomega.Expect(meta.GitRepo).To(gomega.Equal("https://github.com/org/new.git#main"))
		gomega.Expect(meta.GitRepo).NotTo(gomega.ContainSubstring("newscret"))
		gomega.Expect(meta.GitRepo).NotTo(gomega.ContainSubstring("query"))
		gomega.Expect(meta.ImageURL).To(gomega.Equal("https://example.com/image?format=json"))
		gomega.Expect(meta.ImageURL).NotTo(gomega.ContainSubstring("signature"))
		gomega.Expect(meta.Documentation).To(gomega.Equal("https://example.com/docs?lang=en#install"))
	})

	ginkgo.It("does not persist an SCP URL whose user is a token", func() {
		gomega.Expect(redactGitURL("token@github.com:org/app.git")).To(gomega.BeEmpty())
		gomega.Expect(redactGitURL("ghp_abcdefghijklmnopqrstuvwxyz@github.com:org/app.git")).To(gomega.BeEmpty())
		gomega.Expect(redactGitURL("git@github.com:org/app.git")).To(gomega.Equal("git@github.com:org/app.git"))
		gomega.Expect(redactGitURL("deploy@git.example.com:org/app.git")).To(gomega.Equal("deploy@git.example.com:org/app.git"))
	})

	ginkgo.It("leaves changelog empty for an unknown host without OCI URL fields", func() {
		c := MockContainer(WithImageName("app:latest"), WithLabels(map[string]string{
			git.RepoLabel: "https://unknown.example/org/app.git",
		}))
		gomega.Expect(ResolveReportMeta(c, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{}).Changelog).To(gomega.BeEmpty())
	})

	ginkgo.It("falls back to OCI URL then documentation for changelog", func() {
		urlOnly := MockContainer(
			WithImageName("app:latest"),
			WithImageLabels(map[string]string{
				oci.URLLabel: "https://example.com/image",
			}),
		)
		gomega.Expect(ResolveReportMeta(urlOnly, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{}).Changelog).
			To(gomega.Equal("https://example.com/image"))

		docsOnly := MockContainer(
			WithImageName("app:latest"),
			WithImageLabels(map[string]string{
				oci.DocumentationLabel: "https://example.com/docs",
			}),
		)
		gomega.Expect(ResolveReportMeta(docsOnly, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{}).Changelog).
			To(gomega.Equal("https://example.com/docs"))
	})

	ginkgo.It("uses a mapping ref when the container is not watcher-associated", func() {
		c := MockContainer(WithImageName("myapp:latest"))

		meta := ResolveReportMeta(c, types.UpdateParams{
			GitImages: map[string]types.GitImage{
				"myapp:latest": {Ref: "from-map"},
			},
		}, ChangelogVars{}, oci.Annotations{})
		gomega.Expect(meta.GitRepo).To(gomega.BeEmpty())
		gomega.Expect(meta.GitRef).To(gomega.Equal("from-map"))
	})

	ginkgo.It("never uses an OCI version as a display ref", func() {
		c := MockContainer(
			WithImageName("app:latest"),
			WithImageLabels(map[string]string{
				oci.VersionLabel: "1.4.0",
			}),
		)

		meta := ResolveReportMeta(c, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{})
		gomega.Expect(meta.GitRef).To(gomega.BeEmpty())
		gomega.Expect(meta.CurrentVersion).To(gomega.Equal("1.4.0"))
	})

	ginkgo.It("does not treat OCI source as a watcher association", func() {
		c := MockContainer(
			WithImageName("app:latest"),
			WithImageLabels(map[string]string{
				oci.SourceLabel: "https://github.com/org/from-oci.git",
			}),
		)

		meta := ResolveReportMeta(c, types.UpdateParams{}, ChangelogVars{}, oci.Annotations{})
		gomega.Expect(meta.GitRepo).To(gomega.Equal("https://github.com/org/from-oci.git"))

		assoc, ok := git.ResolveAssociation(c, types.UpdateParams{})
		gomega.Expect(ok).To(gomega.BeFalse())
		gomega.Expect(assoc).To(gomega.Equal(git.Association{}))
	})
})

package actions

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"

	dockerContainer "github.com/moby/moby/api/types/container"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/internal/compose"
	mockCompose "github.com/nicholas-fedor/watchtower/internal/compose/mocks"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/git/project"
	gitPkg "github.com/nicholas-fedor/watchtower/pkg/container/git"
	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/session"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func writeComposeProjectDir() string {
	dir := ginkgo.GinkgoT().TempDir()
	gomega.Expect(os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  api:\n    build: .\n  worker:\n    build: ./worker\n"), 0o600)).To(gomega.Succeed())

	return dir
}

func gitTestLogger() *zerolog.Logger {
	nop := zerolog.Nop()

	return &nop
}

func gitTestClient() *git.Client {
	return git.New(gitTestLogger(), git.Options{Timeout: time.Second})
}

func gitAssociatedContainer(id, name, image string, extra map[string]string) types.Container {
	labels := map[string]string{
		gitPkg.RepoLabel: "https://github.com/org/app.git",
		gitPkg.RefLabel:  "main",
	}

	maps.Copy(labels, extra)

	return mockActions.CreateMockContainerWithConfig(
		id,
		name,
		image,
		true,
		false,
		time.Now().Add(-time.Hour),
		&dockerContainer.Config{
			Image:  image,
			Labels: labels,
		},
	)
}

var _ = ginkgo.Describe("gitSession", ginkgo.Label("git-session"), func() {
	ginkgo.Describe("newGitSession", func() {
		ginkgo.It("returns empty result and built maps", func() {
			client := gitTestClient()
			sess := newGitSession(client)

			gomega.Expect(sess).NotTo(gomega.BeNil())
			gomega.Expect(sess.client).To(gomega.Equal(client))
			gomega.Expect(sess.results).To(gomega.BeEmpty())
			gomega.Expect(sess.built).To(gomega.BeEmpty())
		})
	})

	ginkgo.Describe("store and result", func() {
		ginkgo.It("does nothing when the session is nil", func() {
			var sess *gitSession

			sess.store("abc", git.CheckResult{Stale: true, Commit: "deadbeef"})
			got, ok := sess.result("abc")
			gomega.Expect(ok).To(gomega.BeFalse())
			gomega.Expect(got).To(gomega.Equal(git.CheckResult{}))
		})

		ginkgo.It("stores and retrieves a check result", func() {
			sess := newGitSession(gitTestClient())
			want := git.CheckResult{Stale: true, Commit: "abc123", Tag: "v1.2.3", Kind: "tag", Ref: "v1.2.3"}

			sess.store("c1", want)
			got, ok := sess.result("c1")
			gomega.Expect(ok).To(gomega.BeTrue())
			gomega.Expect(got).To(gomega.Equal(want))
		})

		ginkgo.It("returns false for an unknown container", func() {
			sess := newGitSession(gitTestClient())
			_, ok := sess.result("missing")
			gomega.Expect(ok).To(gomega.BeFalse())
		})
	})

	ginkgo.Describe("watching", func() {
		ginkgo.It("is false when the session is nil", func() {
			var sess *gitSession

			c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)

			gomega.Expect(sess.watching(gitTestLogger(), c, types.UpdateParams{EnableGitMonitoring: true})).
				To(gomega.BeFalse())
		})

		ginkgo.It("is false when the client is nil", func() {
			sess := &gitSession{}
			c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)

			gomega.Expect(sess.watching(gitTestLogger(), c, types.UpdateParams{EnableGitMonitoring: true})).
				To(gomega.BeFalse())
		})

		ginkgo.It("is true for an associated container when monitoring is enabled", func() {
			sess := newGitSession(gitTestClient())
			c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)

			gomega.Expect(sess.watching(gitTestLogger(), c, types.UpdateParams{EnableGitMonitoring: true})).
				To(gomega.BeTrue())
		})

		ginkgo.It("is false when git-watch is false", func() {
			sess := newGitSession(gitTestClient())
			c := gitAssociatedContainer("id", "/app", "myapp:latest", map[string]string{
				gitPkg.WatchLabel: "false",
			})

			gomega.Expect(sess.watching(gitTestLogger(), c, types.UpdateParams{EnableGitMonitoring: true})).
				To(gomega.BeFalse())
		})

		ginkgo.It("is false for an unassociated container", func() {
			sess := newGitSession(gitTestClient())
			c := mockActions.CreateMockContainer("id", "/app", "myapp:latest", time.Now())

			gomega.Expect(sess.watching(gitTestLogger(), c, types.UpdateParams{EnableGitMonitoring: true})).
				To(gomega.BeFalse())
		})

		ginkgo.It("is false for Watchtower itself", func() {
			sess := newGitSession(gitTestClient())
			c := mockActions.CreateMockContainerWithConfig(
				"wt",
				"/watchtower",
				"nickfedor/watchtower:latest",
				true,
				false,
				time.Now(),
				&dockerContainer.Config{
					Image: "nickfedor/watchtower:latest",
					Labels: map[string]string{
						"com.centurylinklabs.watchtower": "true",
						gitPkg.RepoLabel:                 "https://github.com/nicholas-fedor/watchtower.git",
						gitPkg.WatchLabel:                "true",
					},
				},
			)

			gomega.Expect(sess.watching(gitTestLogger(), c, types.UpdateParams{EnableGitMonitoring: true})).
				To(gomega.BeFalse())
		})
	})

	ginkgo.Describe("check", func() {
		ginkgo.It("treats an unassociated container as fresh and stores the empty result", func() {
			sess := newGitSession(gitTestClient())
			c := mockActions.CreateMockContainer("plain", "/plain", "nginx:latest", time.Now())

			stale, newest, err := sess.check(ginkgo.GinkgoT().Context(), c, types.UpdateParams{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(stale).To(gomega.BeFalse())
			gomega.Expect(newest).To(gomega.Equal(c.ImageID()))

			stored, ok := sess.result(c.ID())
			gomega.Expect(ok).To(gomega.BeTrue())
			gomega.Expect(stored.Stale).To(gomega.BeFalse())
		})

		ginkgo.It("returns a wrapped error when the remote cannot be listed", func() {
			sess := newGitSession(git.New(gitTestLogger(), git.Options{Timeout: 50 * time.Millisecond}))
			c := gitAssociatedContainer("bad", "/bad", "myapp:latest", map[string]string{
				gitPkg.RepoLabel: "https://127.0.0.1:1/org/app.git",
			})

			ctx, cancel := context.WithTimeout(ginkgo.GinkgoT().Context(), 200*time.Millisecond)
			defer cancel()

			stale, newest, err := sess.check(ctx, c, types.UpdateParams{
				EnableGitMonitoring: true,
			})
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("git check:"))
			gomega.Expect(stale).To(gomega.BeFalse())
			gomega.Expect(newest).To(gomega.BeEmpty())
		})
	})

	ginkgo.Describe("prepareRebuilds", func() {
		ginkgo.It("returns immediately when the session or client is nil", func() {
			failed := map[types.ContainerID]error{}
			c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
			c.SetStale(true)

			var sess *gitSession
			sess.prepareRebuilds(
				gitTestLogger(),
				ginkgo.GinkgoT().Context(),
				mockActions.CreateMockClient(&mockActions.TestData{}, false, false),
				[]types.Container{c},
				types.UpdateParams{},
				nil,
				failed,
			)
			gomega.Expect(failed).To(gomega.BeEmpty())

			sess = &gitSession{results: map[types.ContainerID]git.CheckResult{}}
			sess.prepareRebuilds(
				gitTestLogger(),
				ginkgo.GinkgoT().Context(),
				mockActions.CreateMockClient(&mockActions.TestData{}, false, false),
				[]types.Container{c},
				types.UpdateParams{},
				nil,
				failed,
			)
			gomega.Expect(failed).To(gomega.BeEmpty())
		})

		ginkgo.It("skips containers that are not stale or have no stored stale result", func() {
			sess := newGitSession(gitTestClient())
			sess.apply = mockCompose.NewMockApplier(ginkgo.GinkgoT())
			fresh := gitAssociatedContainer("fresh", "/fresh", "myapp:latest", nil)
			staleNoResult := gitAssociatedContainer("none", "/none", "myapp:latest", nil)
			staleFreshResult := gitAssociatedContainer("stored", "/stored", "myapp:latest", nil)

			staleNoResult.SetStale(true)
			staleFreshResult.SetStale(true)
			sess.store(staleFreshResult.ID(), git.CheckResult{Stale: false})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{fresh, staleNoResult, staleFreshResult}, types.UpdateParams{}, nil, failed)

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(docker.TestData.TriedToRemoveImageCount.Load()).To(gomega.Equal(int32(0)))
		})

		ginkgo.It("skips monitor-only and no-pull containers", func() {
			sess := newGitSession(gitTestClient())
			sess.apply = mockCompose.NewMockApplier(ginkgo.GinkgoT())
			monitor := gitAssociatedContainer("mon", "/mon", "myapp:latest", map[string]string{
				"com.centurylinklabs.watchtower.monitor-only": "true",
			})
			noPull := gitAssociatedContainer("np", "/np", "myapp:latest", map[string]string{
				"com.centurylinklabs.watchtower.no-pull": "true",
			})

			monitor.SetStale(true)
			noPull.SetStale(true)
			sess.store(monitor.ID(), git.CheckResult{Stale: true, Commit: "abc123abc123"})
			sess.store(noPull.ID(), git.CheckResult{Stale: true, Commit: "abc123abc123"})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{monitor, noPull}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(sess.built).To(gomega.BeEmpty())
		})

		ginkgo.It("records a failed build and unmarks the container stale", func() {
			sess := newGitSession(gitTestClient())
			sess.apply = mockCompose.NewMockApplier(ginkgo.GinkgoT())
			c := gitAssociatedContainer("fail", "/fail", "myapp:latest", nil)
			c.SetStale(true)
			sess.store(c.ID(), git.CheckResult{Stale: true, Commit: ""})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{c}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

			gomega.Expect(failed).To(gomega.HaveKey(c.ID()))
			gomega.Expect(c.IsStale()).To(gomega.BeFalse())
		})

		ginkgo.It("builds a stale associated container", func() {
			sess := newGitSession(gitTestClient())
			sess.apply = mockCompose.NewMockApplier(ginkgo.GinkgoT())
			c := gitAssociatedContainer("ok", "/ok", "myapp:latest", nil)
			c.SetStale(true)
			sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "0123456789abcdef0123456789abcdef01234567", Tag: "v1.0.0"})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			progress := &session.Progress{}
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{c}, types.UpdateParams{EnableGitMonitoring: true}, progress, failed)

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(sess.built).To(gomega.HaveKey(c.ID()))
			gomega.Expect(c.ImageName()).To(gomega.HavePrefix("docker.io/library/myapp:git-"))
			commit, tag := gitPkg.LastStamp(c)
			gomega.Expect(commit).To(gomega.Equal("0123456789abcdef0123456789abcdef01234567"))
			gomega.Expect(tag).To(gomega.Equal("v1.0.0"))
		})
	})

	ginkgo.Describe("buildOne", func() {
		ginkgo.It("returns errGitNotAssociated when the container has no repo", func() {
			sess := newGitSession(gitTestClient())
			c := mockActions.CreateMockContainer("plain", "/plain", "nginx:latest", time.Now())
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

			err := sess.buildOne(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, c, git.CheckResult{Commit: "abc"}, types.UpdateParams{}, nil)
			gomega.Expect(err).To(gomega.MatchError(errGitNotAssociated))
		})

		ginkgo.It("returns an error when the commit is empty", func() {
			sess := newGitSession(gitTestClient())
			c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

			err := sess.buildOne(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, c, git.CheckResult{Stale: true}, types.UpdateParams{EnableGitMonitoring: true}, nil)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("git remote context:"))
		})

		ginkgo.It("returns an error when the build context is cancelled", func() {
			sess := newGitSession(gitTestClient())
			c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			ctx, cancel := context.WithCancel(ginkgo.GinkgoT().Context())
			cancel()

			err := sess.buildOne(gitTestLogger(), ctx, docker, c, git.CheckResult{Stale: true, Commit: "abc123abc123"}, types.UpdateParams{EnableGitMonitoring: true}, nil)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("build git image:"))
		})

		ginkgo.It("keeps a nested dockerfile and context on the rebuilt container", func() {
			sess := newGitSession(gitTestClient())
			c := gitAssociatedContainer("id", "/app", "myapp:latest", map[string]string{
				gitPkg.DockerfileLabel: "build/docker/Dockerfile",
				gitPkg.ContextLabel:    ".",
			})
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

			err := sess.buildOne(
				gitTestLogger(),
				ginkgo.GinkgoT().Context(),
				docker,
				c,
				git.CheckResult{Stale: true, Commit: "ffffffffffffffffffffffffffffffffffffffff"},
				types.UpdateParams{EnableGitMonitoring: true},
				nil,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			assoc, ok := gitPkg.ResolveAssociation(c, types.UpdateParams{EnableGitMonitoring: true})
			gomega.Expect(ok).To(gomega.BeTrue())
			gomega.Expect(assoc.Dockerfile).To(gomega.Equal("build/docker/Dockerfile"))
			gomega.Expect(assoc.Context).To(gomega.Equal("."))

			_, watchSet := c.GetLabel(gitPkg.WatchLabel)
			gomega.Expect(watchSet).To(gomega.BeFalse())
		})

		ginkgo.It("persists git-watch only when the source label enabled it", func() {
			sess := newGitSession(gitTestClient())
			c := gitAssociatedContainer("id", "/app", "myapp:latest", map[string]string{
				gitPkg.WatchLabel: "yes",
			})
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)

			err := sess.buildOne(
				gitTestLogger(),
				ginkgo.GinkgoT().Context(),
				docker,
				c,
				git.CheckResult{Stale: true, Commit: "ffffffffffffffffffffffffffffffffffffffff"},
				types.UpdateParams{},
				nil,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			got, ok := c.GetLabel(gitPkg.WatchLabel)
			gomega.Expect(ok).To(gomega.BeTrue())
			gomega.Expect(got).To(gomega.Equal("true"))
		})
		ginkgo.It("does not pass a digest-pinned image as a build tag", func() {
			sess := newGitSession(gitTestClient())
			image := "ghcr.io/org/app@sha256:" + strings.Repeat("a", 64)
			commit := "0123456789abcdef0123456789abcdef01234567"
			c := gitAssociatedContainer("digest", "/digest", image, nil)

			var tags []string

			docker := mockContainer.NewMockClient(ginkgo.GinkgoT())
			docker.EXPECT().
				BuildRemoteImage(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Run(func(_ context.Context, _, _ string, got []string) {
					tags = append([]string(nil), got...)
				}).
				Return(types.ImageID("sha256:built"), nil)

			err := sess.buildOne(
				gitTestLogger(),
				ginkgo.GinkgoT().Context(),
				docker,
				c,
				git.CheckResult{Stale: true, Commit: commit},
				types.UpdateParams{EnableGitMonitoring: true},
				nil,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(tags).To(gomega.HaveLen(1))
			gomega.Expect(tags[0]).To(gomega.Equal("ghcr.io/org/app:git-0123456789ab"))
			gomega.Expect(tags[0]).NotTo(gomega.ContainSubstring("@"))
		})

		ginkgo.It("applies the Git timeout to the URL-context build", func() {
			sess := newGitSession(git.New(gitTestLogger(), git.Options{Timeout: 50 * time.Millisecond}))
			commit := "0123456789abcdef0123456789abcdef01234567"
			c := gitAssociatedContainer("timeout", "/timeout", "myapp:latest", nil)

			docker := mockContainer.NewMockClient(ginkgo.GinkgoT())
			docker.EXPECT().
				BuildRemoteImage(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Run(func(ctx context.Context, _, _ string, _ []string) {
					deadline, ok := ctx.Deadline()
					gomega.Expect(ok).To(gomega.BeTrue())
					gomega.Expect(time.Until(deadline)).To(gomega.BeNumerically("<=", 50*time.Millisecond))
				}).
				Return(types.ImageID("sha256:built"), nil)

			err := sess.buildOne(
				gitTestLogger(),
				ginkgo.GinkgoT().Context(),
				docker,
				c,
				git.CheckResult{Stale: true, Commit: commit},
				types.UpdateParams{EnableGitMonitoring: true},
				nil,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("keeps a build that returns nil after its timeout elapses", func() {
			sess := newGitSession(git.New(gitTestLogger(), git.Options{Timeout: 10 * time.Millisecond}))
			c := gitAssociatedContainer("timeout", "/timeout", "myapp:latest", nil)
			commit := "0123456789abcdef0123456789abcdef01234567"

			docker := mockContainer.NewMockClient(ginkgo.GinkgoT())
			docker.EXPECT().
				BuildRemoteImage(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				RunAndReturn(func(ctx context.Context, _, _ string, _ []string) (types.ImageID, error) {
					<-ctx.Done()

					return "sha256:built", nil
				}).
				Once()

			err := sess.buildOne(
				gitTestLogger(),
				ginkgo.GinkgoT().Context(),
				docker,
				c,
				git.CheckResult{Stale: true, Commit: commit},
				types.UpdateParams{EnableGitMonitoring: true},
				nil,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(sess.built[c.ID()]).To(gomega.Equal(types.ImageID("sha256:built")))
		})
	})

	ginkgo.Describe("skipRecreate", func() {
		ginkgo.It("is false when the session is nil", func() {
			var sess *gitSession

			c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)

			gomega.Expect(sess.skipRecreate(c, types.UpdateParams{NoRestart: true})).To(gomega.BeFalse())
		})

		ginkgo.It("is false when no-restart is off", func() {
			sess := newGitSession(gitTestClient())
			c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
			sess.built[c.ID()] = "sha256:git"

			gomega.Expect(sess.skipRecreate(c, types.UpdateParams{})).To(gomega.BeFalse())
		})

		ginkgo.It("is false for Watchtower even when an image was built", func() {
			sess := newGitSession(gitTestClient())
			c := mockActions.CreateMockContainerWithConfig(
				"wt",
				"/watchtower",
				"nickfedor/watchtower:latest",
				true,
				false,
				time.Now(),
				&dockerContainer.Config{
					Image: "nickfedor/watchtower:latest",
					Labels: map[string]string{
						"com.centurylinklabs.watchtower": "true",
					},
				},
			)
			sess.built[c.ID()] = "sha256:git"

			gomega.Expect(sess.skipRecreate(c, types.UpdateParams{NoRestart: true})).To(gomega.BeFalse())
		})

		ginkgo.It("is true when no-restart is on and this session built the image", func() {
			sess := newGitSession(gitTestClient())
			c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
			sess.built[c.ID()] = "sha256:git"

			gomega.Expect(sess.skipRecreate(c, types.UpdateParams{NoRestart: true})).To(gomega.BeTrue())
		})
	})

	ginkgo.Describe("excludeNoRestart", func() {
		ginkgo.It("returns the original list when the session is nil or no-restart is off", func() {
			c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
			list := []types.Container{c}

			var sess *gitSession
			gomega.Expect(sess.excludeNoRestart(list, types.UpdateParams{NoRestart: true})).To(gomega.Equal(list))

			sess = newGitSession(gitTestClient())
			gomega.Expect(sess.excludeNoRestart(list, types.UpdateParams{})).To(gomega.Equal(list))
		})

		ginkgo.It("drops containers this session built when no-restart is on", func() {
			sess := newGitSession(gitTestClient())
			built := gitAssociatedContainer("built", "/built", "myapp:latest", nil)
			kept := gitAssociatedContainer("kept", "/kept", "other:latest", nil)
			sess.built[built.ID()] = "sha256:git"

			got := sess.excludeNoRestart([]types.Container{built, kept}, types.UpdateParams{NoRestart: true})
			gomega.Expect(got).To(gomega.HaveLen(1))
			gomega.Expect(got[0].ID()).To(gomega.Equal(kept.ID()))
		})
	})

	ginkgo.Describe("excludeApplied", func() {
		ginkgo.It("returns the original list when the session is nil or nothing was applied", func() {
			c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
			list := []types.Container{c}

			var sess *gitSession
			gomega.Expect(sess.excludeApplied(list)).To(gomega.Equal(list))

			sess = newGitSession(gitTestClient())
			gomega.Expect(sess.excludeApplied(list)).To(gomega.Equal(list))
		})

		ginkgo.It("drops compose-applied containers", func() {
			sess := newGitSession(gitTestClient())
			applied := gitAssociatedContainer("applied", "/applied", "myapp:latest", nil)
			kept := gitAssociatedContainer("kept", "/kept", "other:latest", nil)
			sess.applied[applied.ID()] = struct{}{}

			got := sess.excludeApplied([]types.Container{applied, kept})
			gomega.Expect(got).To(gomega.HaveLen(1))
			gomega.Expect(got[0].ID()).To(gomega.Equal(kept.ID()))
		})
	})

	ginkgo.Describe("compose apply", func() {
		ginkgo.It("checkouts once and applies stale services that share a project dir", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())

			var got compose.Request

			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				Run(func(ctx context.Context, req compose.Request) {
					deadline, ok := ctx.Deadline()
					gomega.Expect(ok).To(gomega.BeTrue())
					gomega.Expect(time.Until(deadline)).To(gomega.BeNumerically("<=", time.Second))

					got = req
				}).
				Return([]compose.Container{
					{Service: "api", ID: "new-api", ImageID: "sha256:api"},
					{Service: "worker", ID: "new-worker", ImageID: "sha256:worker"},
				}, nil).
				Once()

			sess := newGitSession(gitTestClient())
			sess.apply = applier

			var checkouts int

			sess.checkout = func(ctx context.Context, gotDir, commit string, opts project.CheckoutOptions) error {
				deadline, ok := ctx.Deadline()
				gomega.Expect(ok).To(gomega.BeTrue())
				gomega.Expect(time.Until(deadline)).To(gomega.BeNumerically("<=", time.Second))

				checkouts++

				gomega.Expect(gotDir).To(gomega.Equal(dir))
				gomega.Expect(commit).To(gomega.Equal("0123456789abcdef0123456789abcdef01234567"))
				gomega.Expect(opts.Stash).To(gomega.BeTrue())
				gomega.Expect(opts.AuthFor).NotTo(gomega.BeNil())

				return nil
			}

			api := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeProjectLabel: "webstack",
				compose.ComposeServiceLabel: "api",
			})
			worker := gitAssociatedContainer("worker", "/worker", "webstack-worker:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeProjectLabel: "webstack",
				compose.ComposeServiceLabel: "worker",
			})

			api.SetStale(true)
			worker.SetStale(true)

			result := git.CheckResult{Stale: true, Commit: "0123456789abcdef0123456789abcdef01234567", Tag: "v1.2.3"}
			sess.store(api.ID(), result)
			sess.store(worker.ID(), result)

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(
				gitTestLogger(),
				ginkgo.GinkgoT().Context(),
				docker,
				[]types.Container{api, worker},
				types.UpdateParams{EnableGitMonitoring: true, GitComposeStash: true},
				nil,
				failed,
			)

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(checkouts).To(gomega.Equal(1))
			gomega.Expect(got.Ref.Dir).To(gomega.Equal(dir))
			gomega.Expect(got.Services).To(gomega.ConsistOf("api", "worker"))
			gomega.Expect(got.BuildOnly).To(gomega.BeFalse())
			gomega.Expect(got.Labels["api"][gitPkg.LastCommitLabel]).To(gomega.Equal(result.Commit))
			gomega.Expect(sess.applied).To(gomega.HaveKey(api.ID()))
			gomega.Expect(sess.applied).To(gomega.HaveKey(worker.ID()))
			gomega.Expect(sess.skipRecreate(api, types.UpdateParams{})).To(gomega.BeTrue())
		})

		ginkgo.It("separates projects that share a directory and config identity", func() {
			dir := writeComposeProjectDir()
			override := filepath.Join(dir, "override.yml")
			gomega.Expect(os.WriteFile(override, []byte("services: {}\n"), 0o600)).To(gomega.Succeed())

			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())

			var requests []compose.Request

			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				RunAndReturn(func(_ context.Context, req compose.Request) ([]compose.Container, error) {
					requests = append(requests, req)

					return []compose.Container{{
						Service: req.Services[0],
						Name:    "/" + req.Ref.Name + "-api",
						ID:      types.ContainerID("new-" + req.Ref.Name),
						ImageID: types.ImageID("sha256:" + req.Ref.Name),
					}}, nil
				}).
				Twice()

			sess := newGitSession(gitTestClient())
			sess.apply = applier

			var checkouts int

			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				checkouts++

				return nil
			}

			alpha := gitAssociatedContainer("alpha", "alpha-api", "alpha:latest", map[string]string{
				gitPkg.ComposeDirLabel:          dir,
				compose.ComposeProjectLabel:     "alpha",
				compose.ComposeServiceLabel:     "api",
				compose.ComposeConfigFilesLabel: "compose.yaml",
			})
			beta := gitAssociatedContainer("beta", "beta-api", "beta:latest", map[string]string{
				gitPkg.ComposeDirLabel:          dir,
				compose.ComposeProjectLabel:     "beta",
				compose.ComposeServiceLabel:     "api",
				compose.ComposeConfigFilesLabel: "override.yml",
			})

			alpha.SetStale(true)
			beta.SetStale(true)

			result := git.CheckResult{Stale: true, Commit: "abc"}
			sess.store(alpha.ID(), result)
			sess.store(beta.ID(), result)

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{alpha, beta}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(checkouts).To(gomega.Equal(2))
			gomega.Expect(requests).To(gomega.HaveLen(2))
			gomega.Expect(requests[0].Ref.Name).NotTo(gomega.Equal(requests[1].Ref.Name))
			gomega.Expect(requests[0].Ref.ConfigFiles).NotTo(gomega.Equal(requests[1].Ref.ConfigFiles))
			gomega.Expect(sess.applied).To(gomega.HaveKey(alpha.ID()))
			gomega.Expect(sess.applied).To(gomega.HaveKey(beta.ID()))
		})

		ginkgo.It("applies dependent Compose projects after their dependencies", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())

			var order []string

			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				RunAndReturn(func(_ context.Context, req compose.Request) ([]compose.Container, error) {
					order = append(order, req.Ref.Name)

					return []compose.Container{{
						Service: req.Services[0],
						Name:    "/" + req.Ref.Name + "-api",
						ID:      types.ContainerID("new-" + req.Ref.Name),
						ImageID: types.ImageID("sha256:" + req.Ref.Name),
					}}, nil
				}).
				Twice()

			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			provider := gitAssociatedContainer("provider", "alpha-api", "alpha:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeProjectLabel: "alpha",
				compose.ComposeServiceLabel: "api",
			})
			dependent := gitAssociatedContainer("dependent", "beta-api", "beta:latest", map[string]string{
				gitPkg.ComposeDirLabel:                      dir,
				compose.ComposeProjectLabel:                 "beta",
				compose.ComposeServiceLabel:                 "api",
				"com.centurylinklabs.watchtower.depends-on": "alpha-api",
			})

			provider.SetStale(true)
			dependent.SetStale(true)

			result := git.CheckResult{Stale: true, Commit: "abc"}
			sess.store(provider.ID(), result)
			sess.store(dependent.ID(), result)

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{dependent, provider}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(order).To(gomega.Equal([]string{"alpha", "beta"}))
		})

		ginkgo.It("applies a dependency project before a project whose first service is independent", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())

			var order []string

			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				RunAndReturn(func(_ context.Context, req compose.Request) ([]compose.Container, error) {
					order = append(order, req.Ref.Name)

					items := make([]compose.Container, 0, len(req.Services))
					for _, service := range req.Services {
						items = append(items, compose.Container{
							Service: service,
							Name:    "/" + req.Ref.Name + "-" + service,
							ID:      types.ContainerID("new-" + req.Ref.Name + "-" + service),
							ImageID: types.ImageID("sha256:" + req.Ref.Name),
						})
					}

					return items, nil
				}).
				Twice()

			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			metrics := gitAssociatedContainer("metrics", "beta-metrics", "beta:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeProjectLabel: "beta",
				compose.ComposeServiceLabel: "metrics",
			})
			dependent := gitAssociatedContainer("dependent", "beta-api", "beta:latest", map[string]string{
				gitPkg.ComposeDirLabel:                      dir,
				compose.ComposeProjectLabel:                 "beta",
				compose.ComposeServiceLabel:                 "api",
				"com.centurylinklabs.watchtower.depends-on": "alpha-api",
			})
			provider := gitAssociatedContainer("provider", "alpha-api", "alpha:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeProjectLabel: "alpha",
				compose.ComposeServiceLabel: "api",
			})

			metrics.SetStale(true)
			dependent.SetStale(true)
			provider.SetStale(true)

			result := git.CheckResult{Stale: true, Commit: "abc"}
			sess.store(metrics.ID(), result)
			sess.store(dependent.ID(), result)
			sess.store(provider.ID(), result)

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(
				gitTestLogger(),
				ginkgo.GinkgoT().Context(),
				docker,
				[]types.Container{metrics, dependent, provider},
				types.UpdateParams{EnableGitMonitoring: true},
				nil,
				failed,
			)

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(order).To(gomega.Equal([]string{"alpha", "beta"}))
		})

		ginkgo.It("keeps a Compose apply that returns success after its timeout elapses", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())
			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				RunAndReturn(func(context.Context, compose.Request) ([]compose.Container, error) {
					time.Sleep(30 * time.Millisecond)

					return []compose.Container{{
						Service: "api",
						Name:    "/web-api",
						ID:      "new-api",
						ImageID: "sha256:api",
					}}, nil
				}).
				Once()

			sess := newGitSession(git.New(gitTestLogger(), git.Options{Timeout: 5 * time.Millisecond}))
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			c := gitAssociatedContainer("api", "web-api", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeProjectLabel: "webstack",
				compose.ComposeServiceLabel: "api",
			})
			c.SetStale(true)
			sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(
				gitTestLogger(),
				ginkgo.GinkgoT().Context(),
				docker,
				[]types.Container{c},
				types.UpdateParams{EnableGitMonitoring: true},
				nil,
				failed,
			)

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(sess.applied).To(gomega.HaveKey(c.ID()))
		})

		ginkgo.It("does not apply after the parent context is canceled during checkout", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())
			parent, cancel := context.WithCancel(ginkgo.GinkgoT().Context())

			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(_ context.Context, _, _ string, opts project.CheckoutOptions) error {
				if opts.Prior != nil {
					*opts.Prior = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				}

				cancel()

				return nil
			}

			c := gitAssociatedContainer("api", "web-api", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeProjectLabel: "webstack",
				compose.ComposeServiceLabel: "api",
			})
			c.SetStale(true)
			sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(
				gitTestLogger(),
				parent,
				docker,
				[]types.Container{c},
				types.UpdateParams{EnableGitMonitoring: true},
				nil,
				failed,
			)

			gomega.Expect(failed[c.ID()]).To(gomega.MatchError(context.Canceled))
			gomega.Expect(sess.applied).To(gomega.BeEmpty())
		})

		ginkgo.It("reports a normal Compose apply as updated", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())
			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				Return([]compose.Container{{Service: "api", ID: "new-api", ImageID: "sha256:api"}}, nil).
				Once()

			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			c := gitAssociatedContainer("api", "web-1", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeProjectLabel: "webstack",
				compose.ComposeServiceLabel: "api",
			})
			c.SetStale(true)

			result := git.CheckResult{Stale: true, Commit: "0123456789abcdef0123456789abcdef01234567", Tag: "v1.2.3"}
			sess.store(c.ID(), result)

			params := types.UpdateParams{EnableGitMonitoring: true}
			progress := &session.Progress{}
			progress.AddScanned(gitTestLogger(), c, types.ImageID("git:"+result.Commit), params)

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{c}, params, progress, failed)

			report := progress.Report(gitTestLogger())

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
			gomega.Expect(report.Updated()[0].NewContainerID()).To(gomega.Equal(types.ContainerID("new-api")))
			gomega.Expect(report.Stale()).To(gomega.BeEmpty())
		})

		ginkgo.It("uses a remote Git build when compose-dir is unset", func() {
			sess := newGitSession(gitTestClient())
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())
			sess.apply = applier
			c := gitAssociatedContainer("ok", "/ok", "myapp:latest", nil)
			c.SetStale(true)
			sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "0123456789abcdef0123456789abcdef01234567"})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{c}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(sess.built).To(gomega.HaveKey(c.ID()))
			gomega.Expect(sess.applied).To(gomega.BeEmpty())
		})

		ginkgo.It("fails when compose-dir is set but the path is not a compose project", func() {
			missing := filepath.Join(ginkgo.GinkgoT().TempDir(), "missing")
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())
			sess := newGitSession(gitTestClient())
			sess.apply = applier

			c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      missing,
				compose.ComposeServiceLabel: "api",
			})
			c.SetStale(true)
			sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{c}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

			gomega.Expect(failed[c.ID()]).To(gomega.MatchError(compose.ErrProjectDir))
			gomega.Expect(c.IsStale()).To(gomega.BeFalse())
			gomega.Expect(sess.built).To(gomega.BeEmpty())
		})

		ginkgo.It("fails a compose container that has no service label", func() {
			dir := writeComposeProjectDir()
			sess := newGitSession(gitTestClient())
			sess.apply = mockCompose.NewMockApplier(ginkgo.GinkgoT())
			c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel: dir,
			})
			c.SetStale(true)
			sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{c}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

			gomega.Expect(failed[c.ID()]).To(gomega.MatchError(errGitComposeService))
			gomega.Expect(c.IsStale()).To(gomega.BeFalse())
		})

		ginkgo.It("aborts the project when checkout fails", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())
			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return project.ErrDirty
			}

			c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})
			c.SetStale(true)
			sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{c}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

			gomega.Expect(failed[c.ID()]).To(gomega.MatchError(project.ErrDirty))
			gomega.Expect(c.IsStale()).To(gomega.BeFalse())
		})

		ginkgo.It("applies after checkout returns nil when only its timeout elapsed", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())
			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				Return([]compose.Container{{Service: "api", Name: "/api", ID: "new-api", ImageID: "sha256:api"}}, nil).
				Once()

			sess := newGitSession(git.New(gitTestLogger(), git.Options{Timeout: 10 * time.Millisecond}))
			sess.apply = applier
			sess.checkout = func(ctx context.Context, _, _ string, _ project.CheckoutOptions) error {
				<-ctx.Done()

				return nil
			}

			c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})
			c.SetStale(true)
			sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{c}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(sess.applied).To(gomega.HaveKey(c.ID()))
		})

		ginkgo.It("unmarks stale containers when compose apply fails", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())
			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				Return([]compose.Container{
					{Service: "api", Name: "/api", ID: "new-api", ImageID: "sha256:api"},
					{Service: "api", Name: "/fresh", ID: "new-fresh", ImageID: "sha256:api"},
				}, context.Canceled).
				Once()

			var logs bytes.Buffer

			log := zerolog.New(&logs)
			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})
			fresh := gitAssociatedContainer("fresh", "/fresh", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})

			c.SetStale(true)
			sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			params := types.UpdateParams{EnableGitMonitoring: true}
			progress := &session.Progress{}
			progress.AddScanned(&log, c, types.ImageID("git:new"), params)
			progress.AddScanned(&log, fresh, fresh.ImageID(), params)
			sess.prepareRebuilds(&log, ginkgo.GinkgoT().Context(), docker, []types.Container{c}, params, progress, failed)
			progress.UpdateFailed(&log, failed)

			report := progress.Report(&log)

			gomega.Expect(failed[c.ID()]).To(gomega.MatchError(context.Canceled))
			gomega.Expect(failed[c.ID()].Error()).To(gomega.ContainSubstring("partially recreated services"))
			gomega.Expect(failed[fresh.ID()]).To(gomega.MatchError(context.Canceled))
			gomega.Expect(c.IsStale()).To(gomega.BeFalse())
			gomega.Expect(sess.applied).To(gomega.BeEmpty())
			gomega.Expect(report.Updated()).To(gomega.BeEmpty())
			gomega.Expect(report.Failed()).To(gomega.HaveLen(2))
			gomega.Expect(logs.String()).To(gomega.ContainSubstring("may have partially recreated services"))
			gomega.Expect(logs.String()).NotTo(gomega.ContainSubstring("Leaving running container untouched"))
		})

		ginkgo.It("fails a service omitted from the compose apply result", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())
			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				Return([]compose.Container{{Service: "api", ID: "new-api", ImageID: "sha256:api"}}, nil).
				Once()

			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			api := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})
			worker := gitAssociatedContainer("worker", "/worker", "webstack-worker:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "worker",
			})

			api.SetStale(true)
			worker.SetStale(true)

			result := git.CheckResult{Stale: true, Commit: "abc"}
			sess.store(api.ID(), result)
			sess.store(worker.ID(), result)

			var logs bytes.Buffer

			log := zerolog.New(&logs)
			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			params := types.UpdateParams{EnableGitMonitoring: true}
			progress := &session.Progress{}
			progress.AddScanned(&log, api, types.ImageID("git:abc"), params)
			progress.AddScanned(&log, worker, types.ImageID("git:abc"), params)
			sess.prepareRebuilds(&log, ginkgo.GinkgoT().Context(), docker, []types.Container{api, worker}, params, progress, failed)
			progress.UpdateFailed(&log, failed)

			report := progress.Report(&log)

			gomega.Expect(failed).NotTo(gomega.HaveKey(api.ID()))
			gomega.Expect(failed[worker.ID()]).To(gomega.MatchError(errGitComposeInstance))
			gomega.Expect(worker.IsStale()).To(gomega.BeFalse())
			gomega.Expect(sess.applied).To(gomega.HaveKey(api.ID()))
			gomega.Expect(sess.applied).NotTo(gomega.HaveKey(worker.ID()))
			gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
			gomega.Expect(report.Failed()).To(gomega.HaveLen(1))
			gomega.Expect(logs.String()).To(gomega.ContainSubstring("Runtime state is unknown"))
			gomega.Expect(logs.String()).NotTo(gomega.ContainSubstring("Leaving running container untouched"))
		})

		ginkgo.It("builds only when no-restart is set", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())

			var got compose.Request

			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				Run(func(_ context.Context, req compose.Request) {
					got = req
				}).
				Return([]compose.Container{{Service: "api", ImageID: "sha256:built"}}, nil).
				Once()

			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})
			c.SetStale(true)
			sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			params := types.UpdateParams{EnableGitMonitoring: true, NoRestart: true}
			progress := &session.Progress{}
			progress.AddScanned(gitTestLogger(), c, types.ImageID("git:abc"), params)
			sess.prepareRebuilds(
				gitTestLogger(),
				ginkgo.GinkgoT().Context(),
				docker,
				[]types.Container{c},
				params,
				progress,
				failed,
			)

			report := progress.Report(gitTestLogger())

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(got.BuildOnly).To(gomega.BeTrue())
			gomega.Expect(sess.applied).To(gomega.BeEmpty())
			gomega.Expect(sess.built[c.ID()]).To(gomega.Equal(types.ImageID("sha256:built")))
			gomega.Expect(sess.skipRecreate(c, params)).To(gomega.BeTrue())
			gomega.Expect(report.Updated()).To(gomega.BeEmpty())
			gomega.Expect(report.Stale()).To(gomega.HaveLen(1))
		})

		ginkgo.It("reports Compose build failures without partial runtime claims", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())
			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				Return(nil, context.Canceled).
				Once()

			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})
			c.SetStale(true)
			sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

			var logs bytes.Buffer

			log := zerolog.New(&logs)
			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			params := types.UpdateParams{EnableGitMonitoring: true, NoRestart: true}
			sess.prepareRebuilds(&log, ginkgo.GinkgoT().Context(), docker, []types.Container{c}, params, nil, failed)

			gomega.Expect(failed[c.ID()]).To(gomega.MatchError(context.Canceled))
			gomega.Expect(failed[c.ID()].Error()).To(gomega.ContainSubstring("compose build failed"))
			gomega.Expect(c.IsStale()).To(gomega.BeFalse())
			gomega.Expect(sess.built).To(gomega.BeEmpty())
			gomega.Expect(sess.applied).To(gomega.BeEmpty())
			gomega.Expect(logs.String()).NotTo(gomega.ContainSubstring("partially recreated services"))
		})

		ginkgo.It("fails the project when services resolve different commits", func() {
			dir := writeComposeProjectDir()
			checkedOut := false
			sess := newGitSession(gitTestClient())
			sess.apply = mockCompose.NewMockApplier(ginkgo.GinkgoT())
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				checkedOut = true

				return nil
			}

			api := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})
			worker := gitAssociatedContainer("worker", "/worker", "webstack-worker:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "worker",
			})

			api.SetStale(true)
			worker.SetStale(true)
			sess.store(api.ID(), git.CheckResult{Stale: true, Commit: "aaa"})
			sess.store(worker.ID(), git.CheckResult{Stale: true, Commit: "bbb"})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{api, worker}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

			gomega.Expect(checkedOut).To(gomega.BeFalse())
			gomega.Expect(failed[api.ID()]).To(gomega.MatchError(errGitComposeCommit))
			gomega.Expect(failed[worker.ID()]).To(gomega.MatchError(errGitComposeCommit))
			gomega.Expect(api.IsStale()).To(gomega.BeFalse())
			gomega.Expect(worker.IsStale()).To(gomega.BeFalse())
		})

		ginkgo.It("stamps each service from its own tag", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())

			var got compose.Request

			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				Run(func(_ context.Context, req compose.Request) {
					got = req
				}).
				Return([]compose.Container{
					{Service: "api", ID: "new-api", ImageID: "sha256:api"},
					{Service: "worker", ID: "new-worker", ImageID: "sha256:worker"},
				}, nil).
				Once()

			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			api := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})
			worker := gitAssociatedContainer("worker", "/worker", "webstack-worker:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "worker",
			})

			api.SetStale(true)
			worker.SetStale(true)
			sess.store(api.ID(), git.CheckResult{Stale: true, Commit: "abc", Tag: "v1.2.3"})
			sess.store(worker.ID(), git.CheckResult{Stale: true, Commit: "abc", Tag: "v1.2.4"})

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{api, worker}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(got.Labels["api"][gitPkg.LastTagLabel]).To(gomega.Equal("v1.2.3"))
			gomega.Expect(got.Labels["worker"][gitPkg.LastTagLabel]).To(gomega.Equal("v1.2.4"))
			gomega.Expect(sess.applied).To(gomega.HaveKey(api.ID()))
			gomega.Expect(sess.applied).To(gomega.HaveKey(worker.ID()))
		})

		ginkgo.It("marks every replica of a service applied", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())

			var got compose.Request

			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				Run(func(_ context.Context, req compose.Request) {
					got = req
				}).
				Return([]compose.Container{
					{Service: "api", Name: "/web-1", ID: "new-1", ImageID: "sha256:api"},
					{Service: "api", Name: "/web-2", ID: "new-2", ImageID: "sha256:api"},
				}, nil).
				Once()

			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			one := gitAssociatedContainer("api-1", "web-1", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})
			two := gitAssociatedContainer("api-2", "web-2", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})

			one.SetStale(true)
			two.SetStale(true)

			result := git.CheckResult{Stale: true, Commit: "abc"}
			sess.store(one.ID(), result)
			sess.store(two.ID(), result)

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{one, two}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(got.Services).To(gomega.Equal([]string{"api"}))
			gomega.Expect(sess.applied).To(gomega.HaveKey(one.ID()))
			gomega.Expect(sess.applied).To(gomega.HaveKey(two.ID()))
		})

		ginkgo.It("tracks every recreated replica in a mixed stale and fresh service", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())
			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				Return([]compose.Container{
					{Service: "api", Name: "/web-1", ID: "new-1", ImageID: "sha256:api"},
					{Service: "api", Name: "/web-2", ID: "new-2", ImageID: "sha256:api"},
				}, nil).
				Once()

			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			stale := gitAssociatedContainer("api-1", "web-1", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})
			fresh := gitAssociatedContainer("api-2", "web-2", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})

			stale.SetStale(true)

			result := git.CheckResult{Stale: true, Commit: "abc", Tag: "v1.2.3"}
			sess.store(stale.ID(), result)

			params := types.UpdateParams{EnableGitMonitoring: true}
			progress := &session.Progress{}
			progress.AddScanned(gitTestLogger(), stale, types.ImageID("git:"+result.Commit), params)
			progress.AddScanned(gitTestLogger(), fresh, fresh.ImageID(), params)

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{stale}, params, progress, failed)

			report := progress.Report(gitTestLogger())

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(sess.applied).To(gomega.HaveKey(stale.ID()))
			gomega.Expect(sess.applied).To(gomega.HaveKey(fresh.ID()))
			gomega.Expect(report.Updated()).To(gomega.HaveLen(2))
			gomega.Expect(report.Stale()).To(gomega.BeEmpty())
			gomega.Expect(report.Updated()[0].NewContainerID()).NotTo(gomega.BeEmpty())
			gomega.Expect(report.Updated()[1].NewContainerID()).NotTo(gomega.BeEmpty())
			gomega.Expect(sess.excludeApplied([]types.Container{stale, fresh})).To(gomega.BeEmpty())
		})

		ginkgo.It("accepts additional Compose replicas absent from filtered progress", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())
			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				Return([]compose.Container{
					{Service: "api", Name: "/web-1", ID: "new-1", ImageID: "sha256:api"},
					{Service: "api", Name: "/web-2", ID: "new-2", ImageID: "sha256:api"},
				}, nil).
				Once()

			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			stale := gitAssociatedContainer("api-1", "web-1", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})
			fresh := gitAssociatedContainer("api-2", "web-2", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})

			stale.SetStale(true)

			result := git.CheckResult{Stale: true, Commit: "abc", Tag: "v1.2.3"}
			sess.store(stale.ID(), result)

			params := types.UpdateParams{EnableGitMonitoring: true}
			progress := &session.Progress{}
			progress.AddScanned(gitTestLogger(), stale, types.ImageID("git:"+result.Commit), params)

			var logs bytes.Buffer

			log := zerolog.New(&logs)
			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(&log, ginkgo.GinkgoT().Context(), docker, []types.Container{stale}, params, progress, failed)

			report := progress.Report(&log)

			gomega.Expect(failed).To(gomega.BeEmpty())
			gomega.Expect(sess.applied).To(gomega.HaveKey(stale.ID()))
			gomega.Expect(sess.applied).NotTo(gomega.HaveKey(fresh.ID()))
			gomega.Expect(report.Updated()).To(gomega.HaveLen(1))
			gomega.Expect(logs.String()).To(gomega.ContainSubstring("replica absent from filtered progress"))
		})

		ginkgo.It("does not apply a replica compose did not identify", func() {
			dir := writeComposeProjectDir()
			applier := mockCompose.NewMockApplier(ginkgo.GinkgoT())
			applier.EXPECT().
				Apply(mock.Anything, mock.Anything).
				Return([]compose.Container{
					{Service: "api", Name: "web-1", ID: "new-1", ImageID: "sha256:api"},
				}, nil).
				Once()

			sess := newGitSession(gitTestClient())
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			one := gitAssociatedContainer("api-1", "web-1", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})
			two := gitAssociatedContainer("api-2", "web-2", "webstack-api:latest", map[string]string{
				gitPkg.ComposeDirLabel:      dir,
				compose.ComposeServiceLabel: "api",
			})

			one.SetStale(true)
			two.SetStale(true)

			result := git.CheckResult{Stale: true, Commit: "abc"}
			sess.store(one.ID(), result)
			sess.store(two.ID(), result)

			failed := map[types.ContainerID]error{}
			docker := mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
			sess.prepareRebuilds(gitTestLogger(), ginkgo.GinkgoT().Context(), docker, []types.Container{one, two}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

			gomega.Expect(failed).NotTo(gomega.HaveKey(one.ID()))
			gomega.Expect(failed[two.ID()]).To(gomega.MatchError(errGitComposeInstance))
			gomega.Expect(two.IsStale()).To(gomega.BeFalse())
			gomega.Expect(sess.applied).To(gomega.HaveKey(one.ID()))
			gomega.Expect(sess.applied).NotTo(gomega.HaveKey(two.ID()))
		})
	})

	ginkgo.Describe("composeServiceLabels", func() {
		ginkgo.It("omits git-watch when only process-wide enable is set", func() {
			c := gitAssociatedContainer("api", "/api", "webstack-api:latest", nil)
			got := composeServiceLabels(c, types.UpdateParams{EnableGitMonitoring: true}, git.CheckResult{Commit: "abc"})
			gomega.Expect(got).NotTo(gomega.HaveKey(gitPkg.WatchLabel))
			gomega.Expect(got[gitPkg.RepoLabel]).To(gomega.Equal("https://github.com/org/app.git"))
		})

		ginkgo.It("redacts credentials from the Compose repository label", func() {
			c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.RepoLabel: "https://user:secret@example.com/org/app.git?token=secret",
			})
			got := composeServiceLabels(c, types.UpdateParams{EnableGitMonitoring: true}, git.CheckResult{Commit: "abc"})
			gomega.Expect(got[gitPkg.RepoLabel]).To(gomega.Equal("https://example.com/org/app.git"))
			gomega.Expect(got[gitPkg.RepoLabel]).NotTo(gomega.ContainSubstring("secret"))
		})

		ginkgo.It("redacts API origin credentials while preserving path and port", func() {
			c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.HostLabel: "https://user:secret@git.example.com:8443/git/api?token=secret",
			})
			got := composeServiceLabels(c, types.UpdateParams{EnableGitMonitoring: true}, git.CheckResult{Commit: "abc"})
			gomega.Expect(got[gitPkg.HostLabel]).To(gomega.Equal("https://git.example.com:8443/git/api"))
			gomega.Expect(got[gitPkg.HostLabel]).NotTo(gomega.ContainSubstring("secret"))
		})

		ginkgo.It("omits malformed API origins", func() {
			c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.HostLabel: "https://",
			})
			got := composeServiceLabels(c, types.UpdateParams{EnableGitMonitoring: true}, git.CheckResult{Commit: "abc"})
			gomega.Expect(got).NotTo(gomega.HaveKey(gitPkg.HostLabel))
		})

		ginkgo.It("writes git-watch when the source label enabled it", func() {
			c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				gitPkg.WatchLabel: "true",
			})
			got := composeServiceLabels(c, types.UpdateParams{}, git.CheckResult{Commit: "abc"})
			gomega.Expect(got[gitPkg.WatchLabel]).To(gomega.Equal("true"))
		})
	})

	ginkgo.Describe("reconcileImplicitRestarts", func() {
		ginkgo.It("drops dependents when the anchor apply failed", func() {
			log := gitTestLogger()
			app := mockActions.CreateMockContainerWithLinks(
				"app",
				"/app",
				"app:latest",
				time.Now(),
				nil,
				nil,
			)
			db := mockActions.CreateMockContainerWithLinks(
				"db",
				"/db",
				"db:latest",
				time.Now(),
				[]string{"/app:app"},
				nil,
			)

			app.SetStale(true)
			containers := []types.Container{app, db}
			UpdateImplicitRestart(log, containers, containers, false)
			gomega.Expect(db.IsLinkedToRestarting()).To(gomega.BeTrue())

			app.SetStale(false)

			got := reconcileImplicitRestarts(log, containers, containers, types.UpdateParams{})

			gomega.Expect(db.IsLinkedToRestarting()).To(gomega.BeFalse())
			gomega.Expect(got).To(gomega.BeEmpty())
		})

		ginkgo.It("keeps dependents when the anchor is still stale", func() {
			log := gitTestLogger()
			app := mockActions.CreateMockContainerWithLinks(
				"app",
				"/app",
				"app:latest",
				time.Now(),
				nil,
				nil,
			)
			db := mockActions.CreateMockContainerWithLinks(
				"db",
				"/db",
				"db:latest",
				time.Now(),
				[]string{"/app:app"},
				nil,
			)

			app.SetStale(true)
			containers := []types.Container{app, db}
			got := reconcileImplicitRestarts(log, containers, containers, types.UpdateParams{})

			gomega.Expect(db.IsLinkedToRestarting()).To(gomega.BeTrue())
			gomega.Expect(got).To(gomega.ConsistOf(app, db))
		})

		ginkgo.It("does not propagate restarts from a no-restart build anchor", func() {
			log := gitTestLogger()
			app := mockActions.CreateMockContainerWithLinks(
				"app",
				"/app",
				"app:latest",
				time.Now(),
				nil,
				nil,
			)
			dependent := mockActions.CreateMockContainerWithLinks(
				"dependent",
				"/dependent",
				"dependent:latest",
				time.Now(),
				[]string{"/app:app"},
				nil,
			)

			app.SetStale(true)
			containers := []types.Container{app, dependent}
			sess := newGitSession(gitTestClient())
			sess.built[app.ID()] = "sha256:git"

			got := reconcileImplicitRestartsExcluding(
				log,
				containers,
				containers,
				types.UpdateParams{NoRestart: true},
				sess.noRestartBuiltIDs(types.UpdateParams{NoRestart: true}),
			)

			gomega.Expect(dependent.IsLinkedToRestarting()).To(gomega.BeFalse())
			gomega.Expect(got).To(gomega.BeEmpty())
		})
		ginkgo.It("does not fall through from an excluded exact link to a service-name match", func() {
			log := gitTestLogger()
			excluded := mockActions.CreateMockContainerWithLinks(
				"app",
				"/app",
				"app:latest",
				time.Now(),
				nil,
				nil,
			)
			serviceMatch := mockActions.CreateMockContainerWithLinks(
				"app-1",
				"/app-1",
				"app:latest",
				time.Now(),
				nil,
				nil,
			)
			dependent := mockActions.CreateMockContainerWithLinks(
				"dependent",
				"/dependent",
				"dependent:latest",
				time.Now(),
				[]string{"/app:app"},
				nil,
			)

			excluded.SetStale(true)
			serviceMatch.SetStale(true)
			containers := []types.Container{excluded, serviceMatch, dependent}

			got := reconcileImplicitRestartsExcluding(
				log,
				containers,
				containers,
				types.UpdateParams{},
				map[types.ContainerID]struct{}{excluded.ID(): {}},
			)

			gomega.Expect(dependent.IsLinkedToRestarting()).To(gomega.BeFalse())
			gomega.Expect(got).To(gomega.ConsistOf(serviceMatch))
		})
	})

	ginkgo.Describe("gitImageTag", func() {
		ginkgo.DescribeTable("formats name:git-<shortsha>",
			func(image, commit, want string) {
				gomega.Expect(gitImageTag(image, commit)).To(gomega.Equal(want))
			},
			ginkgo.Entry("docker official image", "nginx:latest", "0123456789abcdef0123456789abcdef01234567", "docker.io/library/nginx:git-0123456789ab"),
			ginkgo.Entry("already normalized name", "docker.io/library/myapp:latest", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "docker.io/library/myapp:git-aaaaaaaaaaaa"),
			ginkgo.Entry("short commit is kept", "myapp:latest", "abc", "docker.io/library/myapp:git-abc"),
			ginkgo.Entry("empty commit", "myapp:latest", "", "docker.io/library/myapp:git-"),
			ginkgo.Entry("unparseable name strips a tag suffix", "not a ref:dev", "0123456789abcdef", "not a ref:git-0123456789ab"),
			ginkgo.Entry("registry with port is not stripped at the port colon", "localhost:5000/app", "0123456789abcdef0123", "localhost:5000/app:git-0123456789ab"),
		)

		ginkgo.DescribeTable("validDockerTag",
			func(image string, want bool) {
				gomega.Expect(validDockerTag(image)).To(gomega.Equal(want))
			},
			ginkgo.Entry("tagged image", "myapp:latest", true),
			ginkgo.Entry("bare image", "myapp", true),
			ginkgo.Entry("digest image", "myapp@sha256:"+strings.Repeat("a", 64), false),
			ginkgo.Entry("tagged digest image", "myapp:latest@sha256:"+strings.Repeat("a", 64), false),
			ginkgo.Entry("invalid image", "not a ref:dev", false),
		)
	})
})

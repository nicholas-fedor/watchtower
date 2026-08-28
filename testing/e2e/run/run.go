package run

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/testing/e2e/assert"
	"github.com/nicholas-fedor/watchtower/testing/e2e/docker"
	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
	"github.com/nicholas-fedor/watchtower/testing/e2e/registry"
	"github.com/nicholas-fedor/watchtower/testing/e2e/report"
	"github.com/nicholas-fedor/watchtower/testing/e2e/watchtower"
)

const (
	// decoyName is the unenlisted container suffix when Topology.Decoy is set.
	decoyName = "e2e-decoy"
	// subjectRepo is the local image name before it is pushed to the inner registry.
	subjectRepo = "e2e/app"
	// subjectTag is the dummy subject tag Watchtower pulls.
	subjectTag = "latest"
	// imageGeneration1 is TAG/REV baked into the first published image.
	imageGeneration1 = "r1"
	// imageGeneration2 is TAG/REV baked into the replacement image.
	imageGeneration2 = "r2"
	// permFile is the mode for inspect and porcelain artifacts.
	permFile = 0o600
	// prefixIDLen is how many case-id characters go into the Docker name prefix.
	prefixIDLen = 12
	// apiReadyWait is how long HTTP-API cases wait before probing /v1/containers/details.
	apiReadyWait = 2 * time.Second
	// containerStopSeconds is the Docker stop timeout after an HTTP-API probe.
	containerStopSeconds = 5
)

// Options controls one sitting's execution environment.
type Options struct {
	// Daemon is the acquired DinD worker.
	Daemon *docker.Daemon
	// Artifacts holds the built Watchtower, subject, and persona binaries.
	Artifacts watchtower.Artifacts
	// RunDir is artifacts/<run-id>.
	RunDir string
	// Keep retains passing case directories.
	Keep bool
}

// Execute runs one case on the acquired worker.
//
// Parameters:
//   - ctx: Cancellation.
//   - opts: Sitting resources.
//   - item: Case vector.
//
// Returns:
//   - engine.Result: Pass/fail/skip.
func Execute(ctx context.Context, opts Options, item engine.Case) engine.Result {
	started := time.Now()
	caseID := item.ID()
	result := engine.Result{CaseID: caseID, Status: "fail"}

	if engine.Unrealizable(item) {
		result.Skipped = true
		result.Status = "skip"
		result.Err = "unrealizable topology"

		return result
	}

	caseDir, dirErr := report.WriteCaseDir(opts.RunDir, report.Meta{ID: caseID, Factors: item.Factors, Expect: item.Expect})
	if dirErr != nil {
		result.Err = dirErr.Error()

		return result
	}

	prefix := "e2e-" + caseID[:min(prefixIDLen, len(caseID))]

	resetErr := opts.Daemon.Reset(ctx, prefix)
	if resetErr != nil {
		result.Err = resetErr.Error()

		return result
	}

	kind := item.Topology.SubjectKind
	if kind == "" {
		kind = "echo"
	}

	regID, regErr := docker.StartInnerRegistry(ctx, opts.Daemon)
	if regErr != nil {
		result.Err = regErr.Error()
		_ = report.WriteFailure(caseDir, result.Err)

		return result
	}

	backend, backendErr := docker.RegistryBackendURL(ctx, opts.Daemon, regID)
	if backendErr != nil {
		result.Err = backendErr.Error()
		_ = report.WriteFailure(caseDir, result.Err)

		return result
	}

	persona := registry.Persona(item.Topology.RegistryPersona)
	personaErr := docker.StartPersona(ctx, opts.Daemon, opts.Artifacts.PersonaBinary, persona, backend)
	if personaErr != nil {
		result.Err = personaErr.Error()
		_ = report.WriteFailure(caseDir, result.Err)

		return result
	}

	publishErr := publishSubject(ctx, opts, kind, imageGeneration1)
	if publishErr != nil {
		result.Err = publishErr.Error()
		_ = report.WriteFailure(caseDir, result.Err)

		return result
	}

	imageRef := docker.SubjectPullRef()

	subjects, createErr := docker.CreateSubjects(ctx, opts.Daemon.Client(), prefix, imageRef, item.Topology)
	if createErr != nil {
		result.Err = createErr.Error()
		_ = report.WriteFailure(caseDir, result.Err)

		return result
	}

	if item.Topology.Decoy {
		decoyTopo := item.Topology
		decoyTopo.EnableLabel = "false"

		_, decoyErr := docker.CreateSubject(ctx, opts.Daemon.Client(), prefix+"-"+decoyName, imageRef, decoyTopo)
		if decoyErr != nil {
			result.Err = decoyErr.Error()
			_ = report.WriteFailure(caseDir, result.Err)

			return result
		}
	}

	beforeRaw, beforeSnap, inspectErr := docker.InspectSnapshot(ctx, opts.Daemon.Client(), subjects.PrimaryID)
	if inspectErr != nil {
		result.Err = inspectErr.Error()
		_ = report.WriteFailure(caseDir, result.Err)

		return result
	}

	_ = os.WriteFile(filepath.Join(caseDir, "inspect-before.json"), beforeRaw, permFile)

	if item.Expect.Outcome == engine.OutcomeUpdated || item.Expect.Outcome == engine.OutcomeRateLimited {
		pub2Err := publishSubject(ctx, opts, kind, imageGeneration2)
		if pub2Err != nil {
			result.Err = pub2Err.Error()
			_ = report.WriteFailure(caseDir, result.Err)

			return result
		}
	}

	if item.Topology.RegistryFault != "" && item.Topology.RegistryFault != "none" {
		faultErr := docker.SetPersonaFault(ctx, opts.Daemon, registry.Fault(item.Topology.RegistryFault))
		if faultErr != nil {
			result.Err = faultErr.Error()
			_ = report.WriteFailure(caseDir, result.Err)

			return result
		}
	}

	inst, argv, env, startErr := watchtower.Start(ctx, opts.Daemon, opts.Artifacts, item, caseDir, nil)
	if startErr != nil {
		result.Err = startErr.Error()
		_ = report.WriteFailure(caseDir, result.Err)

		return result
	}
	defer inst.Close(ctx, opts.Daemon)

	_, _ = report.WriteCaseDir(opts.RunDir, report.Meta{ID: caseID, Factors: item.Factors, Expect: item.Expect, Argv: argv, Env: env})

	httpAPI := item.Watchtower.HTTPAPIUpdate != nil && *item.Watchtower.HTTPAPIUpdate &&
		(item.Watchtower.RunOnce == nil || !*item.Watchtower.RunOnce)

	var (
		exitCode    int
		waitErr     error
		detailsBody string
	)

	if httpAPI {
		select {
		case <-ctx.Done():
			waitErr = ctx.Err()
		case <-time.After(apiReadyWait):
		}

		token := ""
		if item.Watchtower.HTTPAPIToken != nil {
			token = *item.Watchtower.HTTPAPIToken
		}

		body, apiErr := docker.GetContainersDetails(ctx, opts.Daemon, inst.ID, token)
		if apiErr != nil {
			result.Err = apiErr.Error()
			_ = report.WriteFailure(caseDir, result.Err)
			result.Duration = time.Since(started).Milliseconds()

			return result
		}

		detailsBody = body
		stopTimeout := containerStopSeconds
		_, _ = opts.Daemon.Client().ContainerStop(ctx, inst.ID, client.ContainerStopOptions{Timeout: &stopTimeout})
	} else {
		exitCode, waitErr = watchtower.WaitRunOnce(ctx, opts.Daemon, inst)
	}

	logs := readLogs(caseDir)

	if item.Expect.Outcome == engine.OutcomeRejectConfig {
		if waitErr != nil || exitCode != 0 {
			result.Passed = true
			result.Status = "pass"
			result.Duration = time.Since(started).Milliseconds()

			return result
		}

		result.Err = "expected reject-config but watchtower exited 0"
		_ = report.WriteFailure(caseDir, result.Err)
		result.Duration = time.Since(started).Milliseconds()

		return result
	}

	if waitErr != nil {
		result.Err = waitErr.Error()
		_ = report.WriteFailure(caseDir, result.Err)
		result.Duration = time.Since(started).Milliseconds()

		return result
	}

	afterRaw, afterSnap, afterErr := docker.InspectSnapshot(ctx, opts.Daemon.Client(), subjects.PrimaryName)
	if afterErr != nil {
		listed, listErr := findRecreated(ctx, opts.Daemon.Client(), subjects.PrimaryName)
		if listErr != nil {
			result.Err = afterErr.Error()
			_ = report.WriteFailure(caseDir, result.Err)
			result.Duration = time.Since(started).Milliseconds()

			return result
		}

		afterRaw, afterSnap, afterErr = docker.InspectSnapshot(ctx, opts.Daemon.Client(), listed)
		if afterErr != nil {
			result.Err = afterErr.Error()
			_ = report.WriteFailure(caseDir, result.Err)
			result.Duration = time.Since(started).Milliseconds()

			return result
		}
	}

	_ = os.WriteFile(filepath.Join(caseDir, "inspect-after.json"), afterRaw, permFile)

	evalErr := evaluate(ctx, opts, item, subjects, beforeSnap, afterSnap, logs, detailsBody, caseDir)
	if evalErr != nil {
		result.Err = evalErr.Error()
		_ = report.WriteFailure(caseDir, result.Err)
		result.Duration = time.Since(started).Milliseconds()

		return result
	}

	if !opts.Keep && result.Passed {
		_ = os.RemoveAll(caseDir)
	}

	result.Passed = true
	result.Status = "pass"
	result.Duration = time.Since(started).Milliseconds()

	return result
}

// evaluate checks Watchtower behavior against Expect after a session.
//
// Parameters:
//   - item: Case that ran.
//   - subjects: Fixture containers including depends-on order.
//   - before: Inspect snapshot taken before Watchtower.
//   - after: Inspect snapshot taken after the session.
//   - logs: Demultiplexed stdout and stderr.
//   - caseDir: Artifact directory for porcelain.json.
//
// Returns:
//   - error: Assertion failure.
func evaluate(
	ctx context.Context,
	opts Options,
	item engine.Case,
	subjects docker.CreatedSubjects,
	before, after assert.InspectSnapshot,
	logs, detailsBody, caseDir string,
) error {
	secrets := item.Watchtower.SecretValues()

	secretErr := assert.ForbiddenSecrets(logs, secrets)
	if secretErr != nil {
		return fmt.Errorf("secret scan: %w", secretErr)
	}

	imageChanged := before.ImageID != "" && after.ImageID != before.ImageID

	switch item.Expect.Outcome {
	case engine.OutcomeUpdated:
		if !imageChanged {
			return ErrImageIDUnchanged
		}

		fidErr := assert.DiffFidelity(before, after)
		if fidErr != nil {
			return fmt.Errorf("fidelity: %w", fidErr)
		}

		for _, extra := range item.Topology.ExtraEnv {
			if !strings.Contains(strings.Join(after.Env, "\n"), extra) {
				return fmt.Errorf("%w: %q", errExtraEnvMissing, extra)
			}
		}

		if item.Topology.Graph == engine.GraphChain4 || item.Topology.Graph == engine.GraphComposeDepends {
			orderErr := assert.AssertDependencyOrder(assert.ParseSession(logs), subjects.DependencyOrder)
			if orderErr != nil {
				return fmt.Errorf("depends-on: %w", orderErr)
			}
		}
	case engine.OutcomeNoUpdate, engine.OutcomeRateLimited, engine.OutcomeAuthFail, engine.OutcomeBlocked:
		if imageChanged {
			return assert.ErrUnexpectedUpdate
		}

		if item.Expect.Outcome == engine.OutcomeRateLimited {
			lower := strings.ToLower(logs)
			rateHit := strings.Contains(lower, "rate limited") ||
				strings.Contains(lower, "retry-after") ||
				strings.Contains(lower, "toomanyrequests") ||
				strings.Contains(lower, "too many requests") ||
				strings.Contains(lower, "failed to pull")
			if !rateHit {
				return errNoRateLimitLog
			}
		}
	case engine.OutcomeRejectConfig, engine.OutcomeTimeout, engine.OutcomeOOM, engine.OutcomeKilled, engine.OutcomeCrash, engine.OutcomeLeftover:
	}

	if item.Watchtower.Cleanup != nil && *item.Watchtower.Cleanup && item.Expect.Outcome == engine.OutcomeUpdated {
		goneErr := assertImageGone(ctx, opts.Daemon.Client(), before.ImageID)
		if goneErr != nil {
			return goneErr
		}
	}

	if item.Topology.SubjectCount > 1 {
		for _, name := range subjects.DependencyOrder {
			_, snap, macErr := docker.InspectSnapshot(ctx, opts.Daemon.Client(), name)
			if macErr != nil {
				return fmt.Errorf("inspect %s: %w", name, macErr)
			}

			if strings.HasPrefix(snap.ConfigMAC, "02:42:") {
				return errPreservedMAC
			}
		}
	}

	if item.Watchtower.HTTPAPIContainers != nil && *item.Watchtower.HTTPAPIContainers {
		enabledErr := docker.DetailsEnabledTrue(detailsBody, subjects.PrimaryName)
		if enabledErr != nil {
			return fmt.Errorf("containers/details: %w", enabledErr)
		}
	}

	porcelainPath := filepath.Join(caseDir, "watchtower.stdout.jsonl")

	raw, readErr := os.ReadFile(porcelainPath)
	if readErr == nil && item.Watchtower.Porcelain != nil && *item.Watchtower.Porcelain == "json" {
		porcelainFile := filepath.Join(caseDir, "porcelain.json")
		_ = os.WriteFile(porcelainFile, raw, permFile) //nolint:gosec // G703: caseDir is created by the harness.

		parsed, parseErr := assert.ParsePorcelain(raw)
		if parseErr == nil && item.Expect.Outcome == engine.OutcomeUpdated {
			updErr := assert.RequireUpdated(parsed)
			if updErr != nil {
				return fmt.Errorf("porcelain: %w", updErr)
			}
		}
	}

	return nil
}

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

// PersonaHosts documents extra_hosts for registry-persona cases.
//
// Parameters:
//   - persona: Registry dialect.
//   - proxyIP: Inner proxy IP.
//
// Returns:
//   - []string: extra_hosts entries.
func PersonaHosts(persona registry.Persona, proxyIP string) []string {
	return docker.ExtraHosts(persona, proxyIP)
}

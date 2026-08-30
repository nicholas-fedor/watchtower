package run

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/testing/e2e/assert"
	"github.com/nicholas-fedor/watchtower/testing/e2e/docker"
	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
	"github.com/nicholas-fedor/watchtower/testing/e2e/registry"
	"github.com/nicholas-fedor/watchtower/testing/e2e/report"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
	"github.com/nicholas-fedor/watchtower/testing/e2e/stream"
	"github.com/nicholas-fedor/watchtower/testing/e2e/watchtower"
)

// session is one case running on an acquired DinD worker.
type session struct {
	opts    Options
	item    engine.Case
	started time.Time
	caseID  string
	caseDir string
	prefix  string

	subjects docker.CreatedSubjects
	before   assert.InspectSnapshot
	after    assert.InspectSnapshot
	logs     string
	details  string
	inst     *watchtower.Instance
	exitCode int
	waitErr  error

	stdoutBuf bytes.Buffer
	stderrBuf bytes.Buffer
	outClose  io.Closer
	errClose  io.Closer
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
	sess := &session{
		opts:    opts,
		item:    item,
		started: time.Now(),
		caseID:  item.ID(),
	}

	return sess.run(ctx)
}

// run walks the case from reset through evaluate.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - engine.Result: Pass/fail/skip.
func (s *session) run(ctx context.Context) engine.Result {
	if engine.Unrealizable(s.item) {
		return engine.Result{
			CaseID:  s.caseID,
			Status:  "skip",
			Skipped: true,
			Err:     "unrealizable topology",
		}
	}

	openErr := s.openDir()
	if openErr != nil {
		return s.fail(openErr)
	}

	resetErr := s.opts.Daemon.Reset(ctx, s.prefix)
	if resetErr != nil {
		return s.fail(fmt.Errorf("reset: %w", resetErr))
	}

	regErr := s.startRegistry(ctx)
	if regErr != nil {
		return s.fail(regErr)
	}

	fixtureErr := s.createFixtures(ctx)
	if fixtureErr != nil {
		return s.fail(fixtureErr)
	}

	beforeErr := s.inspectBefore(ctx)
	if beforeErr != nil {
		return s.fail(beforeErr)
	}

	pubErr := s.publishReplacement(ctx)
	if pubErr != nil {
		return s.fail(pubErr)
	}

	faultErr := s.armFault(ctx)
	if faultErr != nil {
		return s.fail(faultErr)
	}

	startErr := s.startWatchtower(ctx)
	if startErr != nil {
		return s.fail(startErr)
	}
	defer s.inst.Close(ctx, s.opts.Daemon)

	awaitErr := s.await(ctx)
	if awaitErr != nil {
		return s.fail(awaitErr)
	}

	if s.item.Expect.Outcome == engine.OutcomeRejectConfig {
		return s.rejectConfig()
	}

	if s.waitErr != nil {
		return s.fail(s.waitErr)
	}

	afterErr := s.inspectAfter(ctx)
	if afterErr != nil {
		return s.fail(afterErr)
	}

	evalErr := s.evaluate(ctx)
	if evalErr != nil {
		return s.fail(evalErr)
	}

	return s.pass()
}

// openDir creates the case artifact directory and the Docker name prefix.
//
// Returns:
//   - error: Directory creation failure.
func (s *session) openDir() error {
	s.prefix = "e2e-" + s.caseID[:min(prefixIDLen, len(s.caseID))]
	if s.opts.Logs != nil {
		return nil
	}

	caseDir, dirErr := report.WriteCaseDir(s.opts.RunDir, report.Meta{
		ID:      s.caseID,
		Factors: s.item.Factors,
		Expect:  s.item.Expect,
	})
	if dirErr != nil {
		return fmt.Errorf("case dir: %w", dirErr)
	}

	s.caseDir = caseDir

	return nil
}

// logWriters returns stdout and stderr sinks for Watchtower.
//
// Parameters:
//   - ctx: Cancellation for Loki pushes.
//
// Returns:
//   - io.WriteCloser: Stdout sink.
//   - io.WriteCloser: Stderr sink.
func (s *session) logWriters(ctx context.Context) (io.WriteCloser, io.WriteCloser) {
	if s.opts.Logs != nil {
		outLog := stream.NewWriter(ctx, s.opts.Logs, s.opts.RunID, s.caseID, stream.StreamStdout)
		errLog := stream.NewWriter(ctx, s.opts.Logs, s.opts.RunID, s.caseID, stream.StreamStderr)

		return &teeCloser{Writer: io.MultiWriter(&s.stdoutBuf, outLog), closer: outLog},
			&teeCloser{Writer: io.MultiWriter(&s.stderrBuf, errLog), closer: errLog}
	}

	stdoutPath := filepath.Join(s.caseDir, "watchtower.stdout.jsonl")
	stderrPath := filepath.Join(s.caseDir, "watchtower.stderr.jsonl")
	stdout, _ := os.Create(stdoutPath)
	stderr, _ := os.Create(stderrPath)

	return stdout, stderr
}

// teeCloser writes to Writer and closes closer.
type teeCloser struct {
	io.Writer
	// closer is the stream.Writer being teed.
	closer io.Closer
}

// Close closes the underlying stream writer.
//
// Returns:
//   - error: Close failure.
func (t *teeCloser) Close() error {
	if t.closer == nil {
		return nil
	}

	return t.closer.Close()
}

// startRegistry starts distribution and the persona proxy inside DinD.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Registry or persona start failure.
func (s *session) startRegistry(ctx context.Context) error {
	regID, regErr := docker.StartInnerRegistry(ctx, s.opts.Daemon)
	if regErr != nil {
		return fmt.Errorf("inner registry: %w", regErr)
	}

	backend, backendErr := docker.RegistryBackendURL(ctx, s.opts.Daemon, regID)
	if backendErr != nil {
		return fmt.Errorf("registry backend: %w", backendErr)
	}

	persona := registry.Persona(s.item.Topology.RegistryPersona)

	personaErr := docker.StartPersona(ctx, s.opts.Daemon, s.opts.Artifacts.PersonaBinary, persona, backend)
	if personaErr != nil {
		return fmt.Errorf("persona: %w", personaErr)
	}

	return nil
}

// createFixtures publishes generation 1 and starts subject containers.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Publish or create failure.
func (s *session) createFixtures(ctx context.Context) error {
	kind := s.item.Topology.SubjectKind
	if kind == "" {
		kind = "echo"
	}

	publishErr := publishSubject(ctx, s.opts, kind, imageGeneration1)
	if publishErr != nil {
		return publishErr
	}

	imageRef := engine.ImageRefForPersona(s.item.Topology.RegistryPersona)
	if tagErr := docker.TagLocal(ctx, s.opts.Daemon.Client(), docker.SubjectPullRef(), imageRef); tagErr != nil {
		return tagErr
	}

	subjects, createErr := docker.CreateSubjects(ctx, s.opts.Daemon.Client(), s.prefix, imageRef, s.item.Topology)
	if createErr != nil {
		return fmt.Errorf("create subjects: %w", createErr)
	}

	s.subjects = subjects

	if !s.item.Topology.Decoy {
		return nil
	}

	decoyTopo := s.item.Topology
	decoyTopo.EnableLabel = "false"

	_, decoyErr := docker.CreateSubject(ctx, s.opts.Daemon.Client(), s.prefix+"-"+decoyName, imageRef, decoyTopo)
	if decoyErr != nil {
		return fmt.Errorf("create decoy: %w", decoyErr)
	}

	return nil
}

// inspectBefore snapshots the primary subject before Watchtower runs.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Inspect failure.
func (s *session) inspectBefore(ctx context.Context) error {
	beforeRaw, beforeSnap, inspectErr := docker.InspectSnapshot(ctx, s.opts.Daemon.Client(), s.subjects.PrimaryID)
	if inspectErr != nil {
		return fmt.Errorf("inspect before: %w", inspectErr)
	}

	s.before = beforeSnap
	if s.caseDir != "" {
		_ = os.WriteFile(filepath.Join(s.caseDir, "inspect-before.json"), beforeRaw, permFile)
	}

	if s.opts.Records != nil {
		_ = s.opts.Records.UpsertCase(ctx, store.Case{
			RunID:         s.opts.RunID,
			CaseID:        s.caseID,
			Status:        store.CaseRunning,
			InspectBefore: beforeRaw,
		})
	}

	return nil
}

// publishReplacement pushes generation 2 when the case expects a pull.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Publish failure.
func (s *session) publishReplacement(ctx context.Context) error {
	if s.item.Expect.Outcome != engine.OutcomeUpdated && s.item.Expect.Outcome != engine.OutcomeRateLimited {
		return nil
	}

	kind := s.item.Topology.SubjectKind
	if kind == "" {
		kind = "echo"
	}

	pubErr := publishSubject(ctx, s.opts, kind, imageGeneration2)
	if pubErr != nil {
		return pubErr
	}

	return docker.TagLocal(ctx, s.opts.Daemon.Client(), docker.SubjectPullRef(), engine.ImageRefForPersona(s.item.Topology.RegistryPersona))
}

// armFault programs the persona when Topology.RegistryFault is set.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Control-plane failure.
func (s *session) armFault(ctx context.Context) error {
	if s.item.Topology.RegistryFault == "" || s.item.Topology.RegistryFault == "none" {
		return nil
	}

	faultErr := docker.SetPersonaFault(ctx, s.opts.Daemon, registry.Fault(s.item.Topology.RegistryFault))
	if faultErr != nil {
		return fmt.Errorf("arm fault: %w", faultErr)
	}

	return nil
}

// startWatchtower launches Watchtower and rewrites case meta with argv and env.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Start failure.
func (s *session) startWatchtower(ctx context.Context) error {
	stdout, stderr := s.logWriters(ctx)
	s.outClose, s.errClose = stdout, stderr

	hosts := []string{}
	persona := registry.Persona(s.item.Topology.RegistryPersona)
	if persona != "" && persona != registry.PersonaNone {
		ip, ipErr := docker.PersonaProxyIP(ctx, s.opts.Daemon.Client())
		if ipErr != nil {
			return fmt.Errorf("persona ip: %w", ipErr)
		}

		hosts = docker.ExtraHosts(persona, ip)
		if hostErr := s.opts.Daemon.AppendHosts(ctx, hosts); hostErr != nil {
			return hostErr
		}
	}

	inst, argv, env, startErr := watchtower.Start(ctx, s.opts.Daemon, s.opts.Artifacts, s.item, stdout, stderr, hosts)
	if startErr != nil {
		return fmt.Errorf("start watchtower: %w", startErr)
	}

	s.inst = inst
	if s.caseDir != "" {
		_, _ = report.WriteCaseDir(s.opts.RunDir, report.Meta{
			ID:      s.caseID,
			Factors: s.item.Factors,
			Expect:  s.item.Expect,
			Argv:    argv,
			Env:     env,
		})
	}

	if s.opts.Records != nil {
		_ = s.opts.Records.UpsertCase(ctx, store.Case{
			RunID:   s.opts.RunID,
			CaseID:  s.caseID,
			Status:  store.CaseRunning,
			Factors: s.item.Factors,
			Expect:  mustJSON(s.item.Expect),
			Argv:    argv,
			Env:     env,
		})
	}

	return nil
}

// await waits for run-once exit or probes the HTTP API then stops Watchtower.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: HTTP API probe failure. Run-once wait errors are stored on the session.
func (s *session) await(ctx context.Context) error {
	httpAPI := s.item.Watchtower.HTTPAPIUpdate != nil && *s.item.Watchtower.HTTPAPIUpdate &&
		(s.item.Watchtower.RunOnce == nil || !*s.item.Watchtower.RunOnce)

	if httpAPI {
		select {
		case <-ctx.Done():
			s.waitErr = ctx.Err()
		case <-time.After(apiReadyWait):
		}

		token := ""
		if s.item.Watchtower.HTTPAPIToken != nil {
			token = *s.item.Watchtower.HTTPAPIToken
		}

		body, apiErr := docker.GetContainersDetails(ctx, s.opts.Daemon, s.inst.ID, token)
		if apiErr != nil {
			return fmt.Errorf("containers/details: %w", apiErr)
		}

		s.details = body
		stopTimeout := containerStopSeconds
		_, _ = s.opts.Daemon.Client().ContainerStop(ctx, s.inst.ID, client.ContainerStopOptions{Timeout: &stopTimeout})
	} else {
		s.exitCode, s.waitErr = watchtower.WaitRunOnce(ctx, s.opts.Daemon, s.inst)
	}

	if s.opts.Logs != nil {
		s.logs = s.stdoutBuf.String() + s.stderrBuf.String()
	} else {
		s.logs = readLogs(s.caseDir)
	}

	return nil
}

// inspectAfter snapshots the recreated primary subject.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Inspect failure.
func (s *session) inspectAfter(ctx context.Context) error {
	afterRaw, afterSnap, afterErr := docker.InspectSnapshot(ctx, s.opts.Daemon.Client(), s.subjects.PrimaryName)
	if afterErr != nil {
		listed, listErr := findRecreated(ctx, s.opts.Daemon.Client(), s.subjects.PrimaryName)
		if listErr != nil {
			return fmt.Errorf("inspect after: %w", afterErr)
		}

		afterRaw, afterSnap, afterErr = docker.InspectSnapshot(ctx, s.opts.Daemon.Client(), listed)
		if afterErr != nil {
			return fmt.Errorf("inspect after: %w", afterErr)
		}
	}

	s.after = afterSnap
	if s.caseDir != "" {
		_ = os.WriteFile(filepath.Join(s.caseDir, "inspect-after.json"), afterRaw, permFile)
	}

	if s.opts.Records != nil {
		_ = s.opts.Records.UpsertCase(ctx, store.Case{
			RunID:        s.opts.RunID,
			CaseID:       s.caseID,
			Status:       store.CaseRunning,
			InspectAfter: afterRaw,
		})
	}

	return nil
}

// rejectConfig passes when Watchtower exited non-zero as expected.
//
// Returns:
//   - engine.Result: Pass or fail.
func (s *session) rejectConfig() engine.Result {
	if s.waitErr != nil || s.exitCode != 0 {
		return s.pass()
	}

	return s.fail(errRejectConfigExitZero)
}

// fail records an error and writes failure.txt when the case dir exists.
//
// Parameters:
//   - err: Failure.
//
// Returns:
//   - engine.Result: Failed result.
func (s *session) fail(err error) engine.Result {
	result := engine.Result{
		CaseID:   s.caseID,
		Status:   "fail",
		Err:      err.Error(),
		Duration: time.Since(s.started).Milliseconds(),
	}
	if s.caseDir != "" {
		_ = report.WriteFailure(s.caseDir, result.Err)
	}

	return result
}

// pass marks the case successful and removes artifacts unless Keep is set.
//
// Returns:
//   - engine.Result: Passing result.
func (s *session) pass() engine.Result {
	result := engine.Result{
		CaseID:   s.caseID,
		Passed:   true,
		Status:   "pass",
		Duration: time.Since(s.started).Milliseconds(),
	}
	if !s.opts.Keep {
		_ = os.RemoveAll(s.caseDir)
	}

	return result
}

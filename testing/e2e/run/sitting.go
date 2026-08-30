package run

import (
	"context"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/nicholas-fedor/watchtower/testing/e2e/docker"
	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
	"github.com/nicholas-fedor/watchtower/testing/e2e/host"
	"github.com/nicholas-fedor/watchtower/testing/e2e/report"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
	"github.com/nicholas-fedor/watchtower/testing/e2e/stream"
	"github.com/nicholas-fedor/watchtower/testing/e2e/watchtower"
)

// permDir is the mode for artifacts/<run-id>.
const permDir = 0o750

// SittingRequest is the backend input for one cartesian sitting.
type SittingRequest struct {
	// Workers is the parallel DinD count.
	Workers int
	// Shard is i/n, or empty for all shards.
	Shard string
	// Offset skips this many selected cases.
	Offset int
	// Limit stops after N executed cases. Zero means no cap.
	Limit int
	// Generator is product, random, or file.
	Generator string
	// Seed is the random generator seed.
	Seed int64
	// Resume is an optional checkpoint.json path.
	Resume string
	// Filter is a regex on case ID or factor values.
	Filter string
	// Topic is a named development slice from engine.Topics.
	Topic string
	// Keep retains successful case artifact dirs.
	Keep bool
	// FilePath is the YAML path when Generator is file.
	FilePath string
	// RunID is the store sitting id when the control plane owns the run.
	RunID string
	// Records is the durable store. Nil keeps the checkpoint.json path.
	Records store.Store
	// Logs is the log stream backend. Nil writes jsonl files under artifacts.
	Logs stream.Logs
	// Skip is the resume set (completed case IDs).
	Skip interface{ Has(caseID string) bool }
	// OnPool reports busy/idle worker counts.
	OnPool func(busy, idle int)
	// OnCaseStart records a case that just acquired a worker.
	OnCaseStart func(caseID string)
	// OnCaseEnd records a case that just released a worker.
	OnCaseEnd func(caseID string)
}

// SittingResult is the aggregated outcome of one sitting.
type SittingResult struct {
	// RunID is artifacts/<run-id>.
	RunID string
	// Passed is the pass count.
	Passed int
	// Failed is the fail count.
	Failed int
	// Skipped is the skip count.
	Skipped int
}

// sitting is one cartesian or file-backed run against a DinD pool.
type sitting struct {
	req       SittingRequest
	runID     string
	runDir    string
	artifacts watchtower.Artifacts
	pool      *docker.Pool
	point     *engine.Checkpoint
	resume    interface{ Has(caseID string) bool }
	filters   []*regexp.Regexp
	shardIdx  int
	shardTot  int
	seq       iter.Seq[engine.Case]
}

// Sitting prepares artifacts, runs the scheduler, and writes reports.
//
// Parameters:
//   - ctx: Cancellation.
//   - req: Sitting knobs from the CLI.
//
// Returns:
//   - SittingResult: Counts and run ID.
//   - error: Setup, scheduler, or report failure. Case failures are in SittingResult.Failed.
func Sitting(ctx context.Context, req SittingRequest) (SittingResult, error) {
	sit := &sitting{req: req}

	prepErr := sit.prepare(ctx)
	if prepErr != nil {
		return SittingResult{}, prepErr
	}
	defer sit.pool.Close(ctx)

	return sit.execute(ctx)
}

// prepare builds artifacts, the DinD pool, checkpoint, filters, and case sequence.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Setup failure.
func (s *sitting) prepare(ctx context.Context) error {
	moduleRoot, rootErr := docker.ModuleRoot()
	if rootErr != nil {
		return fmt.Errorf("module root: %w", rootErr)
	}

	if s.req.RunID != "" {
		s.runID = s.req.RunID
	} else {
		s.runID = docker.StampRunID(time.Now(), docker.GitSHA(ctx, moduleRoot))
	}

	s.runDir = filepath.Join(moduleRoot, "artifacts", s.runID)
	if s.req.Records == nil {
		mkdirErr := os.MkdirAll(s.runDir, permDir)
		if mkdirErr != nil {
			return fmt.Errorf("run dir: %w", mkdirErr)
		}
	}

	identity := docker.GitSHA(ctx, moduleRoot)
	if identity == "" {
		identity = "dirty"
	}

	artifacts, prepErr := watchtower.Prepare(ctx, moduleRoot, watchtower.WatchtowerSource(), identity, watchtower.ImageSourceThin)
	if prepErr != nil {
		return fmt.Errorf("prepare artifacts: %w", prepErr)
	}

	s.artifacts = artifacts

	bound, boundErr := engine.WorkBound(s.req.Generator, s.req.FilePath, s.req.Limit)
	if boundErr != nil {
		return boundErr
	}

	s.req.Workers = host.CapWorkers(s.req.Workers, bound)

	var unset engine.Envelope

	pool, poolErr := docker.NewPool(ctx, s.req.Workers, unset)
	if poolErr != nil {
		return fmt.Errorf("dind pool: %w", poolErr)
	}

	s.pool = pool
	s.reportPool()

	s.point = &engine.Checkpoint{Completed: map[string]string{}, RunID: s.runID}

	s.resume = s.req.Skip
	if s.req.Records == nil {
		checkpointPath := filepath.Join(s.runDir, "checkpoint.json")
		if s.req.Resume != "" {
			checkpointPath = s.req.Resume
		}

		point, loadErr := engine.LoadCheckpoint(checkpointPath)
		if loadErr != nil {
			return fmt.Errorf("checkpoint: %w", loadErr)
		}

		point.RunID = s.runID

		s.point = point
		if s.resume == nil {
			s.resume = point
		}
	}

	filters, filterErr := engine.CompileFilters(s.req.Topic, s.req.Filter)
	if filterErr != nil {
		return fmt.Errorf("selector: %w", filterErr)
	}

	s.filters = filters

	shardIndex, shardTotal, shardErr := engine.ParseShard(s.req.Shard)
	if shardErr != nil {
		return fmt.Errorf("shard: %w", shardErr)
	}

	s.shardIdx = shardIndex
	s.shardTot = shardTotal

	seq, seqErr := engine.Sequence(engine.SequenceRequest{
		Generator: s.req.Generator,
		Seed:      s.req.Seed,
		FilePath:  s.req.FilePath,
	})
	if seqErr != nil {
		return fmt.Errorf("case sequence: %w", seqErr)
	}

	if len(s.filters) > 0 && (s.req.Generator == "" || s.req.Generator == "product") {
		seq = engine.ProductMatching(engine.Model(), s.filters)
	}

	s.seq = seq

	return nil
}

// execute runs the scheduler and writes the sitting report.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - SittingResult: Counts and run ID.
//   - error: Scheduler, report, or case failure.
func (s *sitting) execute(ctx context.Context) (SittingResult, error) {
	started := time.Now()
	sched := engine.Scheduler{
		Workers:    s.req.Workers,
		ShardIndex: s.shardIdx,
		ShardTotal: s.shardTot,
		Offset:     s.req.Offset,
		Limit:      s.req.Limit,
		Filters:    s.filters,
		Resume:     s.resume,
		Run:        s.runCase,
	}

	results, runErr := sched.RunStream(ctx, s.seq)
	if runErr != nil {
		return SittingResult{}, fmt.Errorf("scheduler: %w", runErr)
	}

	summary := report.Summary{
		RunID:    s.runID,
		Started:  started,
		Finished: time.Now(),
		Cases:    results,
	}
	for _, result := range results {
		switch result.Status {
		case "pass":
			summary.Passed++
		case "skip":
			summary.Skipped++
		default:
			summary.Failed++
		}
	}

	if s.req.Records == nil {
		writeErr := report.Write(s.runDir, summary)
		if writeErr != nil {
			return SittingResult{}, fmt.Errorf("write report: %w", writeErr)
		}
	}

	return SittingResult{
		RunID:   s.runID,
		Passed:  summary.Passed,
		Failed:  summary.Failed,
		Skipped: summary.Skipped,
	}, nil
}

// runCase acquires a worker, executes the case, and records the checkpoint.
//
// Parameters:
//   - runCtx: Cancellation for this case.
//   - item: Case vector.
//
// Returns:
//   - engine.Result: Pass/fail/skip.
func (s *sitting) runCase(runCtx context.Context, item engine.Case) engine.Result {
	if startErr := s.startCase(runCtx, item); startErr != nil {
		return s.finishCase(runCtx, item, engine.Result{CaseID: item.ID(), Status: "fail", Err: startErr.Error()})
	}

	daemon, acquireErr := s.pool.Acquire(runCtx)
	if acquireErr != nil {
		return s.finishCase(runCtx, item, engine.Result{CaseID: item.ID(), Status: "fail", Err: acquireErr.Error()})
	}

	s.reportPool()
	defer func() {
		s.pool.Release(daemon)
		s.reportPool()
	}()

	result := Execute(runCtx, Options{
		Daemon:    daemon,
		Artifacts: s.artifacts,
		RunDir:    s.runDir,
		Keep:      s.req.Keep,
		RunID:     s.runID,
		Logs:      s.req.Logs,
		Records:   s.req.Records,
	}, item)

	if s.req.Records == nil {
		_ = s.point.Record(result.CaseID, result.Status)
	}

	if runCtx.Err() != nil && result.Status == "fail" {
		result.Status = "interrupted"
		result.Passed = false
	}

	return s.finishCase(runCtx, item, result)
}

// reportPool publishes busy/idle counts when OnPool is set.
func (s *sitting) reportPool() {
	if s.req.OnPool == nil || s.pool == nil {
		return
	}

	busy, idle := s.pool.Stats()
	s.req.OnPool(busy, idle)
}

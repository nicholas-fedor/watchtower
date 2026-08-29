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
	"github.com/nicholas-fedor/watchtower/testing/e2e/report"
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
//   - error: Setup, scheduler, or report failure. ErrCasesFailed when any case failed.
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

	s.runID = docker.StampRunID(time.Now(), docker.GitSHA(ctx, moduleRoot))
	s.runDir = filepath.Join(moduleRoot, "artifacts", s.runID)

	mkdirErr := os.MkdirAll(s.runDir, permDir)
	if mkdirErr != nil {
		return fmt.Errorf("run dir: %w", mkdirErr)
	}

	artifacts, prepErr := watchtower.Prepare(ctx, moduleRoot, watchtower.WatchtowerSource(), s.runID, watchtower.ImageSourceThin)
	if prepErr != nil {
		return fmt.Errorf("prepare artifacts: %w", prepErr)
	}

	s.artifacts = artifacts

	var unset engine.Envelope

	pool, poolErr := docker.NewPool(ctx, s.req.Workers, unset)
	if poolErr != nil {
		return fmt.Errorf("dind pool: %w", poolErr)
	}

	s.pool = pool

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
		Resume:     s.point,
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

	writeErr := report.Write(s.runDir, summary)
	if writeErr != nil {
		return SittingResult{}, fmt.Errorf("write report: %w", writeErr)
	}

	out := SittingResult{
		RunID:   s.runID,
		Passed:  summary.Passed,
		Failed:  summary.Failed,
		Skipped: summary.Skipped,
	}
	if out.Failed > 0 {
		return out, fmt.Errorf("%w: %d", ErrCasesFailed, out.Failed)
	}

	return out, nil
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
	daemon, acquireErr := s.pool.Acquire(runCtx)
	if acquireErr != nil {
		return engine.Result{CaseID: item.ID(), Status: "fail", Err: acquireErr.Error()}
	}
	defer s.pool.Release(daemon)

	result := Execute(runCtx, Options{
		Daemon:    daemon,
		Artifacts: s.artifacts,
		RunDir:    s.runDir,
		Keep:      s.req.Keep,
	}, item)

	_ = s.point.Record(result.CaseID, result.Status)

	return result
}

package run

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	moduleRoot, rootErr := docker.ModuleRoot()
	if rootErr != nil {
		return SittingResult{}, fmt.Errorf("module root: %w", rootErr)
	}

	runID := docker.StampRunID(time.Now(), docker.GitSHA(ctx, moduleRoot))
	runDir := filepath.Join(moduleRoot, "artifacts", runID)

	mkdirErr := os.MkdirAll(runDir, permDir)
	if mkdirErr != nil {
		return SittingResult{}, fmt.Errorf("run dir: %w", mkdirErr)
	}

	artifacts, prepErr := watchtower.Prepare(ctx, moduleRoot, watchtower.WatchtowerSource(), runID, watchtower.ImageSourceThin)
	if prepErr != nil {
		return SittingResult{}, fmt.Errorf("prepare artifacts: %w", prepErr)
	}

	var unset engine.Envelope

	pool, poolErr := docker.NewPool(ctx, req.Workers, unset)
	if poolErr != nil {
		return SittingResult{}, fmt.Errorf("dind pool: %w", poolErr)
	}
	defer pool.Close(ctx)

	checkpointPath := filepath.Join(runDir, "checkpoint.json")
	if req.Resume != "" {
		checkpointPath = req.Resume
	}

	point, loadErr := engine.LoadCheckpoint(checkpointPath)
	if loadErr != nil {
		return SittingResult{}, fmt.Errorf("checkpoint: %w", loadErr)
	}

	point.RunID = runID

	filters, filterErr := engine.CompileFilters(req.Topic, req.Filter)
	if filterErr != nil {
		return SittingResult{}, fmt.Errorf("selector: %w", filterErr)
	}

	shardIndex, shardTotal, shardErr := engine.ParseShard(req.Shard)
	if shardErr != nil {
		return SittingResult{}, fmt.Errorf("shard: %w", shardErr)
	}

	seq, seqErr := engine.Sequence(engine.SequenceRequest{
		Generator: req.Generator,
		Seed:      req.Seed,
		FilePath:  req.FilePath,
	})
	if seqErr != nil {
		return SittingResult{}, fmt.Errorf("case sequence: %w", seqErr)
	}

	started := time.Now()
	sched := engine.Scheduler{
		Workers:    req.Workers,
		ShardIndex: shardIndex,
		ShardTotal: shardTotal,
		Offset:     req.Offset,
		Limit:      req.Limit,
		Filters:    filters,
		Resume:     point,
		Run: func(runCtx context.Context, item engine.Case) engine.Result {
			daemon, acquireErr := pool.Acquire(runCtx)
			if acquireErr != nil {
				return engine.Result{CaseID: item.ID(), Status: "fail", Err: acquireErr.Error()}
			}
			defer pool.Release(daemon)

			result := Execute(runCtx, Options{
				Daemon:    daemon,
				Artifacts: artifacts,
				RunDir:    runDir,
				Keep:      req.Keep,
			}, item)

			_ = point.Record(result.CaseID, result.Status)

			return result
		},
	}

	results, runErr := sched.RunStream(ctx, seq)
	if runErr != nil {
		return SittingResult{}, fmt.Errorf("scheduler: %w", runErr)
	}

	summary := report.Summary{
		RunID:    runID,
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

	writeErr := report.Write(runDir, summary)
	if writeErr != nil {
		return SittingResult{}, fmt.Errorf("write report: %w", writeErr)
	}

	out := SittingResult{
		RunID:   runID,
		Passed:  summary.Passed,
		Failed:  summary.Failed,
		Skipped: summary.Skipped,
	}
	if out.Failed > 0 {
		return out, fmt.Errorf("%w: %d", ErrCasesFailed, out.Failed)
	}

	return out, nil
}

package engine

import (
	"context"
	"hash/fnv"
	"iter"
	"regexp"
	"sync"
)

// Result is the scheduler-facing outcome of one case.
type Result struct {
	// CaseID is the stable case identifier.
	CaseID string `json:"case_id"`
	// Passed is true when Expect and invariants held.
	Passed bool `json:"passed"`
	// Skipped is true when the case was filtered, resumed, or unrealizable.
	Skipped bool `json:"skipped"`
	// Status is pass, fail, or skip.
	Status string `json:"status"`
	// Err is a failure message.
	Err string `json:"error,omitempty"`
	// Duration is wall time for the case.
	Duration int64 `json:"duration_ms"`
}

// RunFunc executes one case against a worker.
type RunFunc func(ctx context.Context, item Case) Result

// Scheduler fans a case stream across workers with shard, offset, limit, and resume.
type Scheduler struct {
	// Workers is the parallel DinD count. Values below 1 become 1.
	Workers int
	// ShardIndex is the 1-based shard (i in i/n). Zero means all shards.
	ShardIndex int
	// ShardTotal is n in i/n. Zero or 1 means no sharding.
	ShardTotal int
	// Offset skips this many selected cases before running.
	Offset int
	// Limit stops after this many executed cases. Zero means no limit.
	Limit int
	// Filters are AND-ed regexes matched against case ID and factor values.
	Filters []*regexp.Regexp
	// Resume skips IDs already in the checkpoint.
	Resume *Checkpoint
	// Run executes a selected case.
	Run RunFunc
}

// RunStream consumes seq, applies selection, and runs selected cases.
//
// Parameters:
//   - ctx: Cancellation.
//   - seq: Case iterator (Product, Random, or a File slice adapter).
//
// Returns:
//   - []Result: Per-case results in completion order.
//   - error: Worker or checkpoint failure.
func (s *Scheduler) RunStream(ctx context.Context, seq iter.Seq[Case]) ([]Result, error) {
	if s.Run == nil {
		return nil, ErrNoRunFunc
	}

	workers := max(s.Workers, 1)

	jobs := make(chan Case)
	results := make(chan Result)

	var waitGroup sync.WaitGroup

	for range workers {
		waitGroup.Go(func() {
			for item := range jobs {
				select {
				case <-ctx.Done():
					results <- Result{CaseID: item.ID(), Skipped: true, Status: "skip", Err: ctx.Err().Error()}
				default:
					results <- s.Run(ctx, item)
				}
			}
		})
	}

	var collectWait sync.WaitGroup

	collected := make([]Result, 0)

	collectWait.Go(func() {
		for result := range results {
			collected = append(collected, result)
		}
	})

	selected := 0
	executed := 0

	for item := range seq {
		if ctx.Err() != nil {
			break
		}

		if !s.selectCase(item) {
			continue
		}

		selected++
		if selected <= s.Offset {
			continue
		}

		if s.Limit > 0 && executed >= s.Limit {
			break
		}

		executed++

		jobs <- item
	}

	close(jobs)
	waitGroup.Wait()
	close(results)
	collectWait.Wait()

	return collected, nil
}

// selectCase reports whether the case belongs to this sitting.
//
// Parameters:
//   - item: Candidate case.
//
// Returns:
//   - bool: True when the case should run.
func (s *Scheduler) selectCase(item Case) bool {
	caseID := item.ID()
	if s.Resume != nil && s.Resume.Has(caseID) {
		return false
	}

	if Unrealizable(item) {
		return false
	}

	if s.ShardTotal > 1 {
		if shardOf(caseID, s.ShardTotal) != s.ShardIndex {
			return false
		}
	}

	if !matchFilters(s.Filters, item) {
		return false
	}

	return true
}

// shardOf maps a case ID onto shard index 1..n using FNV-1a.
//
// Parameters:
//   - caseID: Case identifier.
//   - total: Number of shards.
//
// Returns:
//   - int: 1-based shard index.
func shardOf(caseID string, total int) int {
	if total <= 1 {
		return 1
	}

	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(caseID))

	mod := hasher.Sum64() % uint64(total)
	if mod > uint64(^uint(0)>>1) {
		mod = 0
	}

	return int(mod) + 1
}

// matchFilters is true when every pattern matches the ID or a factor name/value.
//
// Parameters:
//   - filters: Compiled patterns. Empty means no extra restriction.
//   - item: Case to match.
//
// Returns:
//   - bool: True when all patterns match.
func matchFilters(filters []*regexp.Regexp, item Case) bool {
	for _, filter := range filters {
		if !matchFilter(filter, item) {
			return false
		}
	}

	return true
}

// matchFilter is true when the regex matches the ID or any factor value.
//
// Parameters:
//   - filter: Compiled regex.
//   - item: Case to match.
//
// Returns:
//   - bool: True on match.
func matchFilter(filter *regexp.Regexp, item Case) bool {
	if filter.MatchString(item.ID()) {
		return true
	}

	for name, level := range item.Factors {
		if filter.MatchString(name) || filter.MatchString(level) {
			return true
		}
	}

	return false
}

// CasesFromSlice adapts a slice to iter.Seq for File generators.
//
// Parameters:
//   - cases: Loaded cases.
//
// Returns:
//   - iter.Seq[Case]: Iterator over the slice.
func CasesFromSlice(cases []Case) iter.Seq[Case] {
	return func(yield func(Case) bool) {
		for _, item := range cases {
			if !yield(item) {
				return
			}
		}
	}
}

// ShardOf is exported for unit tests.
//
// Parameters:
//   - id: Case identifier.
//   - total: Number of shards.
//
// Returns:
//   - int: 1-based shard index.
func ShardOf(caseID string, total int) int {
	return shardOf(caseID, total)
}

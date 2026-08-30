package engine

import (
	"context"
	"maps"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatchtowerConfigEnvDenseComposeShape(t *testing.T) {
	t.Parallel()

	cfg := WatchtowerConfig{
		RunOnce:          new(true),
		Cleanup:          new(true),
		IncludeStopped:   new(true),
		ReviveStopped:    new(true),
		Debug:            new(true),
		Porcelain:        new("json"),
		LogFormat:        new("json"),
		LogLevel:         new("trace"),
		NoColor:          new(true),
		HTTPAPIEndpoints: StringsPtr("update", "health"),
		HTTPAPIToken:     new("e2e-api-token"),
		NotificationURL:  StringsPtr("generic://webhook.e2e/notify"),
	}

	env := cfg.Env()
	require.Equal(t, "true", env["WATCHTOWER_RUN_ONCE"])
	require.Equal(t, "true", env["WATCHTOWER_CLEANUP"])
	require.Equal(t, "true", env["WATCHTOWER_INCLUDE_STOPPED"])
	require.Equal(t, "true", env["WATCHTOWER_REVIVE_STOPPED"])
	require.Equal(t, "true", env["WATCHTOWER_DEBUG"])
	require.Equal(t, "json", env["WATCHTOWER_PORCELAIN"])
	require.Equal(t, "json", env["WATCHTOWER_LOG_FORMAT"])
	require.Equal(t, "trace", env["WATCHTOWER_LOG_LEVEL"])
	require.Equal(t, "true", env["NO_COLOR"])
	require.Equal(t, "update,health", env["WATCHTOWER_HTTP_API_ENDPOINTS"])
	require.Equal(t, "e2e-api-token", env["WATCHTOWER_HTTP_API_TOKEN"])
	require.Equal(t, "generic://webhook.e2e/notify", env["WATCHTOWER_NOTIFICATION_URL"])
	assert.Len(t, env, 12)
}

func TestWatchtowerConfigArgsDense(t *testing.T) {
	t.Parallel()

	cfg := WatchtowerConfig{
		RunOnce:        new(true),
		Cleanup:        new(true),
		IncludeStopped: new(true),
		ReviveStopped:  new(true),
		Porcelain:      new("json"),
	}

	args := cfg.Args()
	require.Contains(t, args, "--run-once")
	require.Contains(t, args, "--cleanup")
	require.Contains(t, args, "--include-stopped")
	require.Contains(t, args, "--revive-stopped")
	require.Contains(t, args, "--porcelain")
	require.Contains(t, args, "json")
}

func TestProductThreeFactorsYieldsTwelveStreamedCases(t *testing.T) {
	t.Parallel()

	factors := []Factor{
		{
			Name:   "alpha",
			Levels: []string{"a1", "a2"},
			Apply: func(c *Case, level string) {
				c.Factors["applied.alpha"] = level
			},
		},
		{
			Name:   "beta",
			Levels: []string{"b1", "b2", "b3"},
			Apply: func(c *Case, level string) {
				c.Factors["applied.beta"] = level
			},
		},
		{
			Name:   "gamma",
			Levels: []string{"c1", "c2"},
			Apply: func(c *Case, level string) {
				c.Factors["applied.gamma"] = level
			},
		},
	}

	require.Equal(t, int64(12), Cardinality(factors).Int64())

	seen := make([]Case, 0, 12)
	for item := range Product(factors) {
		seen = append(seen, item)
		require.Contains(t, item.Factors, "alpha")
		require.Contains(t, item.Factors, "beta")
		require.Contains(t, item.Factors, "gamma")
		require.Equal(t, item.Factors["alpha"], item.Factors["applied.alpha"])
		require.Equal(t, item.Factors["beta"], item.Factors["applied.beta"])
		require.Equal(t, item.Factors["gamma"], item.Factors["applied.gamma"])
		require.NotEmpty(t, item.ID())
	}

	require.Len(t, seen, 12)

	ids := make(map[string]struct{}, 12)
	for _, item := range seen {
		ids[item.ID()] = struct{}{}
	}

	assert.Len(t, ids, 12)
}

func TestRandomSeedIsReproducibleAndComplete(t *testing.T) {
	t.Parallel()

	factors := []Factor{
		{Name: "one", Levels: []string{"x", "y"}, Apply: func(*Case, string) {}},
		{Name: "two", Levels: []string{"p", "q", "r"}, Apply: func(*Case, string) {}},
	}

	first := takeRandom(factors, 7, 8)
	second := takeRandom(factors, 7, 8)

	require.Len(t, first, 8)
	require.Equal(t, first, second)

	for _, item := range first {
		require.Contains(t, item, "one")
		require.Contains(t, item, "two")
		require.NotEmpty(t, item["one"])
		require.NotEmpty(t, item["two"])
	}
}

// takeRandom collects the first limit factor maps from Random.
//
// Parameters:
//   - factors: Axes to sample.
//   - seed: RNG seed.
//   - limit: Number of draws.
//
// Returns:
//   - []map[string]string: Copied factor assignments.
func takeRandom(factors []Factor, seed int64, limit int) []map[string]string {
	out := make([]map[string]string, 0, limit)
	count := 0

	for item := range Random(factors, seed) {
		copied := make(map[string]string, len(item.Factors))
		maps.Copy(copied, item.Factors)

		out = append(out, copied)

		count++
		if count >= limit {
			break
		}
	}

	return out
}

func TestProductEmitsIntervalScheduleAsRejectConfig(t *testing.T) {
	t.Parallel()

	factors := []Factor{processShapeFactor()}
	found := false

	for item := range Product(factors) {
		if item.Shape != ShapeIntervalSchedule {
			continue
		}

		found = true

		require.Equal(t, OutcomeRejectConfig, item.Expect.Outcome)
		require.NotNil(t, item.Watchtower.Interval)
		require.NotNil(t, item.Watchtower.Schedule)
		require.Contains(t, item.Expect.RejectReason, "interval and schedule")
	}

	require.True(t, found)
}

func TestSchedulerShardIsDeterministic(t *testing.T) {
	t.Parallel()

	const total = 8

	counts := make([]int, total+1)
	for item := range Product([]Factor{
		{Name: "a", Levels: []string{"1", "2", "3", "4"}, Apply: func(*Case, string) {}},
		{Name: "b", Levels: []string{"x", "y", "z", "w"}, Apply: func(*Case, string) {}},
	}) {
		idx := ShardOf(item.ID(), total)
		require.GreaterOrEqual(t, idx, 1)
		require.LessOrEqual(t, idx, total)
		counts[idx]++
		require.Equal(t, idx, ShardOf(item.ID(), total))
	}

	sum := 0
	for shard := 1; shard <= total; shard++ {
		sum += counts[shard]
	}

	assert.Equal(t, 16, sum)
}

func TestSchedulerResumeSkipsCompletedIDs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	point, err := LoadCheckpoint(filepath.Join(dir, "checkpoint.json"))
	require.NoError(t, err)

	factors := []Factor{
		{Name: "n", Levels: []string{"a", "b", "c"}, Apply: func(*Case, string) {}},
	}

	var firstID string
	for item := range Product(factors) {
		firstID = item.ID()

		break
	}

	require.NoError(t, point.Record(firstID, "pass"))

	var ran atomic.Int64

	sched := Scheduler{
		Workers: 1,
		Resume:  point,
		Run: func(_ context.Context, item Case) Result {
			ran.Add(1)

			return Result{CaseID: item.ID(), Passed: true, Status: "pass"}
		},
	}

	results, runErr := sched.RunStream(t.Context(), Product(factors))
	require.NoError(t, runErr)
	assert.Equal(t, int64(2), ran.Load())
	assert.Len(t, results, 2)

	for _, result := range results {
		assert.NotEqual(t, firstID, result.CaseID)
	}
}

func TestLoadFileSmokeYAML(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "testdata", "cases", "smoke.yaml")
	cases, err := LoadFile(path)
	require.NoError(t, err)
	require.Len(t, cases, 1)
	require.Equal(t, "smoke", cases[0].ID())
	require.Equal(t, ShapeRunOnce, cases[0].Shape)
	require.Equal(t, ChannelFlags, cases[0].Channel)
	require.NotNil(t, cases[0].Watchtower.RunOnce)
	require.True(t, *cases[0].Watchtower.RunOnce)
	require.Nil(t, cases[0].Watchtower.Cleanup)
	require.Nil(t, cases[0].Watchtower.Scope)
	require.Nil(t, cases[0].Watchtower.LabelEnable)
	require.Equal(t, []string{"--run-once"}, cases[0].Watchtower.Args())
	require.False(t, cases[0].Topology.Decoy)
	require.Equal(t, OutcomeUpdated, cases[0].Expect.Outcome)
}

func TestLoadFileReferenceYAML(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "testdata", "cases", "reference.yaml")
	cases, err := LoadFile(path)
	require.NoError(t, err)
	require.Len(t, cases, 1)
	require.Equal(t, "example", cases[0].ID())
	require.Equal(t, ChannelFlags, cases[0].Channel)
	require.NotNil(t, cases[0].Watchtower.RunOnce)
	require.True(t, *cases[0].Watchtower.RunOnce)
	require.NotNil(t, cases[0].Watchtower.HTTPAPIToken)
	require.Equal(t, "echo", cases[0].Topology.SubjectKind)
	require.Equal(t, OutcomeUpdated, cases[0].Expect.Outcome)
}

func TestModelCoversRegisterAllFlags(t *testing.T) {
	t.Parallel()

	covered := CoveredFlags()
	for _, name := range FlagNames() {
		_, exists := covered[name]
		assert.True(t, exists, "flag %s missing from Model coverage", name)
	}
}

func TestUnrealizableBinaryPersona(t *testing.T) {
	t.Parallel()

	item := Case{
		Packaging: PackagingBinary,
		Topology:  Topology{RegistryPersona: "ghcr"},
	}
	assert.True(t, Unrealizable(item))
	item.Packaging = PackagingContainer
	assert.False(t, Unrealizable(item))
}

func TestParseShard(t *testing.T) {
	t.Parallel()

	index, total, err := ParseShard("")
	require.NoError(t, err)
	assert.Equal(t, 0, index)
	assert.Equal(t, 0, total)

	index, total, err = ParseShard("2/8")
	require.NoError(t, err)
	assert.Equal(t, 2, index)
	assert.Equal(t, 8, total)

	_, _, err = ParseShard("foo")
	require.ErrorIs(t, err, ErrShardSyntax)

	_, _, err = ParseShard("9/8")
	require.ErrorIs(t, err, ErrShardRange)
}

func TestUncoveredFlagsEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, UncoveredFlags())
}

func TestFilterStackMonitorSkipUsesInnerRegistryRef(t *testing.T) {
	t.Parallel()

	item := Case{}
	personaFactor().Apply(&item, "lscr")
	filterStackFactor().Apply(&item, "monitor-skip")
	require.Equal(t, []string{"lscr.io/e2e/app:latest"}, *item.Watchtower.MonitorImageNames)
	require.Equal(t, []string{SkipImageRef()}, *item.Watchtower.SkipImageNames)
	require.Equal(t, "127.0.0.1:5000/e2e/app:latest", SubjectImageRef())
	require.Equal(t, "lscr.io/e2e/app:latest", ImageRefForPersona("lscr"))
}

func TestWorkBoundFileAndLimit(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "testdata", "cases", "smoke.yaml")
	n, err := WorkBound(generatorFile, path, 0)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	n, err = WorkBound(generatorFile, path, 20)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	n, err = WorkBound(generatorProduct, "", 20)
	require.NoError(t, err)
	require.Equal(t, 20, n)

	n, err = WorkBound(generatorProduct, "", 0)
	require.NoError(t, err)
	require.Equal(t, 0, n)
}

func TestSequenceFileRequiresPath(t *testing.T) {
	t.Parallel()

	_, err := Sequence(SequenceRequest{Generator: generatorFile})
	require.ErrorIs(t, err, ErrFileGeneratorNeedsPath)
}

func TestLookupTopic(t *testing.T) {
	t.Parallel()

	topic, err := LookupTopic("ratelimit")
	require.NoError(t, err)
	assert.Equal(t, "ratelimit", topic.Name)
	assert.Contains(t, topic.Filter, "429-ghcr")

	_, err = LookupTopic("not-a-topic")
	require.ErrorIs(t, err, ErrUnknownTopic)
}

func TestProductMatchingRatelimitFindsCasesQuickly(t *testing.T) {
	t.Parallel()

	filters, err := CompileFilters("ratelimit", "")
	require.NoError(t, err)

	n := 0
	for item := range ProductMatching(Model(), filters) {
		require.True(t, matchFilters(filters, item))
		n++
		if n >= 20 {
			break
		}
	}

	require.Equal(t, 20, n)
}

func TestProductMatchingAndsTopicAndShape(t *testing.T) {
	t.Parallel()

	filters, err := CompileFilters("ratelimit", "run-once")
	require.NoError(t, err)

	n := 0
	for item := range ProductMatching(Model(), filters) {
		require.True(t, matchFilters(filters, item))
		n++
		if n >= 5 {
			break
		}
	}

	require.Equal(t, 5, n)
}

func TestCompileFiltersAndsTopicAndExtra(t *testing.T) {
	t.Parallel()

	filters, err := CompileFilters("ratelimit", "run-once")
	require.NoError(t, err)
	require.Len(t, filters, 2)
	assert.True(t, filters[0].MatchString("429-ghcr"))
	assert.True(t, filters[1].MatchString("run-once"))
}

func TestBuildInventory(t *testing.T) {
	t.Parallel()

	inv := BuildInventory("product", true)
	require.Equal(t, "product", inv.Generator)
	require.NotEmpty(t, inv.Cardinality)
	require.Positive(t, inv.FactorCount)
	require.NotEmpty(t, inv.FirstID)
	require.NotEmpty(t, inv.LastID)
	require.Len(t, inv.Factors, inv.FactorCount)
}

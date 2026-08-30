package host

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecommend(t *testing.T) {
	require.Equal(t, 1, Recommend(1, 512<<20))
	require.Equal(t, 1, Recommend(2, 2<<30))
	require.Equal(t, 4, Recommend(8, 16<<30))
	require.Equal(t, 8, Recommend(64, 64<<30))
}

func TestResolveWorkers(t *testing.T) {
	require.Equal(t, 4, ResolveWorkers(4, 2))
	require.Equal(t, 2, ResolveWorkers(0, 2))
	require.Equal(t, 1, ResolveWorkers(0, 0))
}

func TestCapWorkers(t *testing.T) {
	require.Equal(t, 1, CapWorkers(8, 1))
	require.Equal(t, 8, CapWorkers(8, 0))
	require.Equal(t, 8, CapWorkers(8, 20))
	require.Equal(t, 1, CapWorkers(0, 1))
}

func TestDiscover(t *testing.T) {
	snap, err := Discover("/")
	require.NoError(t, err)
	require.GreaterOrEqual(t, snap.CPUs, 1)
	require.Positive(t, snap.MemoryTotalBytes)
	require.GreaterOrEqual(t, snap.RecommendedWorkers, 1)
	require.LessOrEqual(t, snap.RecommendedWorkers, maxAutoWorkers)
}

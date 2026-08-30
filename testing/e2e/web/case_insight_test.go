package web

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
	"github.com/nicholas-fedor/watchtower/testing/e2e/stream"
)

func TestDiffInspectShowsImageAndSkipsGraphDriver(t *testing.T) {
	before := json.RawMessage(`{
		"Image": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"Id": "container-a",
		"GraphDriver": {"Name": "overlay2", "Data": {"UpperDir": "/a"}},
		"LogPath": "/var/lib/docker/a.log",
		"Config": {"Env": ["TAG=r1", "PATH=/bin"], "Image": "e2e/app:latest"}
	}`)
	after := json.RawMessage(`{
		"Image": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"Id": "container-b",
		"GraphDriver": {"Name": "overlay2", "Data": {"UpperDir": "/b"}},
		"LogPath": "/var/lib/docker/b.log",
		"Config": {"Env": ["TAG=r2", "PATH=/bin"], "Image": "e2e/app:latest"}
	}`)

	got := diffInspect(before, after)
	paths := make([]string, 0, len(got))
	for _, row := range got {
		paths = append(paths, row.Path)
	}

	require.Contains(t, paths, "Image")
	require.Contains(t, paths, "Id")
	require.Contains(t, paths, "Config.Env")
	require.NotContains(t, paths, "GraphDriver")
	require.NotContains(t, paths, "LogPath")
	require.NotContains(t, paths, "Config.Image")

	verdict := buildVerdict(store.Case{
		Expect:        json.RawMessage(`{"outcome":"updated"}`),
		InspectBefore: before,
		InspectAfter:  after,
	})
	require.True(t, verdict.ImageChanged)
	require.True(t, verdict.Recreated)
	require.Equal(t, "sha256:aaaaaaaaaaaa", verdict.ImageBefore)
	require.Equal(t, "sha256:bbbbbbbbbbbb", verdict.ImageAfter)
}

func TestParseLogsJSONMessageAndNoise(t *testing.T) {
	rows := parseLogs([]stream.Line{
		{Stream: stream.StreamStderr, Body: `{"level":"debug","time":"2026-08-30T18:03:19Z","message":"Filter built"}`},
		{Stream: stream.StreamStderr, Body: `{"level":"error","time":"2026-08-30T18:03:20Z","message":"Unable to update"}`},
		{Stream: stream.StreamStdout, Body: "not json"},
	})
	require.Len(t, rows, 3)
	require.True(t, rows[0].Noise)
	require.Equal(t, "Filter built", rows[0].Message)
	require.Equal(t, "debug", rows[0].Level)
	require.Equal(t, "18:03:19", rows[0].Time)
	require.False(t, rows[1].Noise)
	require.Equal(t, "Unable to update", rows[1].Message)
	require.Equal(t, "not json", rows[2].Message)
	require.Equal(t, stream.StreamStdout, rows[2].Stream)
}

func TestParseLogsHidesBatchingWrappersAndFragments(t *testing.T) {
	rows := parseLogs([]stream.Line{
		{Body: `{"level":"debug","notify":"no","message":"Watchtower started","entry_level":"info","time":"2026-08-30T18:03:20Z","message":"Log entry queued for batching"}`},
		{Body: `{`},
		{Body: `"containers": []`},
		{Body: `}`},
		{Body: `{"level":"info","time":"2026-08-30T18:03:20Z","message":"Update session completed"}`},
	})
	require.Len(t, rows, 5)
	require.True(t, rows[0].Noise)
	require.True(t, rows[1].Noise)
	require.True(t, rows[2].Noise)
	require.True(t, rows[3].Noise)
	require.False(t, rows[4].Noise)
	require.Equal(t, "Update session completed", rows[4].Message)
}

package stream

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	json "encoding/json/v2"

	"github.com/stretchr/testify/require"
)

type captureLogs struct {
	ctx context.Context
}

func (c *captureLogs) Push(ctx context.Context, _, _, _ string, _ []Line) error {
	c.ctx = ctx

	return nil
}

func (c *captureLogs) Query(context.Context, string, string, string) ([]Line, error) {
	return nil, nil
}

func (c *captureLogs) Ready(context.Context) error { return nil }

func (c *captureLogs) Close() error { return nil }

func TestWriterBatchesLines(t *testing.T) {
	mem := NewMemory()
	w := NewWriter(t.Context(), mem, "run", "case", StreamStdout)

	_, err := io.WriteString(w, "hello\nworld")
	require.NoError(t, err)

	lines, err := mem.Query(t.Context(), "run", "case", StreamStdout)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	require.Equal(t, "hello", lines[0].Body)

	require.NoError(t, w.Close())

	lines, err = mem.Query(t.Context(), "run", "case", StreamStdout)
	require.NoError(t, err)
	require.Len(t, lines, 2)
	require.Equal(t, "world", lines[1].Body)
}

func TestMemoryQueryBothStreams(t *testing.T) {
	mem := NewMemory()
	require.NoError(t, mem.Push(t.Context(), "r", "c", StreamStdout, []Line{{Body: "out"}}))
	require.NoError(t, mem.Push(t.Context(), "r", "c", StreamStderr, []Line{{Body: "err"}}))

	all, err := mem.Query(t.Context(), "r", "c", "")
	require.NoError(t, err)
	require.Len(t, all, 2)
}

func TestWriterPushContext(t *testing.T) {
	cap := &captureLogs{}
	w := NewWriter(t.Context(), cap, "run", "case", StreamStdout)
	_, err := io.WriteString(w, "x\n")
	require.NoError(t, err)
	require.Equal(t, t.Context(), cap.ctx)
}

func TestWriterNilSafe(t *testing.T) {
	var w *Writer
	n, err := w.Write([]byte("x"))
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.NoError(t, w.Close())
}

func TestWriterCloseEmpty(t *testing.T) {
	mem := NewMemory()
	w := NewWriter(t.Context(), mem, "run", "case", StreamStdout)
	require.NoError(t, w.Close())
	require.NoError(t, w.Close())
}

func TestLokiPushAndQuery(t *testing.T) {
	var pushed []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/loki/api/v1/push", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		pushed, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/loki/api/v1/query_range", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		require.Contains(t, q, `run_id="run"`)
		require.Contains(t, q, `stream="stdout"`)
		out := lokiQuery{Status: "success"}
		out.Data.Result = []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		}{{
			Stream: map[string]string{"stream": StreamStdout},
			Values: [][]string{{"1", "hello"}},
		}}
		_ = json.MarshalWrite(w, out)
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewTestServer(t, mux)
	l := OpenLoki("http://e2e.test")
	l.client = srv.Client()

	require.NoError(t, l.Ready(t.Context()))
	require.NoError(t, l.Push(t.Context(), "run", "case", StreamStdout, []Line{{Body: "hello"}}))
	require.Contains(t, string(pushed), `"job":"watchtower-e2e"`)

	lines, err := l.Query(t.Context(), "run", "case", StreamStdout)
	require.NoError(t, err)
	require.Equal(t, "hello", lines[0].Body)
}

func TestLokiPushEmptyIsNoop(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/loki/api/v1/push", func(http.ResponseWriter, *http.Request) {
		t.Fatal("push should not be called")
	})
	srv := httptest.NewTestServer(t, mux)
	l := OpenLoki("http://e2e.test")
	l.client = srv.Client()
	require.NoError(t, l.Push(t.Context(), "run", "case", StreamStdout, nil))
}

func TestLokiHTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/loki/api/v1/push", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewTestServer(t, mux)
	l := OpenLoki("http://e2e.test")
	l.client = srv.Client()
	err := l.Push(t.Context(), "run", "case", StreamStdout, []Line{{Body: "x"}})
	require.ErrorIs(t, err, ErrHTTP)
}

func TestEscapeLabel(t *testing.T) {
	require.Equal(t, `a\"b\\c`, escapeLabel(`a"b\c`))
}

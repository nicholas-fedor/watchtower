package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"testing/synctest"

	json "encoding/json/v2"

	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
)

func TestClientWaitCanceled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.MarshalWrite(w, store.Run{ID: "id", Status: store.RunRunning})
		}))
		cli := NewClientAt("http://e2e.test", "")
		cli.UseHTTP(srv.Client())

		ctx, cancel := context.WithCancel(t.Context())
		errCh := make(chan error, 1)
		go func() {
			_, err := cli.Wait(ctx, "id")
			errCh <- err
		}()

		synctest.Sleep(waitTick)
		cancel()
		require.ErrorIs(t, <-errCh, context.Canceled)
	})
}

func TestClientErrorStatus(t *testing.T) {
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"missing"}`)
	}))
	cli := NewClientAt("http://e2e.test", "")
	cli.UseHTTP(srv.Client())

	_, err := cli.GetRun(t.Context(), "missing")
	require.ErrorIs(t, err, ErrStatus)
	require.Contains(t, err.Error(), "missing")
}

func TestClientListCasesQueryEncoding(t *testing.T) {
	var rawQuery string
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_ = json.MarshalWrite(w, map[string]any{"cases": []store.Case{}, "total": 0})
	}))
	cli := NewClientAt("http://e2e.test", "")
	cli.UseHTTP(srv.Client())

	_, _, err := cli.ListCases(t.Context(), "run", "fail", "a b&c")
	require.NoError(t, err)

	q, err := url.ParseQuery(rawQuery)
	require.NoError(t, err)
	require.Equal(t, "fail", q.Get("status"))
	require.Equal(t, "a b&c", q.Get("q"))
}

func TestClientLogsStreamQuery(t *testing.T) {
	var rawQuery string
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_ = json.MarshalWrite(w, map[string]any{"lines": []any{}})
	}))
	cli := NewClientAt("http://e2e.test", "")
	cli.UseHTTP(srv.Client())

	_, err := cli.Logs(t.Context(), "run", "case", "stderr")
	require.NoError(t, err)
	require.Equal(t, "stream=stderr", rawQuery)
}

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/testing/e2e/api"
)

func TestEnsureAlreadyHealthy(t *testing.T) {
	ensureMu.Lock()
	ensureLive = false
	ensureMu.Unlock()

	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	cli := api.NewClientAt("http://e2e.test", "")
	cli.UseHTTP(srv.Client())

	var started atomic.Int32
	err := ensure(t.Context(), cli, func(context.Context) {
		started.Add(1)
	}, nil)
	require.NoError(t, err)
	require.Equal(t, int32(0), started.Load())
}

func TestEnsureStartsOnce(t *testing.T) {
	ensureMu.Lock()
	ensureLive = false
	ensureMu.Unlock()

	ready := atomic.Bool{}
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	cli := api.NewClientAt("http://e2e.test", "")
	cli.UseHTTP(srv.Client())

	var started atomic.Int32
	err := ensure(t.Context(), cli, func(context.Context) {
		started.Add(1)
		ready.Store(true)
	}, nil)
	require.NoError(t, err)
	require.Equal(t, int32(1), started.Load())

	err = ensure(t.Context(), cli, func(context.Context) {
		started.Add(1)
	}, nil)
	require.NoError(t, err)
	require.Equal(t, int32(1), started.Load())
}

func TestEnsureReturnsListenError(t *testing.T) {
	ensureMu.Lock()
	ensureLive = false
	ensureMu.Unlock()

	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	cli := api.NewClientAt("http://e2e.test", "")
	cli.UseHTTP(srv.Client())

	boom := errors.New("compose boom")
	errCh := make(chan error, 1)
	err := ensure(t.Context(), cli, func(context.Context) {
		errCh <- boom
	}, errCh)
	require.ErrorIs(t, err, boom)
}

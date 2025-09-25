// Package api provides an HTTP server for Watchtower’s API endpoints.
package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// serverReadTimeout defines the maximum duration for reading the request, including headers.
	serverReadTimeout = 10 * time.Second
	// serverWriteTimeout defines the maximum duration for writing the response.
	serverWriteTimeout = 10 * time.Minute
	// serverIdleTimeout defines the maximum duration for keeping idle connections alive.
	serverIdleTimeout = 30 * time.Second
	// serverMaxHeaderShift defines the bit shift for the maximum header size (1 MB).
	serverMaxHeaderShift = 20
	// serverShutdownTimeout defines the maximum duration allowed for the server to shut down gracefully.
	serverShutdownTimeout = 5 * time.Second
	// tokenPrefixLength defines the number of characters to reveal at the start of a masked token.
	tokenPrefixLength = 4
)

// Errors for API server operations.
var (
	// errServerFailed indicates a failure to start or run the HTTP server.
	errServerFailed = errors.New("http server failed")
	// errServerShutdownFailed indicates a failure during the HTTP server shutdown process.
	errServerShutdownFailed = errors.New("server shutdown failed")
)

// HTTPServer defines the interface for an HTTP server.
type HTTPServer interface {
	ListenAndServe() error
	Shutdown(ctx context.Context) error
}

// API is the http server responsible for serving the HTTP API endpoints.
type API struct {
	Token       string
	Addr        string // Set dynamically from flags
	hasHandlers bool
	mux         *http.ServeMux // Custom mux to avoid global collisions
}

// New is a factory function creating a new API instance.
func New(token, addr string) *API {
	api := &API{
		Token:       token,
		Addr:        addr,
		hasHandlers: false,
		mux:         http.NewServeMux(),
	}
	logrus.WithFields(logrus.Fields{
		"addr":  api.Addr,
		"token": maskToken(token),
	}).Debug("Initialized new API instance")

	return api
}

// RequireToken is a wrapper around http.HandleFunc that checks token validity.
func (api *API) RequireToken(handleFunc http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		want := "Bearer " + api.Token

		if auth != want {
			logrus.WithFields(logrus.Fields{
				"provided": maskToken(auth),
				"expected": maskToken(want),
			}).Warn("Invalid token attempt detected")
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		logrus.WithField("path", r.URL.Path).Debug("Valid token authenticated")
		handleFunc(w, r)
	}
}

// RegisterFunc is a wrapper around http.HandleFunc that also sets the flag used to determine whether to launch the API.
func (api *API) RegisterFunc(path string, fn http.HandlerFunc) {
	api.hasHandlers = true
	api.mux.HandleFunc(path, api.RequireToken(fn))
	logrus.WithField("path", path).Debug("Registered API function handler")
}

// RegisterHandler is a wrapper around http.Handler that also sets the flag used to determine whether to launch the API.
func (api *API) RegisterHandler(path string, handler http.Handler) {
	api.hasHandlers = true
	api.mux.Handle(path, api.RequireToken(handler.ServeHTTP))
	logrus.WithField("path", path).Debug("Registered API handler")
}

// Start launches the API server over HTTP, requiring a non-empty token.
func (api *API) Start(ctx context.Context, block bool) error {
	if !api.hasHandlers {
		logrus.WithField("addr", api.Addr).Debug("No handlers registered, skipping API start")

		return nil
	}

	if api.Token == "" {
		logrus.WithField("addr", api.Addr).Fatal("API token is empty or unset")
	}

	server := &http.Server{
		Addr:              api.Addr,
		Handler:           api.mux,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		ReadHeaderTimeout: serverReadTimeout,
		MaxHeaderBytes:    1 << serverMaxHeaderShift,
		TLSConfig:         nil,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
	}

	logrus.WithField("addr", api.Addr).Info("Starting HTTP API server")

	if block {
		return RunHTTPServer(ctx, server)
	}

	go func() {
		if err := RunHTTPServer(ctx, server); err != nil {
			logrus.WithError(err).
				WithField("addr", api.Addr).
				Debug("HTTP server encountered an error")
		}
	}()

	return nil
}

// RunHTTPServer starts the HTTP server with configured timeouts and handlers.
func RunHTTPServer(ctx context.Context, server HTTPServer) error {
	errChan := make(chan error, 1)

	go func() {
		logrus.Debug("Launching HTTP server listener")

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- fmt.Errorf("%w: %w", errServerFailed, err)
		} else {
			errChan <- nil
		}
	}()

	select {
	case err := <-errChan:
		if err != nil {
			logrus.WithError(err).Error("HTTP server failed to start or run")
		}

		return err
	case <-ctx.Done():
		logrus.Info("Initiating HTTP server shutdown due to context cancellation")

		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, serverShutdownTimeout)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			logrus.WithError(err).Debug("Failed to shut down HTTP server")

			return fmt.Errorf("%w: %w", errServerShutdownFailed, err)
		}

		logrus.Info("HTTP server shut down successfully")

		return nil
	}
}

// maskToken obscures a token string for safe logging, showing only the first few characters.
// It helps prevent sensitive data exposure in logs.
func maskToken(token string) string {
	if len(token) <= tokenPrefixLength {
		return strings.Repeat("*", len(token))
	}

	return token[:tokenPrefixLength] + strings.Repeat("*", len(token)-tokenPrefixLength)
}

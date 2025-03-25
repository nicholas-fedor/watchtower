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
	serverWriteTimeout = 10 * time.Second
	// serverIdleTimeout defines the maximum duration for keeping idle connections alive.
	serverIdleTimeout = 30 * time.Second
	// serverMaxHeaderShift defines the bit shift for the maximum header size (1 MB).
	serverMaxHeaderShift = 20
	// serverShutdownTimeout defines the maximum duration allowed for the server to shut down gracefully.
	serverShutdownTimeout = 5 * time.Second
)

// errServerFailed indicates a failure in starting or running the HTTP server.
var errServerFailed = errors.New("http server failed")

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
func New(token string) *API {
	return &API{
		Token:       token,
		Addr:        ":8080", // Default here, overridden in run()
		hasHandlers: false,
		mux:         http.NewServeMux(),
	}
}

// RequireToken is a wrapper around http.HandleFunc that checks token validity.
func (api *API) RequireToken(handleFunc http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		want := "Bearer " + api.Token

		if auth != want {
			logrus.Warn("Invalid token attempt")
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		logrus.Debug("Valid token found.")
		handleFunc(w, r)
	}
}

// RegisterFunc is a wrapper around http.HandleFunc that also sets the flag used to determine whether to launch the API.
func (api *API) RegisterFunc(path string, fn http.HandlerFunc) {
	api.hasHandlers = true
	api.mux.HandleFunc(path, api.RequireToken(fn))
}

// RegisterHandler is a wrapper around http.Handler that also sets the flag used to determine whether to launch the API.
func (api *API) RegisterHandler(path string, handler http.Handler) {
	api.hasHandlers = true
	api.mux.Handle(path, api.RequireToken(handler.ServeHTTP))
}

// Start launches the API server over HTTP, requiring a non-empty token.
func (api *API) Start(ctx context.Context, block bool) error {
	if !api.hasHandlers {
		logrus.Debug("Watchtower HTTP API skipped.")

		return nil
	}

	if api.Token == "" {
		logrus.Fatal("api token is empty or has not been set. exiting")
	}

	server := &http.Server{
		Addr:                         api.Addr,
		Handler:                      api.mux,
		ReadTimeout:                  serverReadTimeout,
		WriteTimeout:                 serverWriteTimeout,
		IdleTimeout:                  serverIdleTimeout,
		ReadHeaderTimeout:            serverReadTimeout,
		MaxHeaderBytes:               1 << serverMaxHeaderShift,
		TLSConfig:                    nil,
		TLSNextProto:                 make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		BaseContext:                  func(_ net.Listener) context.Context { return ctx },
		DisableGeneralOptionsHandler: false, // Default behavior
		ConnState:                    nil,   // No custom connection state handling
		ErrorLog:                     nil,   // Use default logging
		ConnContext:                  nil,   // No custom connection context
		HTTP2:                        nil,   // No HTTP/2 specific config
		Protocols:                    nil,   // No custom protocols
	}

	if block {
		return RunHTTPServer(ctx, server)
	}

	go func() {
		if err := RunHTTPServer(ctx, server); err != nil {
			logrus.Errorf("HTTP server failed: %v", err)
		}
	}()

	return nil
}

// RunHTTPServer starts the HTTP server with configured timeouts and handlers.
func RunHTTPServer(ctx context.Context, server HTTPServer) error {
	errChan := make(chan error, 1)

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- fmt.Errorf("%w: %w", errServerFailed, err)
		} else {
			errChan <- nil
		}
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, serverShutdownTimeout)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("server shutdown failed: %w", err)
		}

		return nil
	}
}

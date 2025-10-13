// Package api provides the HTTP API server implementation for Watchtower.
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

// readHeaderTimeout is the timeout for reading request headers.
const readHeaderTimeout = 10 * time.Second

// shutdownTimeout is the timeout for graceful server shutdown.
const shutdownTimeout = 5 * time.Second

// API represents the HTTP API server for Watchtower.
type API struct {
	Token       string
	Addr        string // Set dynamically from flags
	hasHandlers bool
	mux         *http.ServeMux // Custom mux to avoid global collisions
	server      HTTPServer     // Optional injected server for testing
}

// New is a factory function creating a new API instance.
// The server parameter is optional and allows dependency injection for testing.
func New(token, addr string, server ...HTTPServer) *API {
	var injectedServer HTTPServer
	if len(server) > 0 {
		injectedServer = server[0]
	}

	api := &API{
		Token:       token,
		Addr:        addr,
		hasHandlers: false,
		mux:         http.NewServeMux(),
		server:      injectedServer,
	}
	logrus.WithFields(logrus.Fields{
		"addr":  api.Addr,
		"token": token,
	}).Debug("Initialized new API instance")

	return api
}

// RegisterFunc registers an HTTP handler function for the given path.
func (a *API) RegisterFunc(path string, handler func(http.ResponseWriter, *http.Request)) {
	a.mux.HandleFunc(path, handler)
	a.registered = true
}

// RegisterHandler registers an HTTP handler for the given path.
func (a *API) RegisterHandler(path string, handler http.Handler) {
	a.mux.Handle(path, handler)
	a.registered = true
}

// Start starts the HTTP API server.
// If blocking is true, it runs in the foreground and blocks until shutdown.
// If blocking is false, it runs in the background.
func (a *API) Start(ctx context.Context, blocking bool) error {
	if !a.registered {
		logrus.Info("No handlers registered, skipping API start")

		return nil
	}

	if a.token == "" {
		logrus.Fatal("API token is empty or unset")
	}

	var server HTTPServer
	if api.server != nil {
		// Use injected server for testing
		server = api.server
	} else {
		// Create real server for production
		server = &http.Server{
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
	}

	logrus.WithField("addr", api.Addr).Info("Starting HTTP API server")

	if block {
		return RunHTTPServer(ctx, server)
	}

	go func() {
		err := a.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.Error("HTTP server failed: ", err)
		}
	}()

	logrus.Info("HTTP API server started successfully")

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()

		if err := a.server.Shutdown(shutdownCtx); err != nil {
			logrus.WithError(err).Error("Failed to shutdown server")
		}
	}()

	return nil
}

// RequireToken wraps a handler function with authentication.
func (a *API) RequireToken(handler func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") ||
			strings.TrimPrefix(auth, "Bearer ") != a.token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)

			return
		}

		handler(w, r)
	}
}

// authMiddleware wraps the handler with authentication for all paths.
func (a *API) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") ||
			strings.TrimPrefix(auth, "Bearer ") != a.token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)

			return
		}

		next.ServeHTTP(w, r)
	})
}

// HTTPServer interface for RunHTTPServer.
type HTTPServer interface {
	ListenAndServe() error
	Shutdown(ctx context.Context) error
}

// RunHTTPServer starts the HTTP server and handles graceful shutdown.
func RunHTTPServer(ctx context.Context, server HTTPServer) error {
	errChan := make(chan error, 1)

	go func() {
		errChan <- server.ListenAndServe()
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown failed: %w", err)
		}

		return nil
	}
}

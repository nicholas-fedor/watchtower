// Package api provides the HTTP API server implementation for Watchtower.
package api

import (
	"context"
	"errors"
	"fmt"
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
	Addr       string
	token      string
	mux        *http.ServeMux
	server     *http.Server
	registered bool
}

// New creates a new API instance with the given authentication token.
func New(token string) *API {
	mux := http.NewServeMux()

	return &API{
		token: token,
		mux:   mux,
	}
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

	a.server = &http.Server{
		Addr:              a.Addr,
		Handler:           a.authMiddleware(a.mux),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	if blocking {
		errChan := make(chan error, 1)

		go func() {
			errChan <- a.server.ListenAndServe()
		}()

		logrus.Info("HTTP API server started successfully")

		select {
		case err := <-errChan:
			return err
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
			defer cancel()

			if err := a.server.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("server shutdown failed: %w", err)
			}

			return nil
		}
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

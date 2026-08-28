package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const headerTimeout = 10 * time.Second

// Serve runs the persona reverse proxy until ctx is cancelled.
//
// Parameters:
//   - ctx: Cancellation. Shutdown begins when ctx is done.
//   - listen: HTTP listen address (for example :80).
//   - backend: Distribution registry base URL.
//   - persona: Dialect to speak.
//
// Returns:
//   - error: Listen, proxy construction, or unexpected server failure.
func Serve(ctx context.Context, listen, backend string, persona Persona) error {
	proxy, err := NewProxy(persona, backend, NewController())
	if err != nil {
		return fmt.Errorf("persona proxy: %w", err)
	}

	server := &http.Server{
		Addr:              listen,
		Handler:           proxy,
		ReadHeaderTimeout: headerTimeout,
	}

	go func() {
		<-ctx.Done()

		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), headerTimeout)
		defer cancel()

		_ = server.Shutdown(shutCtx)
	}()

	serveErr := server.ListenAndServe()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("persona listen: %w", serveErr)
	}

	return nil
}

package assert

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// APIContractError describes an HTTP API assertion failure.
type APIContractError struct {
	// Message is the assertion failure text.
	Message string
}

// Error implements error.
func (e *APIContractError) Error() string {
	return e.Message
}

// RequireStatus accepts any of the listed HTTP statuses.
//
// Parameters:
//   - got: Actual status.
//   - want: Allowed statuses.
//
// Returns:
//   - error: When got is not in want.
func RequireStatus(got int, want []int) error {
	if slices.Contains(want, got) {
		return nil
	}

	return &APIContractError{Message: fmt.Sprintf("http status %d not in %v", got, want)}
}

// RequireUnauthorized is true for unauthenticated non-health endpoints.
//
// Parameters:
//   - status: HTTP status.
//
// Returns:
//   - error: When status is not 401.
func RequireUnauthorized(status int) error {
	if status == http.StatusUnauthorized {
		return nil
	}

	return &APIContractError{Message: fmt.Sprintf("expected 401, got %d", status)}
}

// RequireNoSecretsInConfig fails when /v1/config JSON echoes a secret.
//
// Parameters:
//   - body: /v1/config response body.
//   - secrets: Tokens that must be redacted.
//
// Returns:
//   - error: Leak error.
func RequireNoSecretsInConfig(body string, secrets []string) error {
	err := ForbiddenSecrets(body, secrets)
	if err != nil {
		return fmt.Errorf("v1/config: %w", err)
	}

	if strings.Contains(strings.ToLower(body), "bearer ") {
		return ErrConfigEchoedAuth
	}

	return nil
}

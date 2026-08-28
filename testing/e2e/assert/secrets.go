package assert

import (
	"strings"
)

// ForbiddenSecrets fails when any secret appears in the captured text.
//
// Parameters:
//   - haystack: Logs, porcelain, config JSON, or event payloads.
//   - secrets: Values that must not leak (tokens, passwords, userinfo).
//
// Returns:
//   - error: When a secret substring is present.
func ForbiddenSecrets(haystack string, secrets []string) error {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}

		if strings.Contains(haystack, secret) {
			return ErrSecretLeaked
		}
	}

	return nil
}

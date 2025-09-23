// Package auth provides Git authentication handling for Watchtower's Git monitoring feature.
package auth

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Predefined error variables for consistent error handling.
var (
	ErrUnsupportedAuthMethod = errors.New("unsupported authentication method")
	ErrSSHKeyPathEmpty       = errors.New("SSH key file path is empty")
	ErrTokenRequired         = errors.New("token authentication requires a token")
	ErrBasicAuthIncomplete   = errors.New(
		"basic authentication requires both username and password",
	)
	ErrSSHKeyRequired    = errors.New("SSH authentication requires a private key")
	ErrUnknownAuthMethod = errors.New("unknown authentication method")
)

// CreateAuthMethod creates a go-git authentication method from AuthConfig.
//
// It converts Watchtower's AuthConfig into the appropriate go-git transport.AuthMethod
// based on the authentication method specified (token, basic, SSH, or none).
//
// Parameters:
//   - config: Authentication configuration containing method and credentials.
//
// Returns:
//   - transport.AuthMethod: Configured authentication method for go-git operations.
//   - error: Non-nil if authentication method is unsupported or configuration is invalid.
func CreateAuthMethod(config types.AuthConfig) (transport.AuthMethod, error) {
	logrus.WithField("method", config.Method).Debug("Creating authentication method")

	switch config.Method {
	case types.AuthMethodToken:
		auth := createTokenAuth(config.Token)
		if auth != nil {
			logrus.Debug("Created token authentication method")
		} else {
			logrus.Debug("Token authentication not configured (empty token)")
		}

		return auth, nil
	case types.AuthMethodBasic:
		auth := createBasicAuth(config.Username, config.Password)
		if auth != nil {
			logrus.Debug("Created basic authentication method")
		} else {
			logrus.Debug("Basic authentication not configured (missing credentials)")
		}

		return auth, nil
	case types.AuthMethodSSH:
		auth, err := createSSHAuth(config.SSHKey)
		if err != nil {
			logrus.WithError(err).Debug("Failed to create SSH authentication method")
		} else {
			logrus.Debug("Created SSH authentication method")
		}

		return auth, err
	case types.AuthMethodNone:
		logrus.Debug("Using no authentication")

		return nil, nil //nolint:nilnil // No authentication needed is valid
	default:
		logrus.WithField("method", config.Method).Debug("Unsupported authentication method")

		return nil, fmt.Errorf("%w: %s", ErrUnsupportedAuthMethod, config.Method)
	}
}

// createTokenAuth creates HTTP token authentication.
func createTokenAuth(token string) transport.AuthMethod {
	if token == "" {
		return nil
	}

	return &http.BasicAuth{
		Username: "token", // GitHub/GitLab convention
		Password: token,
	}
}

// createBasicAuth creates username/password authentication.
func createBasicAuth(username, password string) transport.AuthMethod {
	if username == "" || password == "" {
		return nil
	}

	return &http.BasicAuth{
		Username: username,
		Password: password,
	}
}

// createSSHAuth creates SSH key authentication.
func createSSHAuth(sshKey []byte) (transport.AuthMethod, error) {
	if len(sshKey) == 0 {
		logrus.Debug("SSH key authentication failed: no key provided")

		return nil, ErrSSHKeyRequired
	}

	logrus.Debug("Creating SSH public key authentication")

	publicKeys, err := ssh.NewPublicKeys("git", sshKey, "")
	if err != nil {
		logrus.WithError(err).Debug("Failed to create SSH public keys")

		return nil, fmt.Errorf("failed to create SSH public keys: %w", err)
	}

	logrus.Debug("Successfully created SSH authentication")

	return publicKeys, nil
}

// LoadSSHKeyFromFile loads an SSH private key from a file.
//
// Parameters:
//   - filePath: Path to the SSH private key file.
//
// Returns:
//   - []byte: Raw SSH key data.
//   - error: Non-nil if file cannot be read or path is empty.
func LoadSSHKeyFromFile(filePath string) ([]byte, error) {
	if filePath == "" {
		logrus.Debug("SSH key file path is empty")

		return nil, ErrSSHKeyPathEmpty
	}

	logrus.WithField("path", filePath).Debug("Loading SSH key from file")

	keyData, err := os.ReadFile(filePath)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"path": filePath,
		}).WithError(err).Debug("Failed to read SSH key file")

		return nil, fmt.Errorf("failed to read SSH key file %s: %w", filePath, err)
	}

	logrus.WithField("path", filePath).Debug("Successfully loaded SSH key from file")

	return keyData, nil
}

// ParseAuthConfigFromFlags creates AuthConfig from command-line flags.
//
// It determines the authentication method based on which credentials are provided,
// following a priority order: token > basic auth > SSH key > none.
//
// Parameters:
//   - token: Personal access token for GitHub/GitLab.
//   - username: Username for basic authentication.
//   - password: Password for basic authentication.
//   - sshKeyPath: Path to SSH private key file.
//
// Returns:
//   - types.AuthConfig: Configured authentication configuration.
//   - error: Non-nil if SSH key loading fails.
func ParseAuthConfigFromFlags(
	token, username, password, sshKeyPath string,
) (types.AuthConfig, error) {
	config := types.AuthConfig{}

	logrus.Debug("Parsing authentication configuration from flags")

	// Determine auth method based on provided credentials
	// Priority order: token > basic auth > SSH key > none
	switch {
	case token != "":
		// Token authentication takes highest priority
		logrus.Debug("Using token authentication method")

		config.Method = types.AuthMethodToken
		config.Token = token
	case username != "" && password != "":
		// Basic authentication (username/password)
		logrus.Debug("Using basic authentication method")

		config.Method = types.AuthMethodBasic
		config.Username = username
		config.Password = password
	case sshKeyPath != "":
		// SSH key authentication
		logrus.WithField("path", sshKeyPath).Debug("Using SSH key authentication method")

		config.Method = types.AuthMethodSSH

		sshKey, err := LoadSSHKeyFromFile(sshKeyPath)
		if err != nil {
			return config, fmt.Errorf("failed to load SSH key: %w", err)
		}

		config.SSHKey = sshKey
	default:
		// No authentication method specified
		logrus.Debug("Using no authentication method")

		config.Method = types.AuthMethodNone
	}

	logrus.WithField("method", config.Method).
		Debug("Successfully parsed authentication configuration")

	return config, nil
}

// ValidateAuthConfig checks if the authentication configuration is valid.
//
// It verifies that the required credentials are present for the specified authentication method.
//
// Parameters:
//   - config: Authentication configuration to validate.
//
// Returns:
//   - error: Non-nil if configuration is invalid for the specified method.
func ValidateAuthConfig(config types.AuthConfig) error {
	logrus.WithField("method", config.Method).Debug("Validating authentication configuration")

	switch config.Method {
	case types.AuthMethodToken:
		if config.Token == "" {
			logrus.Debug("Token authentication validation failed: token is empty")

			return ErrTokenRequired
		}
	case types.AuthMethodBasic:
		if config.Username == "" || config.Password == "" {
			logrus.Debug("Basic authentication validation failed: missing username or password")

			return ErrBasicAuthIncomplete
		}
	case types.AuthMethodSSH:
		if len(config.SSHKey) == 0 {
			logrus.Debug("SSH authentication validation failed: no SSH key provided")

			return ErrSSHKeyRequired
		}
	case types.AuthMethodNone:
		// No validation needed for no auth
		logrus.Debug("No authentication method validation passed")
	default:
		logrus.WithField("method", config.Method).Debug("Unknown authentication method")

		return fmt.Errorf("%w: %s", ErrUnknownAuthMethod, config.Method)
	}

	logrus.WithField("method", config.Method).
		Debug("Authentication configuration validation successful")

	return nil
}

// IsPrivateRepo checks if a repository URL likely requires authentication.
//
// It uses common patterns to determine if a repository is likely private and requires authentication.
// This is a heuristic check based on URL patterns.
//
// Parameters:
//   - repoURL: Repository URL to check.
//
// Returns:
//   - bool: True if the repository likely requires authentication.
func IsPrivateRepo(repoURL string) bool {
	// Common patterns for private repositories
	privateIndicators := []string{
		"git@github.com:",
		"git@gitlab.com:",
		"git@bitbucket.org:",
		// Add more patterns as needed
	}

	for _, indicator := range privateIndicators {
		if strings.Contains(repoURL, indicator) {
			return true
		}
	}

	// If it contains a username, it's likely private
	if strings.Contains(repoURL, "@") && strings.Contains(repoURL, "://") {
		return true
	}

	return false
}

// GetDefaultAuthConfig returns a default authentication configuration.
//
// Returns:
//   - types.AuthConfig: Default configuration with no authentication method.
func GetDefaultAuthConfig() types.AuthConfig {
	return types.AuthConfig{
		Method: types.AuthMethodNone,
	}
}

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
func CreateAuthMethod(config types.AuthConfig) (transport.AuthMethod, error) {
	switch config.Method {
	case types.AuthMethodToken:
		return createTokenAuth(config.Token), nil
	case types.AuthMethodBasic:
		return createBasicAuth(config.Username, config.Password), nil
	case types.AuthMethodSSH:
		auth, err := createSSHAuth(config.SSHKey)

		return auth, err
	case types.AuthMethodNone:
		return nil, nil //nolint:nilnil // No authentication needed is valid
	default:
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
		return nil, ErrSSHKeyRequired
	}

	publicKeys, err := ssh.NewPublicKeys("git", sshKey, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH public keys: %w", err)
	}

	return publicKeys, nil
}

// LoadSSHKeyFromFile loads an SSH private key from a file.
func LoadSSHKeyFromFile(filePath string) ([]byte, error) {
	if filePath == "" {
		return nil, ErrSSHKeyPathEmpty
	}

	keyData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SSH key file %s: %w", filePath, err)
	}

	return keyData, nil
}

// ParseAuthConfigFromFlags creates AuthConfig from command-line flags.
func ParseAuthConfigFromFlags(
	token, username, password, sshKeyPath string,
) (types.AuthConfig, error) {
	config := types.AuthConfig{}

	// Determine auth method based on provided credentials
	switch {
	case token != "":
		config.Method = types.AuthMethodToken
		config.Token = token
	case username != "" && password != "":
		config.Method = types.AuthMethodBasic
		config.Username = username
		config.Password = password
	case sshKeyPath != "":
		config.Method = types.AuthMethodSSH

		sshKey, err := LoadSSHKeyFromFile(sshKeyPath)
		if err != nil {
			return config, fmt.Errorf("failed to load SSH key: %w", err)
		}

		config.SSHKey = sshKey
	default:
		config.Method = types.AuthMethodNone
	}

	return config, nil
}

// ValidateAuthConfig checks if the authentication configuration is valid.
func ValidateAuthConfig(config types.AuthConfig) error {
	switch config.Method {
	case types.AuthMethodToken:
		if config.Token == "" {
			return ErrTokenRequired
		}
	case types.AuthMethodBasic:
		if config.Username == "" || config.Password == "" {
			return ErrBasicAuthIncomplete
		}
	case types.AuthMethodSSH:
		if len(config.SSHKey) == 0 {
			return ErrSSHKeyRequired
		}
	case types.AuthMethodNone:
		// No validation needed for no auth
	default:
		return fmt.Errorf("%w: %s", ErrUnknownAuthMethod, config.Method)
	}

	return nil
}

// IsPrivateRepo checks if a repository URL likely requires authentication.
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
func GetDefaultAuthConfig() types.AuthConfig {
	return types.AuthConfig{
		Method: types.AuthMethodNone,
	}
}

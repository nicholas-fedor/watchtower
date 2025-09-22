// Package git provides Git repository operations for Watchtower's Git monitoring feature.
package git

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// CreateAuthMethod creates a go-git authentication method from AuthConfig.
func CreateAuthMethod(config AuthConfig) (transport.AuthMethod, error) {
	switch config.Method {
	case AuthMethodToken:
		return createTokenAuth(config.Token), nil
	case AuthMethodBasic:
		return createBasicAuth(config.Username, config.Password), nil
	case AuthMethodSSH:
		return createSSHAuth(config.SSHKey)
	case AuthMethodNone:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported auth method: %s", config.Method)
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
		return nil, nil
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
		return nil, fmt.Errorf("SSH key file path is empty")
	}

	keyData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SSH key file %s: %w", filePath, err)
	}

	return keyData, nil
}

// ParseAuthConfigFromFlags creates AuthConfig from command-line flags.
func ParseAuthConfigFromFlags(token, username, password, sshKeyPath string) (AuthConfig, error) {
	config := AuthConfig{}

	// Determine auth method based on provided credentials
	switch {
	case token != "":
		config.Method = AuthMethodToken
		config.Token = token
	case username != "" && password != "":
		config.Method = AuthMethodBasic
		config.Username = username
		config.Password = password
	case sshKeyPath != "":
		config.Method = AuthMethodSSH
		sshKey, err := LoadSSHKeyFromFile(sshKeyPath)
		if err != nil {
			return config, fmt.Errorf("failed to load SSH key: %w", err)
		}
		config.SSHKey = sshKey
	default:
		config.Method = AuthMethodNone
	}

	return config, nil
}

// ValidateAuthConfig checks if the authentication configuration is valid.
func ValidateAuthConfig(config AuthConfig) error {
	switch config.Method {
	case AuthMethodToken:
		if config.Token == "" {
			return fmt.Errorf("token authentication requires a token")
		}
	case AuthMethodBasic:
		if config.Username == "" || config.Password == "" {
			return fmt.Errorf("basic authentication requires both username and password")
		}
	case AuthMethodSSH:
		if len(config.SSHKey) == 0 {
			return fmt.Errorf("SSH authentication requires a private key")
		}
	case AuthMethodNone:
		// No validation needed for no auth
	default:
		return fmt.Errorf("unknown authentication method: %s", config.Method)
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
func GetDefaultAuthConfig() AuthConfig {
	return AuthConfig{
		Method: AuthMethodNone,
	}
}

package auth

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestCreateAuthMethod(t *testing.T) {
	tests := []struct {
		name     string
		config   types.AuthConfig
		wantNil  bool
		wantType any
		wantErr  error
	}{
		{
			name: "token auth",
			config: types.AuthConfig{
				Method: types.AuthMethodToken,
				Token:  "test-token",
			},
			wantNil:  false,
			wantType: &http.BasicAuth{},
		},
		{
			name: "empty token auth",
			config: types.AuthConfig{
				Method: types.AuthMethodToken,
				Token:  "",
			},
			wantNil: true,
		},
		{
			name: "basic auth",
			config: types.AuthConfig{
				Method:   types.AuthMethodBasic,
				Username: "user",
				Password: "pass",
			},
			wantNil:  false,
			wantType: &http.BasicAuth{},
		},
		{
			name: "basic auth missing password",
			config: types.AuthConfig{
				Method:   types.AuthMethodBasic,
				Username: "user",
				Password: "",
			},
			wantNil: true,
		},
		{
			name: "basic auth missing username",
			config: types.AuthConfig{
				Method:   types.AuthMethodBasic,
				Username: "",
				Password: "pass",
			},
			wantNil: true,
		},
		{
			name: "ssh auth",
			config: types.AuthConfig{
				Method: types.AuthMethodSSH,
				SSHKey: []byte("fake-key"),
			},
			wantErr: nil, // Will fail with invalid key, but we check this in the test logic
		},
		{
			name: "ssh auth empty key",
			config: types.AuthConfig{
				Method: types.AuthMethodSSH,
				SSHKey: []byte{},
			},
			wantErr: ErrSSHKeyRequired,
		},
		{
			name: "none auth",
			config: types.AuthConfig{
				Method: types.AuthMethodNone,
			},
			wantNil: true,
		},
		{
			name: "unknown auth method",
			config: types.AuthConfig{
				Method: "unknown",
			},
			wantErr: ErrUnsupportedAuthMethod,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := CreateAuthMethod(tt.config)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr.Error())

				return
			}

			// Special handling for SSH auth with invalid key
			if tt.name == "ssh auth" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "failed to create SSH public keys")

				return
			}

			require.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, auth)
			} else {
				assert.NotNil(t, auth)

				if tt.wantType != nil {
					assert.IsType(t, tt.wantType, auth)
				}
			}
		})
	}
}

func TestCreateTokenAuth(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantNil bool
	}{
		{
			name:    "valid token",
			token:   "test-token",
			wantNil: false,
		},
		{
			name:    "empty token",
			token:   "",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := createTokenAuth(tt.token)

			if tt.wantNil {
				assert.Nil(t, auth)
			} else {
				assert.NotNil(t, auth)
				basicAuth, ok := auth.(*http.BasicAuth)
				require.True(t, ok)
				assert.Equal(t, "token", basicAuth.Username)
				assert.Equal(t, tt.token, basicAuth.Password)
			}
		})
	}
}

func TestCreateBasicAuth(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		wantNil  bool
	}{
		{
			name:     "valid credentials",
			username: "user",
			password: "pass",
			wantNil:  false,
		},
		{
			name:     "empty username",
			username: "",
			password: "pass",
			wantNil:  true,
		},
		{
			name:     "empty password",
			username: "user",
			password: "",
			wantNil:  true,
		},
		{
			name:     "both empty",
			username: "",
			password: "",
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := createBasicAuth(tt.username, tt.password)

			if tt.wantNil {
				assert.Nil(t, auth)
			} else {
				assert.NotNil(t, auth)
				basicAuth, ok := auth.(*http.BasicAuth)
				require.True(t, ok)
				assert.Equal(t, tt.username, basicAuth.Username)
				assert.Equal(t, tt.password, basicAuth.Password)
			}
		})
	}
}

func TestCreateSSHAuth(t *testing.T) {
	tests := []struct {
		name    string
		sshKey  []byte
		wantErr error
	}{
		{
			name: "valid ssh key",
			sshKey: []byte(
				"-----BEGIN OPENSSH PRIVATE KEY-----\nMOCK\n-----END OPENSSH PRIVATE KEY-----",
			),
			// Note: This will fail with invalid key format, but tests the error path
		},
		{
			name:    "empty ssh key",
			sshKey:  []byte{},
			wantErr: ErrSSHKeyRequired,
		},
		{
			name:    "nil ssh key",
			sshKey:  nil,
			wantErr: ErrSSHKeyRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := createSSHAuth(tt.sshKey)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.Equal(t, tt.wantErr, err)
				assert.Nil(t, auth)
			} else {
				// For valid keys, we expect either success or a different error
				// The mock key above will fail, but that's expected
				if err != nil {
					assert.Contains(t, err.Error(), "failed to create SSH public keys")
				} else {
					assert.NotNil(t, auth)
					assert.IsType(t, &ssh.PublicKeys{}, auth)
				}
			}
		})
	}
}

func TestLoadSSHKeyFromFile(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		setup    func() string // returns temp file path
		wantErr  error
	}{
		{
			name:     "empty file path",
			filePath: "",
			wantErr:  ErrSSHKeyPathEmpty,
		},
		{
			name:     "nonexistent file",
			filePath: "/nonexistent/file",
			wantErr:  nil, // Will be a file read error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filePath string
			if tt.setup != nil {
				filePath = tt.setup()
			} else {
				filePath = tt.filePath
			}

			key, err := LoadSSHKeyFromFile(filePath)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.Equal(t, tt.wantErr, err)
				assert.Nil(t, key)
			} else if tt.name == "nonexistent file" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "failed to read SSH key file")
				assert.Nil(t, key)
			}
		})
	}
}

func TestParseAuthConfigFromFlags(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		username   string
		password   string
		sshKeyPath string
		expected   types.AuthConfig
		wantErr    bool
	}{
		{
			name:       "token takes priority",
			token:      "token123",
			username:   "user",
			password:   "pass",
			sshKeyPath: "/path/to/key",
			expected: types.AuthConfig{
				Method: types.AuthMethodToken,
				Token:  "token123",
			},
		},
		{
			name:     "basic auth when no token",
			token:    "",
			username: "user",
			password: "pass",
			expected: types.AuthConfig{
				Method:   types.AuthMethodBasic,
				Username: "user",
				Password: "pass",
			},
		},
		{
			name:       "ssh auth when no token or basic",
			token:      "",
			username:   "",
			password:   "",
			sshKeyPath: "/path/to/key",
			expected: types.AuthConfig{
				Method: types.AuthMethodSSH,
				// SSHKey would be loaded, but we can't mock file reading easily
			},
			wantErr: true, // Because file doesn't exist
		},
		{
			name:       "none auth when no credentials",
			token:      "",
			username:   "",
			password:   "",
			sshKeyPath: "",
			expected: types.AuthConfig{
				Method: types.AuthMethodNone,
			},
		},
		{
			name:     "basic auth incomplete",
			token:    "",
			username: "user",
			password: "",
			expected: types.AuthConfig{
				Method: types.AuthMethodNone,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ParseAuthConfigFromFlags(
				tt.token,
				tt.username,
				tt.password,
				tt.sshKeyPath,
			)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected.Method, config.Method)

				if tt.expected.Token != "" {
					assert.Equal(t, tt.expected.Token, config.Token)
				}

				if tt.expected.Username != "" {
					assert.Equal(t, tt.expected.Username, config.Username)
				}

				if tt.expected.Password != "" {
					assert.Equal(t, tt.expected.Password, config.Password)
				}
			}
		})
	}
}

func TestValidateAuthConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  types.AuthConfig
		wantErr error
	}{
		{
			name: "valid token config",
			config: types.AuthConfig{
				Method: types.AuthMethodToken,
				Token:  "token123",
			},
		},
		{
			name: "invalid token config - empty token",
			config: types.AuthConfig{
				Method: types.AuthMethodToken,
				Token:  "",
			},
			wantErr: ErrTokenRequired,
		},
		{
			name: "valid basic config",
			config: types.AuthConfig{
				Method:   types.AuthMethodBasic,
				Username: "user",
				Password: "pass",
			},
		},
		{
			name: "invalid basic config - missing username",
			config: types.AuthConfig{
				Method:   types.AuthMethodBasic,
				Username: "",
				Password: "pass",
			},
			wantErr: ErrBasicAuthIncomplete,
		},
		{
			name: "invalid basic config - missing password",
			config: types.AuthConfig{
				Method:   types.AuthMethodBasic,
				Username: "user",
				Password: "",
			},
			wantErr: ErrBasicAuthIncomplete,
		},
		{
			name: "valid ssh config",
			config: types.AuthConfig{
				Method: types.AuthMethodSSH,
				SSHKey: []byte("key"),
			},
		},
		{
			name: "invalid ssh config - empty key",
			config: types.AuthConfig{
				Method: types.AuthMethodSSH,
				SSHKey: []byte{},
			},
			wantErr: ErrSSHKeyRequired,
		},
		{
			name: "valid none config",
			config: types.AuthConfig{
				Method: types.AuthMethodNone,
			},
		},
		{
			name: "unknown method",
			config: types.AuthConfig{
				Method: "unknown",
			},
			wantErr: nil, // Error will be checked by containing the base error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAuthConfig(tt.config)

			switch {
			case tt.wantErr != nil:
				require.Error(t, err)
				assert.Equal(t, tt.wantErr, err)
			case tt.name == "unknown method":
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unknown authentication method")
			default:
				require.NoError(t, err)
			}
		})
	}
}

func TestIsPrivateRepo(t *testing.T) {
	tests := []struct {
		name     string
		repoURL  string
		expected bool
	}{
		{
			name:     "github ssh",
			repoURL:  "git@github.com:user/repo.git",
			expected: true,
		},
		{
			name:     "gitlab ssh",
			repoURL:  "git@gitlab.com:user/repo.git",
			expected: true,
		},
		{
			name:     "bitbucket ssh",
			repoURL:  "git@bitbucket.org:user/repo.git",
			expected: true,
		},
		{
			name:     "https with username",
			repoURL:  "https://user@github.com/user/repo.git",
			expected: true,
		},
		{
			name:     "https without username",
			repoURL:  "https://github.com/user/repo.git",
			expected: false,
		},
		{
			name:     "public http",
			repoURL:  "http://github.com/user/repo.git",
			expected: false,
		},
		{
			name:     "empty url",
			repoURL:  "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPrivateRepo(tt.repoURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDefaultAuthConfig(t *testing.T) {
	config := GetDefaultAuthConfig()
	assert.Equal(t, types.AuthMethodNone, config.Method)
	assert.Empty(t, config.Token)
	assert.Empty(t, config.Username)
	assert.Empty(t, config.Password)
	assert.Empty(t, config.SSHKey)
}

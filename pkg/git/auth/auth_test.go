// Package auth provides Git authentication handling for Watchtower's Git monitoring feature.
package auth

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestCreateAuthMethod_Token(t *testing.T) {
	config := types.AuthConfig{
		Method: types.AuthMethodToken,
		Token:  "test-token",
	}

	auth, err := CreateAuthMethod(config)

	require.NoError(t, err)
	assert.NotNil(t, auth)
}

func TestCreateAuthMethod_Basic(t *testing.T) {
	config := types.AuthConfig{
		Method:   types.AuthMethodBasic,
		Username: "testuser",
		Password: "testpass",
	}

	auth, err := CreateAuthMethod(config)

	require.NoError(t, err)
	assert.NotNil(t, auth)
}

func TestCreateAuthMethod_SSH(t *testing.T) {
	// Skip SSH test as it requires valid SSH key format
	// This would be tested manually or with integration tests
	t.Skip("SSH authentication requires valid key format - test manually")
}

func TestCreateAuthMethod_None(t *testing.T) {
	config := types.AuthConfig{
		Method: types.AuthMethodNone,
	}

	auth, err := CreateAuthMethod(config)

	require.NoError(t, err)
	assert.Nil(t, auth)
}

func TestCreateAuthMethod_Invalid(t *testing.T) {
	config := types.AuthConfig{
		Method: "invalid-method",
	}

	auth, err := CreateAuthMethod(config)

	require.Error(t, err)
	assert.Nil(t, auth)
	assert.Contains(t, err.Error(), "unsupported authentication method")
}

func TestParseAuthConfigFromFlags_Token(t *testing.T) {
	config, err := ParseAuthConfigFromFlags("test-token", "", "", "")

	require.NoError(t, err)
	assert.Equal(t, types.AuthMethodToken, config.Method)
	assert.Equal(t, "test-token", config.Token)
}

func TestParseAuthConfigFromFlags_Basic(t *testing.T) {
	config, err := ParseAuthConfigFromFlags("", "testuser", "testpass", "")

	require.NoError(t, err)
	assert.Equal(t, types.AuthMethodBasic, config.Method)
	assert.Equal(t, "testuser", config.Username)
	assert.Equal(t, "testpass", config.Password)
}

func TestParseAuthConfigFromFlags_SSH(t *testing.T) {
	// Create a temporary SSH key file for testing
	tempFile, err := os.CreateTemp(t.TempDir(), "ssh_key_test")
	require.NoError(t, err)

	defer os.Remove(tempFile.Name())

	_, err = tempFile.WriteString(
		"-----BEGIN OPENSSH PRIVATE KEY-----\nfake-key-content\n-----END OPENSSH PRIVATE KEY-----",
	)
	require.NoError(t, err)
	tempFile.Close()

	config, err := ParseAuthConfigFromFlags("", "", "", tempFile.Name())

	require.NoError(t, err)
	assert.Equal(t, types.AuthMethodSSH, config.Method)
	assert.NotEmpty(t, config.SSHKey)
}

func TestParseAuthConfigFromFlags_None(t *testing.T) {
	config, err := ParseAuthConfigFromFlags("", "", "", "")

	require.NoError(t, err)
	assert.Equal(t, types.AuthMethodNone, config.Method)
}

func TestValidateAuthConfig_Token(t *testing.T) {
	config := types.AuthConfig{
		Method: types.AuthMethodToken,
		Token:  "test-token",
	}

	err := ValidateAuthConfig(config)

	require.NoError(t, err)
}

func TestValidateAuthConfig_TokenEmpty(t *testing.T) {
	config := types.AuthConfig{
		Method: types.AuthMethodToken,
		Token:  "",
	}

	err := ValidateAuthConfig(config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "token authentication requires a token")
}

func TestValidateAuthConfig_Basic(t *testing.T) {
	config := types.AuthConfig{
		Method:   types.AuthMethodBasic,
		Username: "testuser",
		Password: "testpass",
	}

	err := ValidateAuthConfig(config)

	require.NoError(t, err)
}

func TestValidateAuthConfig_BasicIncomplete(t *testing.T) {
	config := types.AuthConfig{
		Method:   types.AuthMethodBasic,
		Username: "testuser",
		Password: "",
	}

	err := ValidateAuthConfig(config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "basic authentication requires both username and password")
}

func TestValidateAuthConfig_SSH(t *testing.T) {
	config := types.AuthConfig{
		Method: types.AuthMethodSSH,
		SSHKey: []byte("fake-key-content"),
	}

	err := ValidateAuthConfig(config)

	require.NoError(t, err)
}

func TestValidateAuthConfig_SSHEmpty(t *testing.T) {
	config := types.AuthConfig{
		Method: types.AuthMethodSSH,
		SSHKey: []byte{},
	}

	err := ValidateAuthConfig(config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "SSH authentication requires a private key")
}

func TestValidateAuthConfig_None(t *testing.T) {
	config := types.AuthConfig{
		Method: types.AuthMethodNone,
	}

	err := ValidateAuthConfig(config)

	require.NoError(t, err)
}

func TestValidateAuthConfig_InvalidMethod(t *testing.T) {
	config := types.AuthConfig{
		Method: "invalid-method",
	}

	err := ValidateAuthConfig(config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown authentication method")
}

func TestIsPrivateRepo(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://github.com/user/repo.git", false},
		{"git@github.com:user/repo.git", true},
		{"https://user:pass@github.com/user/repo.git", true},
		{"git@gitlab.com:user/repo.git", true},
		{"https://example.com/user/repo.git", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := IsPrivateRepo(tt.url)
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

package registry

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/distribution/reference"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerCliConfig "github.com/docker/cli/cli/config"
)

// TestEncodedEnvAuth_ReturnsCredentialsWhenSet verifies that EncodedEnvAuth
// returns base64-encoded credentials when REPO_USER and REPO_PASS are set.
func TestEncodedEnvAuth_ReturnsCredentialsWhenSet(t *testing.T) {
	expected := "eyJ1c2VybmFtZSI6IndhdGNodG93ZXItdXNlciIsInBhc3N3b3JkIjoid2F0Y2h0b3dlci1wYXNzIn0="

	t.Setenv("REPO_USER", "watchtower-user")
	t.Setenv("REPO_PASS", "watchtower-pass")

	config, err := EncodedEnvAuth()
	require.NoError(t, err)
	assert.Equal(t, expected, config)
}

// TestEncodedEnvAuth_ReturnsErrorWhenUnset verifies that EncodedEnvAuth
// returns an error when REPO_USER and REPO_PASS are not set.
func TestEncodedEnvAuth_ReturnsErrorWhenUnset(t *testing.T) {
	t.Setenv("REPO_USER", "")
	t.Setenv("REPO_PASS", "")

	_, err := EncodedEnvAuth()
	require.Error(t, err)
}

// TestEncodedEnvAuth_PartialCredentials verifies that EncodedEnvAuth returns
// an error when only one of REPO_USER or REPO_PASS is set.
func TestEncodedEnvAuth_PartialCredentials(t *testing.T) {
	tests := []struct {
		name     string
		repoUser string
		repoPass string
	}{
		{
			name:     "username set but password missing",
			repoUser: "partial-user",
			repoPass: "",
		},
		{
			name:     "password set but username missing",
			repoUser: "",
			repoPass: "partial-pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("REPO_USER", tt.repoUser)
			t.Setenv("REPO_PASS", tt.repoPass)

			credentials, err := EncodedEnvAuth()
			require.Error(t, err)
			assert.Empty(t, credentials)
		})
	}
}

// TestEncodedConfigAuth_ReturnsErrorWhenFileNotPresent verifies that
// EncodedConfigCredentials returns an error when the Docker config directory
// does not contain a valid config file.
func TestEncodedConfigAuth_ReturnsErrorWhenFileNotPresent(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", "/dev/null/should-fail")

	_, err := EncodedConfigCredentials("docker.io/library/nginx:latest")
	require.Error(t, err)
}

// TestEncodedConfigCredentials_FileStoreNoUsername tests that EncodedConfigCredentials
// returns empty string and nil error when the Docker config file's auth entry has
// no username (covers the empty-username guard added for native store compatibility).
func TestEncodedConfigCredentials_FileStoreNoUsername(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "watchtower-test-docker-config")
	require.NoError(t, err)

	defer os.RemoveAll(tempDir)

	configContent, err := json.Marshal(map[string]any{
		"auths": map[string]any{
			"ghcr.io": map[string]string{
				"serveraddress": "ghcr.io",
			},
		},
	})
	require.NoError(t, err)

	configPath := filepath.Join(tempDir, "config.json")
	writeErr := os.WriteFile(configPath, configContent, 0o600)
	require.NoError(t, writeErr)

	t.Setenv("DOCKER_CONFIG", tempDir)
	t.Setenv("HOME", tempDir)
	dockerCliConfig.SetDir(tempDir)

	normalizedRef, parseErr := reference.ParseNormalizedNamed("ghcr.io/test/image:latest")
	require.NoError(t, parseErr)

	credentials, err := EncodedConfigCredentials(normalizedRef.String())
	require.NoError(t, err)
	assert.Empty(t, credentials)
}

// TestEncodedConfigCredentials_FileStoreUsernameOnly tests that EncodedConfigCredentials
// returns empty string and nil error when the Docker config file's auth entry has
// a username but no password.
func TestEncodedConfigCredentials_FileStoreUsernameOnly(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "watchtower-test-docker-config")
	require.NoError(t, err)

	defer os.RemoveAll(tempDir)

	configContent, err := json.Marshal(map[string]any{
		"auths": map[string]any{
			"ghcr.io": map[string]string{
				"serveraddress": "ghcr.io",
				"username":      "testuser",
			},
		},
	})
	require.NoError(t, err)

	configPath := filepath.Join(tempDir, "config.json")
	writeErr := os.WriteFile(configPath, configContent, 0o600)
	require.NoError(t, writeErr)

	t.Setenv("DOCKER_CONFIG", tempDir)
	t.Setenv("HOME", tempDir)
	dockerCliConfig.SetDir(tempDir)

	normalizedRef, parseErr := reference.ParseNormalizedNamed("ghcr.io/test/image:latest")
	require.NoError(t, parseErr)

	credentials, err := EncodedConfigCredentials(normalizedRef.String())
	require.NoError(t, err)
	assert.Empty(t, credentials)
}

// TestEncodedConfigCredentials_FileStoreValidCredentials tests the happy path
// where the Docker config file contains valid username and password.
func TestEncodedConfigCredentials_FileStoreValidCredentials(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "watchtower-test-docker-config")
	require.NoError(t, err)

	defer os.RemoveAll(tempDir)

	configContent, err := json.Marshal(map[string]any{
		"auths": map[string]any{
			"ghcr.io": map[string]string{
				"username": "testuser",
				"password": "testpass",
			},
		},
	})
	require.NoError(t, err)

	configPath := filepath.Join(tempDir, "config.json")
	writeErr := os.WriteFile(configPath, configContent, 0o600)
	require.NoError(t, writeErr)

	t.Setenv("DOCKER_CONFIG", tempDir)
	t.Setenv("HOME", tempDir)
	dockerCliConfig.SetDir(tempDir)

	normalizedRef, parseErr := reference.ParseNormalizedNamed("ghcr.io/test/image:latest")
	require.NoError(t, parseErr)

	credentials, err := EncodedConfigCredentials(normalizedRef.String())
	require.NoError(t, err)
	assert.NotEmpty(t, credentials)

	authConfig := decodeEncodedAuth(t, credentials)
	assert.Equal(t, "testuser", authConfig["username"])
	assert.Equal(t, "testpass", authConfig["password"])
}

// TestEncodedConfigCredentials_NoConfigFile tests that EncodedConfigCredentials
// returns empty credentials and nil error when the Docker config directory
// does not exist, since the file store gracefully handles missing config files.
func TestEncodedConfigCredentials_NoConfigFile(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", "/nonexistent/watchtower-test-path")
	dockerCliConfig.SetDir("/nonexistent/watchtower-test-path")

	credentials, err := EncodedConfigCredentials("ghcr.io/test/image:latest")
	require.NoError(t, err)
	assert.Empty(t, credentials)
}

// TestEncodedAuth_UsesConfigWhenEnvUnset verifies that EncodedAuth falls
// through to EncodedConfigCredentials when REPO_USER/REPO_PASS are unset
// and a config file is present.
func TestEncodedAuth_UsesConfigWhenEnvUnset(t *testing.T) {
	writeTestDockerConfig(t, map[string]map[string]string{
		"ghcr.io": {
			"username": "cfguser",
			"password": "cfgpass",
		},
	})

	defer os.Unsetenv("DOCKER_CONFIG")

	t.Setenv("REPO_USER", "")
	t.Setenv("REPO_PASS", "")

	credentials, err := EncodedAuth("ghcr.io/test/image:latest")
	require.NoError(t, err)
	assert.NotEmpty(t, credentials)

	authConfig := decodeEncodedAuth(t, credentials)
	assert.Equal(t, "cfguser", authConfig["username"])
	assert.Equal(t, "cfgpass", authConfig["password"])
}

// TestEncodedConfigCredentials_MultipleRegistries verifies that a Docker config
// file with credentials for multiple registries returns the correct distinct
// credentials for each registry.
func TestEncodedConfigCredentials_MultipleRegistries(t *testing.T) {
	writeTestDockerConfig(t, map[string]map[string]string{
		"ghcr.io": {
			"username": "ghcr-user",
			"password": "ghcr-pass",
		},
		"registry.example.com": {
			"username": "example-user",
			"password": "example-pass",
		},
	})

	defer os.Unsetenv("DOCKER_CONFIG")

	tests := []struct {
		name      string
		imageRef  string
		wantUser  string
		wantPass  string
		wantEmpty bool
	}{
		{
			name:     "ghcr.io returns ghcr credentials",
			imageRef: "ghcr.io/test/image:latest",
			wantUser: "ghcr-user",
			wantPass: "ghcr-pass",
		},
		{
			name:     "registry.example.com returns example credentials",
			imageRef: "registry.example.com/org/image:latest",
			wantUser: "example-user",
			wantPass: "example-pass",
		},
		{
			name:      "unconfigured registry returns empty",
			imageRef:  "docker.io/library/alpine:latest",
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			credentials, err := EncodedConfigCredentials(tt.imageRef)
			require.NoError(t, err)

			if tt.wantEmpty {
				assert.Empty(t, credentials)

				return
			}

			assert.NotEmpty(t, credentials)

			authConfig := decodeEncodedAuth(t, credentials)
			assert.Equal(t, tt.wantUser, authConfig["username"])
			assert.Equal(t, tt.wantPass, authConfig["password"])
		})
	}
}

// TestEncodedAuth_EnvVarsOverrideConfig verifies that EncodedAuth returns env
// credentials when REPO_USER/REPO_PASS are set, even if a config file with
// different credentials is present.
func TestEncodedAuth_EnvVarsOverrideConfig(t *testing.T) {
	writeTestDockerConfig(t, map[string]map[string]string{
		"ghcr.io": {
			"username": "cfg-user",
			"password": "cfg-pass",
		},
	})

	defer os.Unsetenv("DOCKER_CONFIG")

	t.Setenv("REPO_USER", "env-user")
	t.Setenv("REPO_PASS", "env-pass")

	credentials, err := EncodedAuth("ghcr.io/test/image:latest")
	require.NoError(t, err)
	assert.NotEmpty(t, credentials)

	authConfig := decodeEncodedAuth(t, credentials)
	assert.Equal(t, "env-user", authConfig["username"])
	assert.Equal(t, "env-pass", authConfig["password"])
}

// TestEncodedConfigCredentials_MultipleImagesSameRegistry verifies that
// different images from the same registry receive identical credentials.
func TestEncodedConfigCredentials_MultipleImagesSameRegistry(t *testing.T) {
	writeTestDockerConfig(t, map[string]map[string]string{
		"ghcr.io": {
			"username": "shared-user",
			"password": "shared-pass",
		},
	})

	defer os.Unsetenv("DOCKER_CONFIG")

	imageRefs := []string{
		"ghcr.io/org/image-a:latest",
		"ghcr.io/org/image-b:v1.2.3",
		"ghcr.io/another/repo:tag",
	}

	var firstCredentials string

	for _, ref := range imageRefs {
		credentials, err := EncodedConfigCredentials(ref)
		require.NoError(t, err)
		assert.NotEmpty(t, credentials)

		if firstCredentials == "" {
			firstCredentials = credentials
		} else {
			assert.Equal(t, firstCredentials, credentials,
				"expected identical credentials for all images from same registry")
		}
	}
}

// TestEncodedConfigCredentials_RegistryMissingFromConfig verifies that
// EncodedConfigCredentials returns empty credentials when the config file
// contains entries for other registries but not the requested one.
func TestEncodedConfigCredentials_RegistryMissingFromConfig(t *testing.T) {
	writeTestDockerConfig(t, map[string]map[string]string{
		"docker.io": {
			"username": "docker-user",
			"password": "docker-pass",
		},
	})

	defer os.Unsetenv("DOCKER_CONFIG")

	credentials, err := EncodedConfigCredentials("ghcr.io/test/image:latest")
	require.NoError(t, err)
	assert.Empty(t, credentials)
}

// TestEncodedConfigCredentials_EmptyAuthsMap verifies that a config file with
// an empty `auths` map returns empty credentials without error.
func TestEncodedConfigCredentials_EmptyAuthsMap(t *testing.T) {
	writeTestDockerConfig(t, map[string]map[string]string{})

	defer os.Unsetenv("DOCKER_CONFIG")

	credentials, err := EncodedConfigCredentials("ghcr.io/test/image:latest")
	require.NoError(t, err)
	assert.Empty(t, credentials)
}

// TestEncodedConfigCredentials_MalformedConfigJSON verifies that EncodedConfigCredentials
// returns an error when the config file contains malformed JSON.
func TestEncodedConfigCredentials_MalformedConfigJSON(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "watchtower-test-docker-config")
	require.NoError(t, err)

	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "config.json")
	writeErr := os.WriteFile(configPath, []byte("not valid json {{{"), 0o600)
	require.NoError(t, writeErr)

	t.Setenv("DOCKER_CONFIG", tempDir)
	t.Setenv("HOME", tempDir)
	dockerCliConfig.SetDir(tempDir)

	credentials, err := EncodedConfigCredentials("ghcr.io/test/image:latest")
	require.Error(t, err)
	assert.Empty(t, credentials)
}

// TestEncodedAuth_FallsThroughToConfigWhenEnvPartiallySet verifies that
// EncodedAuth falls through to EncodedConfigCredentials when env vars are
// partially set (one present, one missing) and a valid config file exists.
func TestEncodedAuth_FallsThroughToConfigWhenEnvPartiallySet(t *testing.T) {
	writeTestDockerConfig(t, map[string]map[string]string{
		"ghcr.io": {
			"username": "cfg-user",
			"password": "cfg-pass",
		},
	})

	defer os.Unsetenv("DOCKER_CONFIG")

	t.Setenv("REPO_USER", "env-only-user")
	t.Setenv("REPO_PASS", "")

	credentials, err := EncodedAuth("ghcr.io/test/image:latest")
	require.NoError(t, err)
	assert.NotEmpty(t, credentials)

	authConfig := decodeEncodedAuth(t, credentials)
	assert.Equal(t, "cfg-user", authConfig["username"])
	assert.Equal(t, "cfg-pass", authConfig["password"])
}

// TestEncodedAuth_EnvPartiallySetWithUnconfiguredRegistry verifies that
// EncodedAuth returns empty credentials without error when env vars are
// partially set and the config file has no entry for the requested registry.
func TestEncodedAuth_EnvPartiallySetWithUnconfiguredRegistry(t *testing.T) {
	writeTestDockerConfig(t, map[string]map[string]string{
		"docker.io": {
			"username": "docker-user",
			"password": "docker-pass",
		},
	})

	defer os.Unsetenv("DOCKER_CONFIG")

	t.Setenv("REPO_USER", "env-only-user")
	t.Setenv("REPO_PASS", "")

	credentials, err := EncodedAuth("ghcr.io/test/image:latest")
	require.NoError(t, err)
	assert.Empty(t, credentials)
}

// TestEncodedConfigCredentials_InvalidImageRef verifies that EncodedConfigCredentials
// returns an error when the image reference is invalid and cannot be parsed.
func TestEncodedConfigCredentials_InvalidImageRef(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "watchtower-test-docker-config")
	require.NoError(t, err)

	defer os.RemoveAll(tempDir)

	t.Setenv("DOCKER_CONFIG", tempDir)
	t.Setenv("HOME", tempDir)
	dockerCliConfig.SetDir(tempDir)

	credentials, err := EncodedConfigCredentials("")
	require.Error(t, err)
	assert.Empty(t, credentials)
}

// writeTestDockerConfig writes a Docker config.json with the given auths map
// and returns the temp directory path. The caller is responsible for cleanup.
func writeTestDockerConfig(t *testing.T, auths map[string]map[string]string) string {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "watchtower-test-docker-config")
	require.NoError(t, err)

	configContent, err := json.Marshal(map[string]any{
		"auths": auths,
	})
	require.NoError(t, err)

	configPath := filepath.Join(tempDir, "config.json")
	writeErr := os.WriteFile(configPath, configContent, 0o600)
	require.NoError(t, writeErr)

	t.Setenv("DOCKER_CONFIG", tempDir)
	t.Setenv("HOME", tempDir)
	dockerCliConfig.SetDir(tempDir)

	return tempDir
}

// decodeEncodedAuth decodes a base64-encoded auth string and unmarshals it
// into a username/password map. It is a test helper that fatals on failure.
func decodeEncodedAuth(t *testing.T, encoded string) map[string]string {
	t.Helper()

	decoded, decodeErr := base64.URLEncoding.DecodeString(encoded)
	require.NoError(t, decodeErr)

	var authConfig map[string]string

	unmarshalErr := json.Unmarshal(decoded, &authConfig)
	require.NoError(t, unmarshalErr)

	return authConfig
}

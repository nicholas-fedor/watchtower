package registry

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	dockerCliConfig "github.com/docker/cli/cli/config"
	dockerConfigConfigfile "github.com/docker/cli/cli/config/configfile"
	dockerConfigCredentials "github.com/docker/cli/cli/config/credentials"
	dockerConfig "github.com/docker/cli/cli/config/types"

	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"
)

// Errors for registry authentication operations.
var (
	// errUnsetRegAuthVars indicates registry auth environment variables (REPO_USER, REPO_PASS) are not set.
	errUnsetRegAuthVars = errors.New(
		"registry auth environment variables (REPO_USER, REPO_PASS) not set",
	)
	// errFailedGetRegistryAddress indicates a failure to extract the registry address from an image reference.
	errFailedGetRegistryAddress = errors.New("failed to get registry address")
	// errFailedLoadDockerConfig indicates a failure to load the Docker configuration file.
	errFailedLoadDockerConfig = errors.New("failed to load Docker config")
	// errFailedMarshalAuthConfig indicates a failure to marshal the auth config to JSON.
	errFailedMarshalAuthConfig = errors.New("failed to marshal auth config to JSON")
)

// EncodedAuth attempts to retrieve encoded authentication credentials for a given image name.
//
// Per-image Docker config credentials are preferred when present so REPO_USER/REPO_PASS
// are not sent to every registry. Environment credentials are used when the config has
// no entry for the image's registry (or config lookup fails), preserving the common
// single-registry REPO_USER/REPO_PASS deployment.
//
// Parameters:
//   - imageName: Image reference string (e.g., "docker.io/library/alpine").
//
// Returns:
//   - string: Base64-encoded credentials string if successful, empty if none found.
//   - error: Non-nil if both methods fail, nil on success or if no credentials are available.
func EncodedAuth(log *zerolog.Logger, imageName string) (string, error) {
	// Set up logging fields for tracking.
	fields := map[string]any{
		"image_ref": imageName,
	}

	log.Debug().
		Fields(fields).
		Msg("Attempting to retrieve auth credentials")

	configCredentials, configErr := EncodedConfigCredentials(log, imageName)
	if configErr == nil && configCredentials != "" {
		log.Debug().
			Fields(fields).
			Msg("Successfully retrieved encoded auth credentials from config")

		return configCredentials, nil
	}

	if configErr != nil {
		log.Debug().
			Err(configErr).
			Fields(fields).
			Msg("Config auth not available, trying environment")
	} else {
		log.Debug().
			Fields(fields).
			Msg("No config credentials for registry, trying environment")
	}

	credentials, err := EncodedEnvAuth(log)
	if err != nil {
		// Prefer surfacing a config load/address error when env is also unset.
		if configErr != nil {
			return "", configErr
		}

		// No config entry and no env: empty credentials is success (anonymous pull).
		return "", nil
	}

	if credentials != "" {
		log.Debug().
			Fields(fields).
			Msg("Successfully retrieved encoded auth credentials from environment")
	}

	return credentials, nil
}

// EncodedEnvAuth checks for REPO_USER and REPO_PASS environment variables and encodes them.
//
// It returns an error if these variables are not set.
//
// Returns:
//   - string: Base64-encoded auth string if credentials are found.
//   - error: Non-nil if env vars are missing, nil on success.
func EncodedEnvAuth(log *zerolog.Logger) (string, error) {
	// Retrieve username and password from environment.
	username := os.Getenv("REPO_USER")
	password := os.Getenv("REPO_PASS")

	// Check if both variables are set.
	if username != "" && password != "" {
		credentials := dockerConfig.AuthConfig{
			Username: username,
			Password: password,
		}

		log.Debug().
			Bool("has_username", true).
			Msg("Loaded auth credentials from environment")

		// Trace only non-sensitive presence indicators. Never log REPO_PASS.
		if log.GetLevel() == zerolog.TraceLevel {
			log.Trace().
				Bool("has_username", true).
				Bool("has_password", true).
				Msg("Using environment credentials")
		}

		// Encode and return the auth config.
		return EncodeCredentials(log, credentials)
	}

	// Return error if variables are missing.
	log.Debug().Msg("Environment auth variables not set")

	return "", errUnsetRegAuthVars
}

// EncodedConfigCredentials retrieves authentication credentials from the Docker config file.
//
// The Docker config must be mounted on the container.
//
// Parameters:
//   - imageRef: Image reference string for registry lookup.
//
// Returns:
//   - string: Base64-encoded credentials string if found, empty if none.
//   - error: Non-nil if config loading or address retrieval fails, nil on success or if no auth is found.
func EncodedConfigCredentials(log *zerolog.Logger, imageRef string) (string, error) {
	// Set up logging fields for tracking.
	fields := map[string]any{
		"image_ref": imageRef,
	}

	// Get the registry server address from the image reference.
	server, err := auth.GetRegistryAddress(log, imageRef)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to get registry address")

		return "", fmt.Errorf("%w: %w", errFailedGetRegistryAddress, err)
	}

	// Use DOCKER_CONFIG env var or default to root directory.
	configDir := os.Getenv("DOCKER_CONFIG")
	if configDir == "" {
		configDir = "/"

		log.Debug().
			Fields(fields).
			Msg("No DOCKER_CONFIG set, using default directory")
	}

	// Load the Docker config file from the specified directory.
	configFile, err := dockerCliConfig.Load(configDir)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Str("config_dir", configDir).
			Msg("Failed to load Docker config")

		return "", fmt.Errorf("%w: %w", errFailedLoadDockerConfig, err)
	}

	// Retrieve credentials from the config's store.
	credStore := CredentialsStore(*configFile)
	credentials, _ := credStore.Get(server)

	// Accept username+password, password-only tokens, or identity tokens from
	// credential helpers (ECR and similar). Empty AuthConfig is a miss.
	if !hasUsableRegistryCredentials(credentials) {
		log.Debug().
			Fields(fields).
			Str("server", server).
			Str("config_file", configFile.Filename).
			Msg("No credentials found in config")

		return "", nil
	}

	// Log successful credential retrieval with non-sensitive presence flags only.
	log.Debug().
		Fields(fields).
		Bool("has_username", credentials.Username != "").
		Bool("has_password", credentials.Password != "").
		Bool("has_identity_tok", credentials.IdentityToken != "").
		Str("server", server).
		Str("config_file", configFile.Filename).
		Msg("Loaded auth credentials from config")

	// Trace only non-sensitive presence indicators. Never log password or tokens.
	if log.GetLevel() == zerolog.TraceLevel {
		log.Trace().
			Fields(fields).
			Bool("has_username", credentials.Username != "").
			Bool("has_password", credentials.Password != "").
			Bool("has_identity_tok", credentials.IdentityToken != "").
			Str("server", server).
			Msg("Using config credentials")
	}

	// Encode and return the auth config (includes IdentityToken when set).
	return EncodeCredentials(log, credentials)
}

// hasUsableRegistryCredentials reports whether AuthConfig carries material the
// Docker daemon or registry HTTP clients can authenticate with.
//
// IdentityToken and Password are supported end to end via EncodeCredentials and
// TransformAuth (Basic auth). RegistryToken-only entries are not accepted until
// a Bearer-auth path is implemented for them.
func hasUsableRegistryCredentials(credentials dockerConfig.AuthConfig) bool {
	if credentials == (dockerConfig.AuthConfig{}) {
		return false
	}

	if credentials.IdentityToken != "" {
		return true
	}

	// Password-only entries are used for PAT-style tokens (for example GHCR).
	if credentials.Password != "" {
		return true
	}

	return false
}

// CredentialsStore returns a new credentials store based on the configuration file settings.
//
// It selects a native or file-based store depending on the config.
//
// Parameters:
//   - configFile: Docker configuration file.
//
// Returns:
//   - dockerConfigCredentials.Store: Configured credentials store.
func CredentialsStore(configFile dockerConfigConfigfile.ConfigFile) dockerConfigCredentials.Store {
	// Use native store if a credentials store is specified.
	if configFile.CredentialsStore != "" {
		return dockerConfigCredentials.NewNativeStore(&configFile, configFile.CredentialsStore)
	}

	// Default to file-based store otherwise.
	return dockerConfigCredentials.NewFileStore(&configFile)
}

// EncodeCredentials Base64 encodes an AuthConfig struct for HTTP transmission.
//
// It marshals the struct to JSON and applies URL-safe base64 encoding.
//
// Parameters:
//   - authConfig: Authentication configuration to encode.
//
// Returns:
//   - string: Base64-encoded auth string if successful.
//   - error: Non-nil if marshaling fails, nil on success.
func EncodeCredentials(log *zerolog.Logger, authConfig dockerConfig.AuthConfig) (string, error) {
	// Set up logging fields with non-sensitive username presence indicator.
	fields := map[string]any{
		"has_username": authConfig.Username != "",
	}

	// Marshal the auth config to JSON.
	//nolint:gosec // G117: This is the expected standard Docker auth format
	buf, err := json.Marshal(authConfig)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to marshal auth config to JSON")

		return "", fmt.Errorf("%w: %w", errFailedMarshalAuthConfig, err)
	}

	// Encode the JSON to base64 for safe transmission.
	encoded := base64.URLEncoding.EncodeToString(buf)

	log.Debug().
		Fields(fields).
		Msg("Encoded auth config")

	return encoded, nil
}

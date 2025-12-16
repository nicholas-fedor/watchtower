// Package registry provides utilities for interacting with container registries in Watchtower.
// It handles authentication, pull option configuration, and API consumption checks for registry operations.
package registry

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/sirupsen/logrus"

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
// It checks environment variables first, then falls back to the Docker config file if needed.
//
// Parameters:
//   - ref: Image reference string (e.g., "docker.io/library/alpine").
//
// Returns:
//   - string: Base64-encoded credentials string if successful, empty if none found.
//   - error: Non-nil if both methods fail, nil on success or if no credentials are available.
func EncodedAuth(imageName string) (string, error) {
	// Set up logging fields for tracking.
	fields := logrus.Fields{
		"image_ref": imageName,
	}

	logrus.WithFields(fields).Debug("Attempting to retrieve auth credentials")

	// Try environment variables first.
	credentials, err := EncodedEnvAuth()
	if err != nil {
		// Fallback to config file if env vars are unavailable.
		logrus.WithError(err).
			WithFields(fields).
			Debug("Environment auth not available, trying config file")

		credentials, err = EncodedConfigCredentials(imageName)
	}

	if err == nil {
		logrus.WithFields(fields).Debug("Successfully retrieved encoded auth credentials")
	}

	return credentials, err
}

// EncodedEnvAuth checks for REPO_USER and REPO_PASS environment variables and encodes them.
//
// It returns an error if these variables are not set.
//
// Returns:
//   - string: Base64-encoded auth string if credentials are found.
//   - error: Non-nil if env vars are missing, nil on success.
func EncodedEnvAuth() (string, error) {
	// Retrieve username and password from environment.
	username := os.Getenv("REPO_USER")
	password := os.Getenv("REPO_PASS")

	// Check if both variables are set.
	if username != "" && password != "" {
		credentials := dockerConfig.AuthConfig{
			Username: username,
			Password: password,
		}

		logrus.WithFields(logrus.Fields{
			"username": username,
		}).Debug("Loaded auth credentials from environment")

		// Log sensitive password only in trace mode.
		if logrus.GetLevel() == logrus.TraceLevel {
			logrus.WithFields(logrus.Fields{
				"username": username,
				"password": password,
			}).Trace("Using environment credentials")
		}

		// Encode and return the auth config.
		return EncodeCredentials(credentials)
	}

	// Return error if variables are missing.
	logrus.Debug("Environment auth variables not set")

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
func EncodedConfigCredentials(imageRef string) (string, error) {
	// Set up logging fields for tracking.
	fields := logrus.Fields{
		"image_ref": imageRef,
	}

	// Get the registry server address from the image reference.
	server, err := auth.GetRegistryAddress(imageRef)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to get registry address")

		return "", fmt.Errorf("%w: %w", errFailedGetRegistryAddress, err)
	}

	// Use DOCKER_CONFIG env var or default to root directory.
	configDir := os.Getenv("DOCKER_CONFIG")
	if configDir == "" {
		configDir = "/"

		logrus.WithFields(fields).Debug("No DOCKER_CONFIG set, using default directory")
	}

	// Load the Docker config file from the specified directory.
	configFile, err := dockerCliConfig.Load(configDir)
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			WithField("config_dir", configDir).
			Debug("Failed to load Docker config")

		return "", fmt.Errorf("%w: %w", errFailedLoadDockerConfig, err)
	}

	// Retrieve credentials from the config’s store.
	credStore := CredentialsStore(*configFile)
	credentials, _ := credStore.Get(server)

	// Return empty string if no credentials are found.
	if credentials == (dockerConfig.AuthConfig{}) {
		logrus.WithFields(fields).WithFields(logrus.Fields{
			"server":      server,
			"config_file": configFile.Filename,
		}).Debug("No credentials found in config")

		return "", nil
	}

	// Log successful credential retrieval, hiding password unless in trace mode.
	logrus.WithFields(fields).WithFields(logrus.Fields{
		"username":    credentials.Username,
		"server":      server,
		"config_file": configFile.Filename,
	}).Debug("Loaded auth credentials from config")

	// Log password only in trace mode
	if logrus.GetLevel() == logrus.TraceLevel {
		logrus.WithFields(fields).WithFields(logrus.Fields{
			"username": credentials.Username,
			"password": credentials.Password,
			"server":   server,
		}).Trace("Using config credentials")
	}

	// Encode and return the auth config.
	return EncodeCredentials(credentials)
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
func EncodeCredentials(authConfig dockerConfig.AuthConfig) (string, error) {
	// Set up logging fields with username for tracking.
	fields := logrus.Fields{
		"username": authConfig.Username,
	}

	// Marshal the auth config to JSON.
	buf, err := json.Marshal(authConfig)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to marshal auth config to JSON")

		return "", fmt.Errorf("%w: %w", errFailedMarshalAuthConfig, err)
	}

	// Encode the JSON to base64 for safe transmission.
	encoded := base64.URLEncoding.EncodeToString(buf)

	logrus.WithFields(fields).Debug("Encoded auth config")

	return encoded, nil
}

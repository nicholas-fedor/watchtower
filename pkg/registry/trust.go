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
	dockerConfigTypes "github.com/docker/cli/cli/config/types"

	"github.com/nicholas-fedor/watchtower/pkg/registry/helpers"
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

// EncodedAuth attempts to retrieve encoded authentication credentials for a given image reference,
// first checking environment variables and then falling back to the Docker config file if necessary.
// It returns the encoded auth string or an error if both methods fail.
func EncodedAuth(ref string) (string, error) {
	fields := logrus.Fields{
		"image_ref": ref,
	}

	logrus.WithFields(fields).Debug("Attempting to retrieve auth credentials")

	auth, err := EncodedEnvAuth()
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Environment auth not available, trying config file")

		auth, err = EncodedConfigAuth(ref)
	}

	if err == nil {
		logrus.WithFields(fields).Debug("Successfully retrieved auth credentials")
	}

	return auth, err
}

// EncodedEnvAuth checks for REPO_USER and REPO_PASS environment variables and encodes them into
// a base64 string if present. It returns an error if these variables are not set.
func EncodedEnvAuth() (string, error) {
	username := os.Getenv("REPO_USER")
	password := os.Getenv("REPO_PASS")

	if username != "" && password != "" {
		auth := dockerConfigTypes.AuthConfig{
			Username: username,
			Password: password,
		}

		logrus.WithFields(logrus.Fields{
			"username": username,
		}).Debug("Loaded auth credentials from environment")

		// Log password only in trace mode
		if logrus.GetLevel() == logrus.TraceLevel {
			logrus.WithFields(logrus.Fields{
				"username": username,
				"password": password,
			}).Trace("Using environment credentials")
		}

		return EncodeAuth(auth)
	}

	logrus.Debug("Environment auth variables not set")

	return "", errUnsetRegAuthVars
}

// EncodedConfigAuth retrieves authentication credentials from the Docker config file for the given
// image reference. The Docker config must be mounted on the container. It returns an encoded auth
// string or an error if the config cannot be loaded or credentials are not found.
func EncodedConfigAuth(imageRef string) (string, error) {
	fields := logrus.Fields{
		"image_ref": imageRef,
	}

	server, err := helpers.GetRegistryAddress(imageRef)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to get registry address")

		return "", fmt.Errorf("%w: %w", errFailedGetRegistryAddress, err)
	}

	configDir := os.Getenv("DOCKER_CONFIG")
	if configDir == "" {
		configDir = "/"

		logrus.WithFields(fields).Debug("No DOCKER_CONFIG set, using default directory")
	}

	configFile, err := dockerCliConfig.Load(configDir)
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			WithField("config_dir", configDir).
			Debug("Failed to load Docker config")

		return "", fmt.Errorf("%w: %w", errFailedLoadDockerConfig, err)
	}

	credStore := CredentialsStore(*configFile)
	auth, _ := credStore.Get(server)

	if auth == (dockerConfigTypes.AuthConfig{}) {
		logrus.WithFields(fields).WithFields(logrus.Fields{
			"server":      server,
			"config_file": configFile.Filename,
		}).Debug("No credentials found in config")

		return "", nil
	}

	logrus.WithFields(fields).WithFields(logrus.Fields{
		"username":    auth.Username,
		"server":      server,
		"config_file": configFile.Filename,
	}).Debug("Loaded auth credentials from config")

	// Log password only in trace mode
	if logrus.GetLevel() == logrus.TraceLevel {
		logrus.WithFields(fields).WithFields(logrus.Fields{
			"username": auth.Username,
			"password": auth.Password,
			"server":   server,
		}).Trace("Using config credentials")
	}

	return EncodeAuth(auth)
}

// CredentialsStore returns a new credentials store based on the settings provided in the configuration file.
// It determines whether to use a native or file-based store depending on the config.
func CredentialsStore(configFile dockerConfigConfigfile.ConfigFile) dockerConfigCredentials.Store {
	if configFile.CredentialsStore != "" {
		return dockerConfigCredentials.NewNativeStore(&configFile, configFile.CredentialsStore)
	}

	return dockerConfigCredentials.NewFileStore(&configFile)
}

// EncodeAuth Base64 encodes an AuthConfig struct for transmission over HTTP.
// It marshals the struct to JSON and applies URL-safe base64 encoding.
func EncodeAuth(authConfig dockerConfigTypes.AuthConfig) (string, error) {
	fields := logrus.Fields{
		"username": authConfig.Username,
	}

	buf, err := json.Marshal(authConfig)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to marshal auth config to JSON")

		return "", fmt.Errorf("%w: %w", errFailedMarshalAuthConfig, err)
	}

	encoded := base64.URLEncoding.EncodeToString(buf)

	logrus.WithFields(fields).Debug("Encoded auth config")

	return encoded, nil
}

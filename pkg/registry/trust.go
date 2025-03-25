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

// Static error for when registry authentication environment variables are unset.
// It provides a clear message for cases where credentials are expected but missing.
var errUnsetRegAuthVars = errors.New(
	"registry auth environment variables (REPO_USER, REPO_PASS) not set",
)

// EncodedAuth attempts to retrieve encoded authentication credentials for a given image reference,
// first checking environment variables and then falling back to the Docker config file if necessary.
// It returns the encoded auth string or an error if both methods fail.
func EncodedAuth(ref string) (string, error) {
	auth, err := EncodedEnvAuth()
	if err != nil {
		auth, err = EncodedConfigAuth(ref)
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

		logrus.Debugf(
			"Loaded auth credentials for registry user %s from environment",
			auth.Username,
		)
		// CREDENTIAL: Uncomment to log REPO_PASS environment variable
		// log.Tracef("Using auth password %s", auth.Password)

		return EncodeAuth(auth)
	}

	return "", errUnsetRegAuthVars
}

// EncodedConfigAuth retrieves authentication credentials from the Docker config file for the given
// image reference. The Docker config must be mounted on the container. It returns an encoded auth
// string or an error if the config cannot be loaded or credentials are not found.
func EncodedConfigAuth(imageRef string) (string, error) {
	server, err := helpers.GetRegistryAddress(imageRef)
	if err != nil {
		logrus.Errorf("Could not get registry from image ref %s", imageRef)

		return "", fmt.Errorf(
			"failed to get registry address from image reference %s: %w",
			imageRef,
			err,
		)
	}

	configDir := os.Getenv("DOCKER_CONFIG")
	if configDir == "" {
		configDir = "/"
	}

	configFile, err := dockerCliConfig.Load(configDir)
	if err != nil {
		logrus.Errorf("Unable to find default config file: %s", err)

		return "", fmt.Errorf("failed to load Docker config from directory %s: %w", configDir, err)
	}

	credStore := CredentialsStore(*configFile)
	auth, _ := credStore.Get(server) // returns (types.AuthConfig{}) if server not in credStore

	if auth == (dockerConfigTypes.AuthConfig{}) {
		logrus.WithField("config_file", configFile.Filename).
			Debugf("No credentials for %s found", server)

		return "", nil
	}

	logrus.Debugf(
		"Loaded auth credentials for user %s, on registry %s, from file %s",
		auth.Username,
		server,
		configFile.Filename,
	)
	// CREDENTIAL: Uncomment to log docker config password
	// log.Tracef("Using auth password %s", auth.Password)
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
	buf, err := json.Marshal(authConfig)
	if err != nil {
		return "", fmt.Errorf("failed to marshal auth config to JSON: %w", err)
	}

	return base64.URLEncoding.EncodeToString(buf), nil
}

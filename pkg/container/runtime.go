package container

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/rs/zerolog"

	dockerClient "github.com/moby/moby/client"
)

// containerRuntime represents the type of container runtime detected.
type containerRuntime int

const (
	// runtimeUnknown indicates no container runtime has been detected.
	runtimeUnknown containerRuntime = iota
	// runtimeDocker indicates Docker is the container runtime.
	runtimeDocker
	// runtimePodman indicates Podman is the container runtime.
	runtimePodman
)

// detectRuntime determines the container runtime using multiple detection methods.
//
// It iterates through detection helpers in priority order, returning as soon as one
// helper indicates Podman/Docker or an error occurs.
//
// Parameters:
//   - log: Logger for detection diagnostics.
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker API client for API-based detection.
//   - fsys: Filesystem rooted at "/" that holds the runtime marker files.
//
// Returns:
//   - bool: True if Podman is detected, false otherwise.
//   - error: Non-nil if detection fails, nil on success.
func detectRuntime(
	log *zerolog.Logger,
	ctx context.Context,
	api dockerClient.APIClient,
	fsys fs.FS,
) (bool, error) {
	// Priority 1: Check for marker files (Podman or Docker)
	runtime, err := detectRuntimeByMarker(log, fsys)
	if err != nil {
		return false, err
	}

	// Handle the detected runtime from marker files
	switch runtime {
	case runtimePodman:
		return true, nil
	case runtimeDocker:
		return false, nil
	case runtimeUnknown:
		// Continue to next detection method
	default:
		// Continue to next detection method
	}

	// Priority 2: Check CONTAINER environment variable
	if detectRuntimeByEnv(log) {
		return true, nil
	}

	// Priority 3: API-based detection
	return detectRuntimeByAPI(log, ctx, api)
}

// detectRuntimeByMarker checks for container runtime marker files.
//
// It first checks for Podman's marker file, then checks for Docker's marker file.
// If either check returns a non-NotExist error (e.g., permission denied), an error
// is returned with details about which path failed.
//
// Parameters:
//   - log: Logger for detection diagnostics.
//   - fsys: Filesystem rooted at "/" that holds the runtime marker files.
//
// Returns:
//   - containerRuntime: The detected container runtime (runtimePodman, runtimeDocker, or runtimeUnknown).
//   - error: Non-nil if checking fails with a non-NotExist error, nil on success.
func detectRuntimeByMarker(log *zerolog.Logger, fsys fs.FS) (containerRuntime, error) {
	// Check for Podman marker file
	_, podmanErr := fs.Stat(fsys, "run/.containerenv")
	if podmanErr == nil {
		log.Debug().Msg("Detected Podman via marker file /run/.containerenv")

		return runtimePodman, nil
	}

	// Check for Docker marker file
	_, dockerErr := fs.Stat(fsys, ".dockerenv")
	if dockerErr == nil {
		log.Debug().Msg("Detected Docker via marker file /.dockerenv")

		return runtimeDocker, nil
	}

	// Both checks failed - determine if it's due to missing files or actual errors
	podmanNotExist := errors.Is(podmanErr, os.ErrNotExist)
	dockerNotExist := errors.Is(dockerErr, os.ErrNotExist)

	// If both files simply don't exist, that's expected (not an error)
	if podmanNotExist && dockerNotExist {
		return runtimeUnknown, nil
	}

	// At least one check returned a non-NotExist error (e.g., permission denied)
	// Build an informative error message
	switch {
	case !podmanNotExist && dockerNotExist:
		// Podman check failed with non-NotExist error
		return runtimeUnknown, fmt.Errorf(
			"failed to check Podman marker file /run/.containerenv: %w",
			podmanErr,
		)
	case podmanNotExist && !dockerNotExist:
		// Docker check failed with non-NotExist error
		return runtimeUnknown, fmt.Errorf(
			"failed to check Docker marker file /.dockerenv: %w",
			dockerErr,
		)
	default:
		// Both checks failed with non-NotExist errors
		return runtimeUnknown, fmt.Errorf(
			"failed to check marker files: /run/.containerenv: %w, /.dockerenv: %w",
			podmanErr,
			dockerErr,
		)
	}
}

// detectRuntimeByEnv reports whether the environment indicates Podman.
//
// It checks if the container environment variable, which Podman sets, or its
// uppercase form is "podman" or "oci", both of which indicate Podman is the
// container runtime.
//
// Parameters:
//   - log: Logger for detection diagnostics.
//
// Returns:
//   - bool: True if the environment variable indicates Podman, false otherwise.
func detectRuntimeByEnv(log *zerolog.Logger) bool {
	for _, key := range []string{"container", "CONTAINER"} {
		value := os.Getenv(key)
		if value == "podman" || value == "oci" {
			log.Debug().
				Str("variable", key).
				Msg("Detected Podman via container environment variable")

			return true
		}
	}

	return false
}

// detectRuntimeByAPI uses the Docker API to detect if the runtime is Podman.
//
// It queries the system info endpoint and checks the Name and ServerVersion
// fields for Podman indicators. Current Podman versions report the host name
// and a bare version there, so it then checks the version endpoint's
// components, where Podman names itself "Podman Engine".
//
// Parameters:
//   - log: Logger for detection diagnostics.
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker API client to query.
//
// Returns:
//   - bool: True if Podman is detected via API, false otherwise.
//   - error: Non-nil if API call fails, nil on success.
func detectRuntimeByAPI(log *zerolog.Logger, ctx context.Context, api dockerClient.APIClient) (bool, error) {
	result, err := api.Info(ctx, dockerClient.InfoOptions{})
	if err != nil {
		log.Debug().
			Err(err).
			Msg("Failed to get system info for Podman detection, assuming Docker")

		return false, fmt.Errorf("failed to get system info: %w", err)
	}

	info := result.Info

	if info.Name == "podman" {
		log.Debug().Msg("Detected Podman via API Name field")

		return true, nil
	}

	if strings.Contains(strings.ToLower(info.ServerVersion), "podman") {
		log.Debug().Msg("Detected Podman via API ServerVersion field")

		return true, nil
	}

	// The components are informational and not part of the API contract, so
	// they are checked only after the info fields.
	version, err := api.ServerVersion(ctx, dockerClient.ServerVersionOptions{})
	if err != nil {
		log.Debug().
			Err(err).
			Msg("Failed to get server version for Podman detection")

		return false, fmt.Errorf("failed to get server version: %w", err)
	}

	for _, component := range version.Components {
		if strings.Contains(strings.ToLower(component.Name), "podman") {
			log.Debug().
				Str("component", component.Name).
				Msg("Detected Podman via API version components")

			return true, nil
		}
	}

	log.Debug().Msg("No Podman detection criteria met, assuming Docker")

	return false, nil
}

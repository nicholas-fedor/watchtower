package container

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	dockerContainer "github.com/docker/docker/api/types/container"
	dockerNetwork "github.com/docker/docker/api/types/network"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Environment variable keys used by the ephemeral orchestrator.
const (
	// orchestratorOldIDEnv is the environment variable key for the old container ID.
	orchestratorOldIDEnv = "WT_ORCHESTRATOR_OLD_ID"
	// orchestratorNewImageEnv is the environment variable key for the new image reference.
	// Note: StartContainer resolves the image from the source container's config, not this var.
	// This env var is retained for debugging and future extensibility.
	orchestratorNewImageEnv = "WT_ORCHESTRATOR_NEW_IMAGE"
	// orchestratorOriginalNameEnv is the environment variable key for the original container name.
	orchestratorOriginalNameEnv = "WT_ORCHESTRATOR_ORIGINAL_NAME"
	// orchestratorContainerChainEnv is the environment variable key for the container chain label.
	orchestratorContainerChainEnv = "WT_ORCHESTRATOR_CONTAINER_CHAIN"
	// orchestratorCleanupTimeout is the timeout for cleanup operations on failed orchestrator creation.
	orchestratorCleanupTimeout = 5 * time.Second
)

// Docker connection environment variable keys.
const (
	// dockerHostEnv is the environment variable key for the Docker daemon host.
	dockerHostEnv = "DOCKER_HOST"
	// dockerTLSEnv is the environment variable key for TLS verification.
	dockerTLSEnv = "DOCKER_TLS_VERIFY"
	// dockerCertPathEnv is the environment variable key for TLS certificate path.
	dockerCertPathEnv = "DOCKER_CERT_PATH"
	// dockerAPIVersionEnv is the environment variable key for the Docker API version.
	dockerAPIVersionEnv = "DOCKER_API_VERSION"
)

// Default Docker socket paths for different platforms and runtimes.
const (
	// defaultUnixSocketPath is the standard Docker socket path on Unix systems.
	defaultUnixSocketPath = "/var/run/docker.sock"
	// defaultWindowsPipePath is the standard Docker named pipe path on Windows.
	defaultWindowsPipePath = "//./pipe/docker_engine"
)

// envPartsCount is the expected number of parts when splitting "KEY=VALUE" strings.
const envPartsCount = 2

// Remote Docker connection schemes that do not use a local socket.
// These require environment variable passthrough rather than socket mounting.
var remoteDockerSchemes = []string{
	"tcp://",
	"http://",
	"https://",
	"ssh://",
}

// DockerConnectionConfig holds the Docker connection configuration extracted from
// a source container's environment variables and bind mounts. This enables the
// ephemeral orchestrator to maintain the same Docker connection as the source
// container, supporting local sockets, Windows named pipes, remote TCP/TLS hosts,
// and socket proxies.
type DockerConnectionConfig struct {
	// Host is the DOCKER_HOST value (e.g., "unix:///var/run/docker.sock", "tcp://host:2375").
	Host string
	// TLSVerify indicates whether TLS verification is enabled ("1" or empty).
	TLSVerify string
	// CertPath is the path to TLS certificates for remote connections.
	CertPath string
	// APIVersion is the pinned Docker API version (empty for autonegotiation).
	APIVersion string
	// IsLocal indicates whether the connection is to a local socket or named pipe.
	IsLocal bool
	// SocketBind is the socket bind mount string for local connections (e.g., "/var/run/docker.sock:/var/run/docker.sock").
	// Empty for remote connections.
	SocketBind string
	// CertBinds are additional bind mounts for TLS certificate directories.
	// Only populated for remote TLS connections where certificates need to be mounted.
	CertBinds []string
}

// CreateEphemeralOrchestrator creates a short-lived container that orchestrates
// the Watchtower self-update transition.
//
// The ephemeral container uses the same Watchtower image (already pulled) with
// the --self-update-orchestrator flag. It is configured with AutoRemove for
// automatic cleanup and mounts the Docker socket for container management.
//
// The ephemeral container does not set the watchtower label
// (com.centurylinklabs.watchtower = "true") to avoid being detected as an
// excess Watchtower instance by the scope and filter systems.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - sourceContainer: Current Watchtower container being replaced.
//   - newImage: Image reference for the new Watchtower container.
//   - containerChain: Container chain label for lineage tracking.
//
// Returns:
//   - types.ContainerID: ID of the ephemeral orchestrator container.
//   - error: Non-nil if creation or start fails, nil on success.
func (c *client) CreateEphemeralOrchestrator(
	ctx context.Context,
	sourceContainer types.Container,
	newImage string,
	containerChain string,
) (types.ContainerID, error) {
	clog := logrus.WithFields(logrus.Fields{
		"source_container": sourceContainer.Name(),
		"source_id":        sourceContainer.ID().ShortID(),
		"new_image":        newImage,
	})

	clog.Debug("Creating ephemeral orchestrator for self-update")

	// Extract the Docker connection configuration from the source container.
	// This ensures the orchestrator uses the same connection method as the source,
	// supporting local sockets, named pipes, remote TCP/TLS, and socket proxies.
	connConfig := extractDockerConnectionConfig(sourceContainer)

	clog.WithFields(logrus.Fields{
		"docker_host": connConfig.Host,
		"is_local":    connConfig.IsLocal,
		"tls_verify":  connConfig.TLSVerify != "",
		"api_version": connConfig.APIVersion,
	}).Debug("Extracted Docker connection configuration")

	// Build the orchestrator container configuration with Docker env vars.
	config := buildOrchestratorConfig(sourceContainer, newImage, containerChain, connConfig)

	// Build the host configuration with appropriate socket/TLS mounts.
	// The orchestrator inherits the source container's NetworkMode to ensure
	// network connectivity parity (e.g., host network for local Docker access).
	hostConfig := buildOrchestratorHostConfig(connConfig, sourceContainer)

	// Generate a deterministic container name based on the full source container ID.
	// Using the full ID instead of ShortID() guarantees uniqueness and avoids
	// potential collisions from the 12-character truncation.
	orchestratorName := "watchtower-orchestrator-" + string(sourceContainer.ID())

	clog.WithField("orchestrator_name", orchestratorName).
		Debug("Creating ephemeral orchestrator container")

	// Create the container without specifying a platform.
	resp, err := c.api.ContainerCreate(
		ctx,
		config,
		hostConfig,
		&dockerNetwork.NetworkingConfig{},
		nil,
		orchestratorName,
	)
	if err != nil {
		clog.WithError(err).Error("Failed to create ephemeral orchestrator container")

		return "", fmt.Errorf("%w: %w", ErrEphemeralCreateFailed, err)
	}

	orchestratorID := types.ContainerID(resp.ID)

	clog.WithField("orchestrator_id", orchestratorID.ShortID()).
		Debug("Created ephemeral orchestrator container")

	// Start the orchestrator container.
	err = c.api.ContainerStart(
		ctx,
		resp.ID,
		dockerContainer.StartOptions{},
	)
	if err != nil {
		clog.WithError(err).Error("Failed to start ephemeral orchestrator container")

		// Attempt cleanup of the created but not-started container.
		// Use a fresh context so cleanup can proceed even if the original ctx is cancelled.
		ctxCleanup, cancelCleanup := context.WithTimeout(
			context.Background(),
			orchestratorCleanupTimeout,
		)
		defer cancelCleanup()

		//nolint:contextcheck // Fresh context intentional for cleanup to survive parent cancellation.
		cleanupErr := c.api.ContainerRemove(
			ctxCleanup,
			resp.ID,
			dockerContainer.RemoveOptions{Force: true},
		)
		if cleanupErr != nil {
			clog.WithError(cleanupErr).
				Warn("Failed to clean up ephemeral orchestrator after start failure")
		}

		return "", fmt.Errorf("%w: %w", ErrEphemeralStartFailed, err)
	}

	clog.WithField("orchestrator_id", orchestratorID.ShortID()).
		Debug("Started ephemeral orchestrator for self-update")

	return orchestratorID, nil
}

// buildOrchestratorConfig builds the Docker container configuration for the
// ephemeral orchestrator.
//
// The configuration:
//   - Uses the same Watchtower image (no separate image pull needed)
//   - Runs with --self-update-orchestrator flag
//   - Passes old container ID, new image, original name, and container chain via environment
//   - Forwards Docker connection environment variables (DOCKER_HOST, DOCKER_TLS_VERIFY,
//     DOCKER_CERT_PATH, DOCKER_API_VERSION) to maintain connection parity with the source
//   - Sets the orchestrator label for identification
//   - Omits the watchtower label and scope label to avoid excess instance detection
//
// Parameters:
//   - sourceContainer: Current Watchtower container.
//   - newImage: Image reference for the new container.
//   - containerChain: Container chain label for lineage tracking.
//   - connConfig: Docker connection configuration extracted from the source container.
//
// Returns:
//   - *dockerContainer.Config: The container configuration.
func buildOrchestratorConfig(
	sourceContainer types.Container,
	newImage string,
	containerChain string,
	connConfig *DockerConnectionConfig,
) *dockerContainer.Config {
	// Build the environment variables with orchestrator-specific values.
	env := []string{
		fmt.Sprintf("%s=%s", orchestratorOldIDEnv, sourceContainer.ID()),
		fmt.Sprintf("%s=%s", orchestratorNewImageEnv, newImage),
		fmt.Sprintf("%s=%s", orchestratorOriginalNameEnv, sourceContainer.Name()),
		fmt.Sprintf("%s=%s", orchestratorContainerChainEnv, containerChain),
	}

	// Forward Docker connection environment variables to maintain parity with the source.
	// This ensures the ephemeral orchestrator can connect to the same Docker daemon,
	// whether via local socket, remote TCP/TLS, or socket proxy.
	if connConfig != nil {
		env = appendDockerEnvVars(env, connConfig)
	}

	return &dockerContainer.Config{
		Image: newImage,
		Cmd:   []string{"--self-update-orchestrator"},
		Env:   env,
		Labels: map[string]string{
			// Orchestrator label only — watchtower label omitted to avoid excess instance detection.
			OrchestratorLabel: "true",
		},
	}
}

// buildOrchestratorHostConfig builds the Docker host configuration for the
// ephemeral orchestrator.
//
// The configuration ensures:
//   - AutoRemove for automatic cleanup on exit
//   - NetworkMode inherited from the source container for network parity
//   - For local connections: Docker socket mount for container management
//   - For remote connections: No socket mount; relies on environment variables
//   - For TLS connections: Certificate directory mounts when certificates are on the host
//   - No port bindings to avoid conflicts
//   - No restart policy (one-shot container)
//
// Parameters:
//   - connConfig: Docker connection configuration indicating connection type and mounts.
//   - sourceContainer: Source container whose NetworkMode is inherited.
//
// Returns:
//   - *dockerContainer.HostConfig: The host configuration.
func buildOrchestratorHostConfig(
	connConfig *DockerConnectionConfig,
	sourceContainer types.Container,
) *dockerContainer.HostConfig {
	var binds []string

	if connConfig != nil && connConfig.IsLocal {
		// Local connection: mount the Docker socket or named pipe.
		// This is the most common case for standard Docker installations.
		binds = append(binds, connConfig.SocketBind)
	}

	// Add TLS certificate bind mounts if present.
	// These are needed when the source container has TLS certificates
	// that must be accessible to the ephemeral orchestrator.
	if connConfig != nil && len(connConfig.CertBinds) > 0 {
		binds = append(binds, connConfig.CertBinds...)
	}

	// Inherit the source container's NetworkMode to ensure the orchestrator
	// has the same network access as the source (e.g., host network for
	// local Docker daemon communication, custom networks for proxy access).
	var networkMode dockerContainer.NetworkMode
	if containerInfo := sourceContainer.ContainerInfo(); containerInfo != nil &&
		containerInfo.HostConfig != nil {
		networkMode = containerInfo.HostConfig.NetworkMode
	}

	return &dockerContainer.HostConfig{
		AutoRemove:  true,
		Binds:       binds,
		NetworkMode: networkMode,
		// No port bindings — avoids conflicts with the new Watchtower container
		// No restart policy — one-shot container that exits after orchestration
	}
}

// resolveDockerSocketBind derives the Docker socket bind mount from the source
// container's host configuration. It searches for a bind mount that references
// the Docker socket and returns the bind string.
//
// If no socket bind is found in the source config, it returns the platform-appropriate
// default socket path:
//   - Unix: "/var/run/docker.sock:/var/run/docker.sock"
//   - Windows: "//./pipe/docker_engine://./pipe/docker_engine" (named pipe)
//
// Parameters:
//   - sourceHostConfig: The source container's host configuration containing bind mounts.
//
// Returns:
//   - string: The Docker socket bind mount string.
func resolveDockerSocketBind(sourceHostConfig *dockerContainer.HostConfig) string {
	// Try to extract the socket bind from the source container's binds.
	if sourceHostConfig != nil {
		for _, bind := range sourceHostConfig.Binds {
			// Check if this bind mount references the Docker socket.
			// Supports both Unix socket paths and Windows named pipes.
			if isDockerSocketBind(bind) {
				return bind
			}
		}
	}

	// Fall back to platform-appropriate default.
	if isWindows() {
		return defaultWindowsPipePath + ":" + defaultWindowsPipePath
	}

	return defaultUnixSocketPath + ":" + defaultUnixSocketPath
}

// isDockerSocketBind checks if a bind mount string references the Docker socket.
// It matches common patterns for both Unix sockets and Windows named pipes.
//
// This supports:
//   - Standard Unix socket: /var/run/docker.sock
//   - Alternative Unix socket: /run/docker.sock
//   - Rootless Docker: /run/user/<UID>/docker.sock
//   - Docker Desktop macOS: $HOME/.docker/run/docker.sock
//   - Windows named pipe: //./pipe/docker_engine
//   - Podman socket: /var/run/podman/podman.sock
//   - balenaOS socket: /var/run/balena-engine.sock
//
// Parameters:
//   - bind: The bind mount string to check (e.g., "/var/run/docker.sock:/var/run/docker.sock").
//
// Returns:
//   - bool: True if the bind mount references a Docker-compatible socket.
func isDockerSocketBind(bind string) bool {
	// Check for known Docker socket patterns.
	// The bind string format is "host_path:container_path[:options]".
	// We check the entire string to catch both host and container paths.
	socketPatterns := []string{
		"docker.sock",
		"docker_engine",
		"podman.sock",
		"balena-engine.sock",
	}

	for _, pattern := range socketPatterns {
		if strings.Contains(bind, pattern) {
			return true
		}
	}

	return false
}

// isWindows returns true if the current platform is Windows.
//
// Returns:
//   - bool: True on Windows platforms.
func isWindows() bool {
	return runtime.GOOS == "windows"
}

// extractDockerConnectionConfig extracts the Docker connection configuration from
// a source container's environment variables and bind mounts.
//
// This function:
//   - Parses DOCKER_HOST, DOCKER_TLS_VERIFY, DOCKER_CERT_PATH, and DOCKER_API_VERSION
//     from the source container's environment
//   - Detects whether the connection is local (socket/pipe) or remote (TCP/TLS/SSH)
//   - For local connections: derives the socket bind mount from the source container's
//     host config or falls back to the platform-appropriate default
//   - For remote connections: prepares environment variables for passthrough
//   - For TLS connections: prepares certificate directory bind mounts if needed
//
// Parameters:
//   - sourceContainer: The source Watchtower container to extract configuration from.
//
// Returns:
//   - *DockerConnectionConfig: The extracted connection configuration.
func extractDockerConnectionConfig(sourceContainer types.Container) *DockerConnectionConfig {
	config := &DockerConnectionConfig{
		// Default to local connection with platform-appropriate socket.
		IsLocal:    true,
		SocketBind: defaultSocketBind(),
	}

	// Extract environment variables from the source container.
	var containerEnv []string
	if containerInfo := sourceContainer.ContainerInfo(); containerInfo != nil && containerInfo.Config != nil {
		containerEnv = containerInfo.Config.Env
	}

	// Parse Docker connection environment variables.
	for _, envVar := range containerEnv {
		key, value, found := parseEnvVar(envVar)
		if !found {
			continue
		}

		switch key {
		case dockerHostEnv:
			config.Host = value
		case dockerTLSEnv:
			config.TLSVerify = value
		case dockerCertPathEnv:
			config.CertPath = value
		case dockerAPIVersionEnv:
			config.APIVersion = value
		}
	}

	// Determine connection type based on DOCKER_HOST.
	if config.Host != "" {
		config.IsLocal = isLocalDockerHost(config.Host)

		if config.IsLocal {
			// Local connection: extract socket path from DOCKER_HOST.
			socketPath := extractSocketPath(config.Host)
			if socketPath != "" {
				config.SocketBind = socketPath + ":" + socketPath
			}
		} else {
			// Remote connection: clear socket bind as no local mount is needed.
			config.SocketBind = ""
		}
	}

	// For local connections, try to find the socket bind in the source container's mounts.
	// This handles cases where the socket is mounted at a different path inside the container.
	if config.IsLocal {
		var sourceHostConfig *dockerContainer.HostConfig
		if containerInfo := sourceContainer.ContainerInfo(); containerInfo != nil {
			sourceHostConfig = containerInfo.HostConfig
		}

		socketBind := resolveDockerSocketBind(sourceHostConfig)
		if socketBind != "" {
			config.SocketBind = socketBind
		}
	}

	// Prepare TLS certificate bind mounts for remote TLS connections.
	if !config.IsLocal && config.CertPath != "" {
		config.CertBinds = prepareTLSCertBinds(config.CertPath, sourceContainer)
	}

	return config
}

// appendDockerEnvVars appends Docker connection environment variables to the
// environment slice for the ephemeral orchestrator container.
//
// This ensures the orchestrator can connect to the same Docker daemon as the
// source container, regardless of connection type (local socket, remote TCP/TLS,
// or socket proxy).
//
// Parameters:
//   - env: The environment variable slice to append to.
//   - connConfig: The Docker connection configuration.
//
// Returns:
//   - []string: The updated environment variable slice.
func appendDockerEnvVars(env []string, connConfig *DockerConnectionConfig) []string {
	// Always forward DOCKER_HOST if set, as it determines the connection endpoint.
	if connConfig.Host != "" {
		env = append(env, fmt.Sprintf("%s=%s", dockerHostEnv, connConfig.Host))
	}

	// Forward TLS verification setting for remote connections.
	if connConfig.TLSVerify != "" {
		env = append(env, fmt.Sprintf("%s=%s", dockerTLSEnv, connConfig.TLSVerify))
	}

	// Forward TLS certificate path for remote TLS connections.
	if connConfig.CertPath != "" {
		env = append(env, fmt.Sprintf("%s=%s", dockerCertPathEnv, connConfig.CertPath))
	}

	// Forward API version if pinned.
	if connConfig.APIVersion != "" {
		env = append(env, fmt.Sprintf("%s=%s", dockerAPIVersionEnv, connConfig.APIVersion))
	}

	return env
}

// defaultSocketBind returns the platform-appropriate default Docker socket bind mount.
//
// Returns:
//   - string: The default socket bind mount string.
func defaultSocketBind() string {
	if isWindows() {
		return defaultWindowsPipePath + ":" + defaultWindowsPipePath
	}

	return defaultUnixSocketPath + ":" + defaultUnixSocketPath
}

// isLocalDockerHost determines whether a DOCKER_HOST value refers to a local
// connection (Unix socket or Windows named pipe) or a remote connection (TCP/TLS/SSH).
//
// Local connections include:
//   - unix:///path/to/socket (Unix domain socket)
//   - npipe:////./pipe/docker_engine (Windows named pipe)
//
// Remote connections include:
//   - tcp://host:port (TCP, possibly with TLS)
//   - http://host:port (HTTP)
//   - https://host:port (HTTPS with TLS)
//   - ssh://user@host (SSH tunnel)
//
// Parameters:
//   - host: The DOCKER_HOST value to check.
//
// Returns:
//   - bool: True if the connection is local, false if remote.
func isLocalDockerHost(host string) bool {
	// Check for remote connection schemes.
	for _, scheme := range remoteDockerSchemes {
		if strings.HasPrefix(host, scheme) {
			return false
		}
	}

	// Unix sockets and Windows named pipes are local.
	// Also handle scheme-less paths that start with / (Unix) or // (Windows pipe).
	if strings.HasPrefix(host, "unix://") ||
		strings.HasPrefix(host, "npipe://") ||
		strings.HasPrefix(host, "/") ||
		strings.HasPrefix(host, "//") {
		return true
	}

	// Default to local for unrecognized schemes (conservative approach).
	logrus.Warnf("unrecognized host scheme for %q, treating as local", host)

	return true
}

// extractSocketPath extracts the socket file path from a DOCKER_HOST value.
//
// For Unix sockets, it strips the "unix://" prefix.
// For Windows named pipes, it strips the "npipe://" prefix.
//
// Parameters:
//   - host: The DOCKER_HOST value.
//
// Returns:
//   - string: The socket file path, or empty string if extraction fails.
func extractSocketPath(host string) string {
	// Handle unix:// scheme.
	if after, ok := strings.CutPrefix(host, "unix://"); ok {
		return after
	}

	// Handle npipe:// scheme (Windows named pipes).
	if after, ok := strings.CutPrefix(host, "npipe://"); ok {
		return after
	}

	// Handle scheme-less paths.
	if strings.HasPrefix(host, "/") || strings.HasPrefix(host, "//") {
		return host
	}

	return ""
}

// parseEnvVar parses an environment variable string in "KEY=VALUE" format.
//
// Parameters:
//   - envVar: The environment variable string to parse.
//
// Returns:
//   - string: The environment variable key.
//   - string: The environment variable value.
//   - bool: True if the string was successfully parsed.
func parseEnvVar(envVar string) (string, string, bool) {
	parts := strings.SplitN(envVar, "=", envPartsCount)
	if len(parts) != envPartsCount {
		return "", "", false
	}

	return parts[0], parts[1], true
}

// prepareTLSCertBinds prepares bind mounts for TLS certificate directories.
//
// When using a remote Docker host with TLS, the ephemeral orchestrator needs
// access to the TLS certificates (ca.pem, cert.pem, key.pem). This function
// examines the source container's bind mounts to find certificate-related mounts
// and prepares corresponding bind strings for the orchestrator.
//
// Parameters:
//   - certPath: The DOCKER_CERT_PATH value from the source container.
//   - sourceContainer: The source container to extract bind mounts from.
//
// Returns:
//   - []string: List of bind mount strings for TLS certificates.
func prepareTLSCertBinds(
	certPath string,
	sourceContainer types.Container,
) []string {
	var certBinds []string

	// Get the source container's host config for bind mounts.
	var sourceHostConfig *dockerContainer.HostConfig
	if containerInfo := sourceContainer.ContainerInfo(); containerInfo != nil {
		sourceHostConfig = containerInfo.HostConfig
	}

	if sourceHostConfig == nil {
		return certBinds
	}

	// Look for bind mounts that contain TLS certificate files.
	tlsFiles := []string{"ca.pem", "cert.pem", "key.pem"}

	for _, bind := range sourceHostConfig.Binds {
		// Parse the bind mount string, stripping mount options (e.g., :ro, :rw)
		// from the container path to ensure correct path comparisons.
		hostPath, containerPath, _, ok := parseBindMount(bind)
		if !ok {
			continue
		}

		// Check if this bind mount is for TLS certificates.
		// We check if the container path matches or is a parent of the cert path.
		for _, tlsFile := range tlsFiles {
			if strings.Contains(containerPath, tlsFile) ||
				strings.Contains(hostPath, tlsFile) {
				certBinds = append(certBinds, bind)

				break
			}
		}

		// Also check if the entire cert directory is mounted.
		if certPath != "" && (containerPath == certPath ||
			strings.HasPrefix(containerPath, certPath+"/")) {
			// Avoid duplicates.
			if !containsBind(certBinds, bind) {
				certBinds = append(certBinds, bind)
			}
		}
	}

	return certBinds
}

// parseBindMount parses a bind mount string in "host_path:container_path[:options]" format.
//
// Parameters:
//   - bind: The bind mount string to parse.
//
// Returns:
//   - string: The host-side path.
//   - string: The container-side path (without mount options).
//   - string: Mount options (e.g., "ro", "rw"), empty if none.
//   - bool: True if parsing succeeded.
func parseBindMount(bind string) (string, string, string, bool) {
	// Detect Windows drive-letter pattern (e.g., "C:" where len >= 2 and second rune is ':').
	// In that case, locate the separator colon after the drive-letter (index > 1).
	// Otherwise, use the first ':' as the separator.
	var sepIdx int

	if len(bind) >= 2 && bind[1] == ':' {
		// Windows drive-letter detected; find separator after drive letter.
		sepIdx = strings.Index(bind[2:], ":")
		if sepIdx != -1 {
			sepIdx += 2 // adjust for offset into bind[2:]
		}
	} else {
		sepIdx = strings.Index(bind, ":")
	}

	if sepIdx == -1 {
		return "", "", "", false
	}

	hostPath := bind[:sepIdx]
	containerAndOpts := bind[sepIdx+1:]

	// Both parts must be non-empty.
	if hostPath == "" || containerAndOpts == "" {
		return "", "", "", false
	}

	// Strip mount options (e.g., :ro, :rw) from the container path.
	// Format is "host:container[:options]" where options come after a third colon.
	containerPath := containerAndOpts
	options := ""

	before, after, ok := strings.Cut(containerAndOpts, ":")
	if ok {
		containerPath = before
		options = after
	}

	// Container path must be non-empty after stripping options.
	if containerPath == "" {
		return "", "", "", false
	}

	return hostPath, containerPath, options, true
}

// containsBind checks if a bind mount string is already present in the slice.
//
// Parameters:
//   - binds: The slice of bind mount strings.
//   - bind: The bind mount string to check.
//
// Returns:
//   - bool: True if the bind mount is already in the slice.
func containsBind(binds []string, bind string) bool {
	return slices.Contains(binds, bind)
}

// RemoveOrphanedOrchestrators removes any ephemeral orchestrator containers
// that may have persisted due to crashes or unexpected termination.
//
// This is called during startup alongside RemoveExcessWatchtowerInstances to
// ensure a clean state.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: Container client for Docker operations.
//
// Returns:
//   - int: Number of orphaned orchestrators removed.
//   - error: Non-nil if listing or removal fails, nil on success.
func RemoveOrphanedOrchestrators(
	ctx context.Context,
	client Client,
) (int, error) {
	clog := logrus.WithField("function", "RemoveOrphanedOrchestrators")

	clog.Debug("Checking for orphaned ephemeral orchestrator containers")

	// List all containers to find orphaned orchestrators.
	allContainers, err := client.ListContainers(ctx)
	if err != nil {
		clog.WithError(err).Error("Failed to list containers for orchestrator cleanup")

		return 0, fmt.Errorf("failed to list containers: %w", err)
	}

	removed := 0

	for _, c := range allContainers {
		containerInfo := c.ContainerInfo()
		if containerInfo == nil || containerInfo.Config == nil {
			continue
		}

		// Check for the orchestrator label.
		if containerInfo.Config.Labels[OrchestratorLabel] != "true" {
			continue
		}

		clog.WithFields(logrus.Fields{
			"container": c.Name(),
			"id":        c.ID().ShortID(),
		}).Info("Removing orphaned ephemeral orchestrator container")

		err := client.StopAndRemoveContainer(ctx, c, 0)
		if err != nil {
			clog.WithError(err).WithFields(logrus.Fields{
				"container": c.Name(),
				"id":        c.ID().ShortID(),
			}).Warn("Failed to remove orphaned orchestrator container")

			continue
		}

		removed++
	}

	if removed > 0 {
		clog.WithField("count", removed).
			Info("Removed orphaned ephemeral orchestrator containers")
	} else {
		clog.Debug("No orphaned ephemeral orchestrator containers found")
	}

	return removed, nil
}

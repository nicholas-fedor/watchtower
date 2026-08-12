package container

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	dockerSystem "github.com/moby/moby/api/types/system"
	dockerClient "github.com/moby/moby/client"
)

// daemonInfoTTL is how long a Docker Info and mirrors snapshot is reused.
const daemonInfoTTL = 5 * time.Minute

// sharedDaemonInfoCache is reused across imageClient instances in one process.
var sharedDaemonInfoCache = &daemonInfoCache{}

// daemonInfoCache holds one Docker Info snapshot for concurrent check/pull paths.
type daemonInfoCache struct {
	mu       sync.Mutex
	info     *dockerSystem.Info
	expires  time.Time
	fetched  bool
	inflight chan struct{}
}

// daemonInfoResult is the outcome of one Docker Info fetch.
type daemonInfoResult struct {
	info *dockerSystem.Info
	ok   bool
}

// resetDaemonInfoCache clears the process-wide Docker Info cache.
//
// Tests use this to isolate Info() call counts between cases.
func resetDaemonInfoCache() {
	sharedDaemonInfoCache.mu.Lock()
	defer sharedDaemonInfoCache.mu.Unlock()

	sharedDaemonInfoCache.info = nil
	sharedDaemonInfoCache.expires = time.Time{}
	sharedDaemonInfoCache.fetched = false
	sharedDaemonInfoCache.inflight = nil
}

// resolveRegistryMirrorConfig fetches the registry mirror configuration from the Docker daemon.
//
// Successful Info() results are reused for daemonInfoTTL so concurrent check and
// pull paths share one daemon call. Failures are not cached.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//
// Returns:
//   - *dockerSystem.Info: System info with mirror configuration, or nil if unavailable.
func (c imageClient) resolveRegistryMirrorConfig(ctx context.Context) *dockerSystem.Info {
	cache := c.daemonInfo
	if cache == nil {
		cache = sharedDaemonInfoCache
	}

	for {
		cache.mu.Lock()
		if cache.fetched && time.Now().Before(cache.expires) {
			info := cache.info
			cache.mu.Unlock()

			return info
		}

		if cache.inflight != nil {
			wait := cache.inflight
			cache.mu.Unlock()

			select {
			case <-wait:
				// Retry so a failed or panicked leader does not strand later callers.
				continue
			case <-ctx.Done():
				return nil
			}
		}

		break
	}

	// This caller performs the fetch. Others wait on inflight.
	done := make(chan struct{})
	cache.inflight = done
	cache.mu.Unlock()

	defer func() {
		cache.mu.Lock()
		cache.inflight = nil

		close(done)
		cache.mu.Unlock()
	}()

	resolved := c.fetchDaemonInfo(ctx)

	cache.mu.Lock()
	if resolved.ok {
		cache.info = resolved.info
		cache.fetched = true
		cache.expires = time.Now().Add(daemonInfoTTL)
	}
	cache.mu.Unlock()

	if !resolved.ok {
		return nil
	}

	return resolved.info
}

// fetchDaemonInfo loads Docker Info for registry mirror resolution.
//
// Failures are not cached. An empty mirror list is a successful empty snapshot.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//
// Returns:
//   - daemonInfoResult: Snapshot and whether the fetch succeeded.
func (c imageClient) fetchDaemonInfo(ctx context.Context) daemonInfoResult {
	info, err := c.api.Info(
		ctx,
		dockerClient.InfoOptions{},
	)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Msg("Failed to get system info for registry mirror resolution")

		return daemonInfoResult{}
	}

	if info.Info.RegistryConfig == nil || len(info.Info.RegistryConfig.Mirrors) == 0 {
		c.logger().Debug().Msg("No registry mirror configuration in Docker daemon")

		return daemonInfoResult{ok: true}
	}

	sanitized := make([]string, 0, len(info.Info.RegistryConfig.Mirrors))
	for _, m := range info.Info.RegistryConfig.Mirrors {
		u, err := url.Parse(m)
		if err == nil && u.Host != "" {
			sanitized = append(sanitized, u.Host)
		} else {
			sanitized = append(sanitized, "<redacted>")
		}
	}

	c.logger().Debug().
		Strs("global_mirrors", sanitized).
		Msg("Resolved registry mirror configuration from Docker daemon")

	return daemonInfoResult{info: &info.Info, ok: true}
}

// buildMirrorEndpoints returns the list of registry endpoints to try for digest comparison.
//
// It uses the global mirrors from the Docker daemon configuration.
// An empty string in the returned list means use the canonical registry host.
// The canonical host is always appended as the final fallback.
//
// If no mirrors are configured, nil is returned (use canonical behavior).
//
// Parameters:
//   - info: System info with mirror configuration from the Docker daemon (may be nil).
//
// Returns:
//   - []string: List of host overrides to try. Empty string means use canonical host.
func (c imageClient) buildMirrorEndpoints(
	info *dockerSystem.Info,
) []string {
	if info == nil || info.RegistryConfig == nil {
		return nil
	}

	mirrors := info.RegistryConfig.Mirrors
	if len(mirrors) == 0 {
		return nil
	}

	endpoints := make([]string, 0, len(mirrors)+1)

	for _, mirror := range mirrors {
		mirror = strings.TrimSpace(mirror)
		if mirror != "" {
			endpoints = append(endpoints, mirror)
		}
	}

	// Always append canonical host as final fallback.
	endpoints = append(endpoints, "")

	return endpoints
}

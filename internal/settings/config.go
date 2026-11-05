package settings

import (
	"github.com/nicholas-fedor/watchtower/internal/settings/config/api"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/client"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/compatibility"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/docker"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/filter"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/git"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/lifecycle"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/logging"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/mode"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/registry"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/schedule"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/update"
)

// Config is the immutable process configuration snapshot produced by Load.
//
// Each field is a domain group aligned with internal/settings/flags/<domain> registration.
// Use UpdateParams and ClientOptions to project this snapshot into the DTOs
// consumed by the update pipeline and Docker client construction.
type Config struct {
	// Registry holds registry TLS settings.
	Registry registry.Registry
	// Docker holds Docker daemon connection settings (host, TLS, API version, cert path).
	Docker docker.Docker
	// Logging holds console log format and level settings.
	Logging logging.Logging
	// Client holds options passed to container.NewClient (include-stopped, revive-stopped, etc.).
	Client client.Client
	// Compatibility holds runtime compatibility options (for example Podman CPU/memory).
	Compatibility compatibility.Compatibility
	// Mode holds process entry shape (run-once, health-check, porcelain, orchestrator).
	Mode mode.Mode
	// Filter holds container selection inputs and the resolved predicate.
	Filter filter.Filter
	// Schedule holds poll interval, cron spec, and update-on-start.
	Schedule schedule.Schedule
	// Git holds Git association mappings and watcher credentials.
	Git git.Git
	// Notify holds notification URLs and related options for notify.NewNotifier.
	Notify notify.Notify
	// API holds HTTP API transport, auth, and endpoint settings.
	API api.API
	// Update holds container update policy (cleanup, no-pull, compose depends-on, timeouts).
	Update update.Update
	// Lifecycle holds pre/post update lifecycle hook defaults (enable, UID, GID).
	Lifecycle lifecycle.Lifecycle
}

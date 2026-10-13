package settings

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	runtime "github.com/nicholas-fedor/watchtower/internal/settings"
	"github.com/nicholas-fedor/watchtower/pkg/notifications"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Store is the runtime settings store the handler reads and changes.
type Store interface {
	// Effective returns the values in force.
	Effective() runtime.Values
	// Defaults returns the values resolved from flags and environment variables.
	Defaults() runtime.Values
	// Overridden reports which settings were changed at run time.
	Overridden() map[string]bool
	// Persistent reports whether changes are saved across restarts.
	Persistent() bool
	// Update applies changes atomically, where JSON null resets a setting.
	Update(changes map[string]json.RawMessage) error
}

// Setting describes one runtime setting.
type Setting struct {
	// Value is the value in force.
	Value any `json:"value"`
	// Default is the value from flags and environment variables.
	Default any `json:"default"`
	// Changed is true when the value was set at run time instead of by a flag.
	Changed bool `json:"changed"`
}

// Response is the body returned by both methods of the endpoint.
type Response struct {
	// Settings maps each setting name to its state.
	Settings map[string]Setting `json:"settings"`
	// Saved is true when changes are written to the data directory and survive a restart.
	Saved bool `json:"saved"`
	// ScheduleActive is false when Watchtower only updates on request, so the schedule has no effect.
	ScheduleActive bool `json:"schedule_active"`
	// Timestamp is when the response was produced.
	Timestamp string `json:"timestamp"`
	// APIVersion is the API version of the response.
	APIVersion string `json:"api_version"`
}

// TestNotificationRequest is the optional request body for testing notification delivery.
type TestNotificationRequest struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

// Handler serves the /v1/settings endpoint.
type Handler struct {
	log *zerolog.Logger

	store          Store
	scheduleActive bool
	notifier       types.Notifier
	Path           string
}

// New creates a settings handler backed by the given store.
//
// Parameters:
//   - log: Logger for request and change messages. Nil disables logging.
//   - store: Runtime settings store.
//   - scheduleActive: Whether scheduled updates run in this process.
func New(log *zerolog.Logger, store Store, scheduleActive bool) *Handler {
	return NewWithNotifier(log, store, scheduleActive, nil)
}

// NewWithNotifier creates a settings handler with notifier support.
func NewWithNotifier(log *zerolog.Logger, store Store, scheduleActive bool, notifier types.Notifier) *Handler {
	if log == nil {
		nop := zerolog.Nop()
		log = &nop
	}

	return &Handler{
		log:            log,
		store:          store,
		scheduleActive: scheduleActive,
		notifier:       notifier,
		Path:           "/v1/settings",
	}
}

// Get responds with the runtime settings.
//
//	@Summary		Get runtime settings
//	@Description	Returns the settings that can be changed at run time, with each one's value, default, and whether it was changed.
//	@Tags			settings
//	@Produce		json
//	@Success		200	{object}	Response	"Runtime settings"
//	@Failure		401	{string}	string		"Missing or invalid API token"
//	@Security		BearerAuth
//	@Router			/v1/settings [get]
func (h *Handler) Get(c fiber.Ctx) error {
	h.log.Debug().
		Str("method", c.Method()).
		Str("path", c.Path()).
		Str("notify", "no").
		Msg("Received HTTP API settings request")

	return h.send(c)
}

// Patch changes one or more runtime settings.
//
//	@Summary		Change runtime settings
//	@Description	Changes the named settings: monitor_only, cleanup, rolling_restart (booleans) and schedule (cron). JSON null resets one.
//	@Tags			settings
//	@Accept			json
//	@Produce		json
//	@Param			changes	body		map[string]interface{}	true	"Setting names mapped to new values"
//	@Success		200		{object}	Response				"Runtime settings after the change"
//	@Failure		400		{object}	map[string]string		"The change was rejected"
//	@Failure		401		{string}	string					"Missing or invalid API token"
//	@Security		BearerAuth
//	@Router			/v1/settings [patch]
func (h *Handler) Patch(c fiber.Ctx) error {
	var changes map[string]json.RawMessage

	err := json.Unmarshal(c.Body(), &changes)
	if err != nil || len(changes) == 0 {
		return h.sendError(c, "request body must be a JSON object naming at least one setting")
	}

	err = h.store.Update(changes)
	if err != nil {
		h.log.Warn().
			Err(err).
			Str("ip", c.IP()).
			Str("notify", "no").
			Msg("Rejected settings change")

		return h.sendError(c, err.Error())
	}

	names := make([]string, 0, len(changes))
	for name := range changes {
		names = append(names, name)
	}

	slices.Sort(names)

	h.log.Info().
		Strs("settings", names).
		Str("ip", c.IP()).
		Str("notify", "no").
		Msg("Settings changed through the HTTP API")

	return h.send(c)
}

// send writes the current settings.
func (h *Handler) send(c fiber.Ctx) error {
	values := h.store.Effective()
	defaults := h.store.Defaults()
	changed := h.store.Overridden()

	err := c.Status(fiber.StatusOK).JSON(Response{
		Settings: map[string]Setting{
			runtime.KeyMonitorOnly: {
				Value:   values.MonitorOnly,
				Default: defaults.MonitorOnly,
				Changed: changed[runtime.KeyMonitorOnly],
			},
			runtime.KeyCleanup: {
				Value:   values.Cleanup,
				Default: defaults.Cleanup,
				Changed: changed[runtime.KeyCleanup],
			},
			runtime.KeyRollingRestart: {
				Value:   values.RollingRestart,
				Default: defaults.RollingRestart,
				Changed: changed[runtime.KeyRollingRestart],
			},
			runtime.KeySchedule: {
				Value:   values.Schedule,
				Default: defaults.Schedule,
				Changed: changed[runtime.KeySchedule],
			},
			runtime.KeyAutoRollback: {
				Value:   values.AutoRollback,
				Default: defaults.AutoRollback,
				Changed: changed[runtime.KeyAutoRollback],
			},
			runtime.KeyContainerOverrides: {
				Value:   values.ContainerOverrides,
				Default: defaults.ContainerOverrides,
				Changed: changed[runtime.KeyContainerOverrides],
			},
		},
		Saved:          h.store.Persistent(),
		ScheduleActive: h.scheduleActive,
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		APIVersion:     "v1",
	})
	if err != nil {
		return fmt.Errorf("failed to send JSON response: %w", err)
	}

	return nil
}

// TestNotification sends a test notification through configured or requested URLs.
//
//	@Summary		Test notification delivery
//	@Description	Sends an immediate test alert to verify notification services. Optionally accepts a custom URL, title, or message.
//	@Tags			settings
//	@Accept			json
//	@Produce		json
//	@Param			body	body		TestNotificationRequest	false	"Notification parameters"
//	@Success		200		{object}	map[string]interface{}	"Notification test result"
//	@Failure		400		{object}	map[string]interface{}	"No URLs configured or invalid request"
//	@Failure		502		{object}	map[string]interface{}	"Delivery failed"
//	@Security		BearerAuth
//	@Router			/v1/settings/test-notification [post]
func (h *Handler) TestNotification(c fiber.Ctx) error {
	var req TestNotificationRequest
	if len(c.Body()) > 0 {
		_ = json.Unmarshal(c.Body(), &req)
	}

	urls := []string{}
	if req.URL != "" {
		urls = []string{req.URL}
	} else if h.notifier != nil {
		urls = h.notifier.GetURLs()
	}

	if len(urls) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "No notification URLs are configured. Set WATCHTOWER_NOTIFICATION_URL or supply a url in the request.",
		})
	}

	errs := notifications.SendTestNotification(h.log, urls, req.Title, req.Message)
	if len(errs) > 0 {
		errStrs := make([]string, len(errs))
		for i, err := range errs {
			errStrs[i] = err.Error()
		}

		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"success": false,
			"error":   "Failed to deliver notification",
			"details": errStrs,
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"message": "Notification sent successfully",
	})
}

// sendError writes a 400 response with a JSON error body.
func (h *Handler) sendError(c fiber.Ctx, message string) error {
	err := c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error":       message,
		"api_version": "v1",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("failed to send JSON response: %w", err)
	}

	return nil
}

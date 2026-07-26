// Package config resolves process configuration into a single immutable Config.
//
// Flags are declared in internal/flags. This package is the only production site
// that should construct types.UpdateParams and container.ClientOptions for
// run-once, scheduled, and HTTP API update paths.
//
// Domain settings live under internal/config/<domain>. Placement rules:
//
//   - Update policy (stop/start/pull/restart) belongs on Update and flows through UpdateParams.
//   - Container selection belongs on Filter (including label-enable, name lists, and scope).
//   - Schedule and Mode control when the process runs (interval/cron, run-once, health-check).
//   - Client construction options (include-stopped, revive-stopped, remove-volumes, etc.)
//     are projected via ClientOptions.
//   - Notify holds notification settings; notifications.NewNotifier takes Config.Notify.
//   - API holds HTTP API transport and endpoint settings.
//
// Per-run deltas use RunOverrides only (filter override, run-once, skip self-update,
// current container ID). Do not rebuild partial UpdateParams in consumers.
//
// Load expects flag registration, ProcessFlagAliases, SetupLogging, and GetSecretsFromFiles
// to have already run. All domains bind through a process-local Viper instance
// (flag > env > static default) via FlagSpec rows.
package config

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository context

This is `rpeters1430/watchtower`, a personal fork of [nicholas-fedor/watchtower](https://github.com/nicholas-fedor/watchtower) (itself the maintained fork of containrrr/watchtower). The Go module path is still `github.com/nicholas-fedor/watchtower`; keep imports and the `gci`/`gofumpt` module prefix on that path.

The goal of this fork is to improve Watchtower and make it more user friendly, with a web UI as the centrepiece. It is not trying to stay a thin patch on upstream: changes to the core (`cmd/`, `internal/`, `pkg/`) are in scope when they serve usability or the web UI, for example new HTTP API endpoints the UI needs. The web UI is to be built into the Watchtower binary itself (one container, served by the existing Fiber server in `internal/api`), not shipped as a separate service. The UI is being designed fresh, with a high bar for visual polish. The existing `dashboard/` module (see below) is an earlier standalone prototype: do not extend it or carry its markup over; it is only a reference for which API calls a UI needs.

## Commands

Tasks are defined in `Taskfile.yml` and run with [Task](https://taskfile.dev) (`task` lists them). Task recipes use POSIX shell; on Windows run them from Git Bash or the devcontainer, or use the underlying `go` commands directly.

| Purpose | Command |
| --- | --- |
| Build to `bin/watchtower` | `task build` |
| Run (flags after `--`) | `task run -- --help` |
| All tests with coverage | `task test` (`go test -timeout 30s -v -coverprofile coverage.out -covermode atomic ./...`) |
| Race detector | `task test:race` |
| Shuffled double run for leaked state | `task test:repeat` |
| Lint with autofix / check only | `task lint` / `task lint:check` |
| Format | `task fmt` |
| Vet | `task vet` |
| Regenerate mocks | `task mocks` (`task mocks:check` fails on drift) |
| Regenerate Swagger docs | `task swag:gen` (`task swag:fmt`, `task swag:check`) |
| Rewrite golden files | `task test:update-golden` |
| Local Docker image | `task docker:build` |
| Docs site | `task docs` |

Go is not installed on the Windows host this fork is developed on; run the toolchain through Docker instead, for example:

```powershell
docker run --rm -v "${PWD}:/src" -v watchtower-gomod:/go/pkg/mod -v watchtower-gocache:/root/.cache/go-build -w /src -e GOFLAGS=-buildvcs=false golang:1.27 go test -count=1 ./...
```

The `golangci/golangci-lint` image works the same way for linting. `TestContainerLookupWithTimeoutContext` in `cmd/` fails when run this way inside a container, including on an untouched checkout, but passes in GitHub Actions on Linux, Windows and macOS. That failure alone does not indicate a regression; run the `test.yaml` workflow (`gh workflow run test.yaml --ref <branch>`) for the authoritative result.

`golangci-lint` must be installed separately and always needs `--config build/golangci-lint/golangci-lint.yaml`. `swag` and `mockery` are pinned as Go tools in `go.mod` (`go tool swag`, `go tool mockery`).

### Running a single test

The suite mixes two styles, and the selector differs:

- Standard `testing`/testify (most packages): `go test ./internal/config/ -run 'TestName$' -v`
- Ginkgo suites (`internal/actions`, `internal/compose`, `internal/logging`, `pkg/container`, `pkg/notifications`, `pkg/registry/{auth,digest,manifest}`, `pkg/sorter`): `go test ./internal/actions/ -ginkgo.focus 'spec description text' -v`

Ginkgo suites reject `-count>1`; that is why they are listed in `REPEAT_EXCLUDE` in `Taskfile.yml`. Suites are being converted away from Ginkgo, and a package leaves that list when converted.

### Separate Go modules

`./...` from the root does not cover these; each has its own `go.mod`:

- `tools/tplprev` — notification template preview (builds to WASM for the docs site). `task tplprev:test`, `tplprev:lint`, `tplprev:vet`, `tplprev:fmt`.
- `dashboard` — fork-specific dashboard. `go -C dashboard build .` / `go -C dashboard vet .`; no tests yet.
- `examples/http-api/events` — sample SSE client.

### Container image

`.github/workflows/publish-image.yaml` builds `build/docker/Dockerfile.self-local` and pushes it to `ghcr.io/rpeters1430/watchtower` on every push that touches Go code or the Dockerfile. It builds multi-arch (`linux/amd64` and `linux/arm64`) and always publishes the `latest` tag alongside branch and commit sha tags for convenient server/NAS deployment. The upstream release workflows (`release-*.yaml`, `build.yaml`) target Docker Hub and upstream's secrets and are not what publishes this fork's image.

## Generated and guarded artifacts

CI fails when any of these drift from the source, so regenerate and commit them in the same change:

- **Swagger** (`internal/api/swagger/`): generated from `// @...` annotations in `main.go` and the API handlers. After changing annotations or handler signatures run `task swag:gen`.
- **Mocks** (`**/mocks/`): generated by mockery from `build/mockery/mockery.yaml`. Don't hand-edit; run `task mocks` after changing an interface.
- **Golden files** (`testdata/*.golden`, via `internal/testutil/golden`): these pin the user-facing surface — `--help` output, the flag manifest, the env var/label inventory, HTTP API routes and responses, and notification template output. A failing golden test means a user-visible change. If intended, run `task test:update-golden` (sets `UPDATE_GOLDEN=1`) and review the diff.
- **tplprev copies**: `tools/tplprev` cannot import the main module, so it carries generated copies of the built-in notification templates plus lists of the container report methods and JSON keys. After changing a built-in template, `types.ContainerReport`, or the JSON notification output, run `task tplprev:gen` and update the preview module to match.

## Architecture

Watchtower is a single long-running process that polls the Docker daemon, detects stale containers (image digest, or Git ref), and recreates them with the same configuration.

### Startup and control flow

`main.go` builds a `*zerolog.Logger` and calls `cmd.Execute(log)`. `cmd/root.go` is the composition root and holds almost all wiring:

1. `init` — `flags.SetDefaults()` and `flags.RegisterAll(rootCmd)`.
2. `preRun` — `flags.ApplyEnvToFlags` → `flags.SetupLogging` → `flags.ProcessFlagAliases` → `flags.GetSecretsFromFiles` → `flags.EnvConfig` → `config.Load` → construct the Docker client and `notifications.NewNotifier`. This order is a contract that `config.Load` relies on.
3. `run` / `runMain` — builds a `types.RunConfig`, then takes one of three paths that all funnel into the same update closure wrapping `actions.RunUpdatesWithNotifications`:
   - run-once,
   - `scheduling.RunUpgradesOnSchedule` (interval/cron via `robfig/cron`),
   - HTTP API triggers via `api.SetupAndStartAPI`.

   An update lock (`chan bool`) is shared between the scheduler and the API so scans never overlap.

### Configuration pipeline (flags → config → params)

Adding or changing an option touches several layers, kept deliberately separate:

- `internal/flags/<domain>` declares flags as `spec.FlagSpec` rows (static default, env key, list-parse kind). `internal/flags` only declares and binds; resolution is flag > env > default through a process-local Viper.
- `internal/config/<domain>` holds the resolved, immutable settings. `config.Load` is the only production site that should build `types.UpdateParams` and `container.ClientOptions`. Per-run differences go through `config.RunOverrides`; consumers must not rebuild partial `UpdateParams`.
- Domains mirror each other across both trees: docker, client, schedule, mode, update, lifecycle, filter, registry, compat(ibility), api, notify, logging, git.
- A new flag also changes the golden files in `cmd/testdata/` and `internal/flags/testdata/`, and needs a docs entry under `docs/configuration/`.

### Runtime settings and saved state (`internal/settings`)

`config.Config` stays immutable. The few settings that can change while running (`monitor_only`, `cleanup`, `rolling_restart`, `schedule`) live in a `settings.Store` that layers overrides over the flag-derived defaults:

- The store is opened in `runMain` and applied in exactly one place for updates: the `runUpdatesWithNotifications` closure calls `ApplyTo(params)`, which every path (schedule, `/v1/update`) goes through. API read paths use `apiconfig.BuildUpdateParams(opts)` per request for the same reason; do not hoist that call out of a handler closure.
- Schedule changes reach the running scheduler through `ScheduleDeps.ScheduleChanges`, which swaps the cron entry in place.
- `--data-dir` (default `/data`) holds `settings.json` and `history.json`. If the directory is missing or not writable, both fall back to memory only; nothing fails. `--run-once` ignores saved overrides.
- `/v1/settings` (endpoint name `settings`) exposes the store over GET and PATCH. To make another setting editable: add it to `settings.Values`, the `overrides` struct and its `apply`/`set`, `ApplyTo`, the handler's response, the Settings view in `app.js`, and the docs table. A setting baked into the Docker client at construction (for example include-stopped) cannot be changed this way without reworking the client.

### Update engine

- `internal/actions` — orchestration. `update.go` is the core scan/stop/start loop: list → filter → staleness check → dependency sort → lifecycle hooks → stop/recreate → cleanup. Also here: Watchtower self-update (`ephemeral.go`, orphan recovery), image cleanup, disk-space guards, Git-driven updates (`git.go`, `git_compose.go`), and implicit restarts of linked containers.
- `pkg/container` — the Docker API wrapper (`Client` interface over `moby/moby/client`) and the `Container` type that interprets `com.centurylinklabs.watchtower.*` labels. Subpackages `git` (association labels, stamps) and `oci` (image annotations).
- `pkg/registry` — registry auth, digest HEAD checks, manifest URLs, rate-limit handling, and image age for the cooldown feature.
- `pkg/sorter` — topological ordering by links / compose `depends_on`, with cycle detection.
- `pkg/session` — per-scan `Progress` and the `Report` that feeds notifications, metrics, and API responses.
- `pkg/lifecycle`, `pkg/filters` — pre/post hooks and the composable `types.Filter` functions.
- `pkg/types` — shared interfaces (`Container`, `Notifier`, `Report`, `UpdateParams`) that keep the packages above from importing each other.
- `internal/git`, `internal/compose`, `internal/release` — Git ref monitoring (provider HTTP APIs with `ls-remote` fallback), applying Compose services through the `docker/compose` v5 library (no shelling out), and resolving the release tag matching a pulled digest.

### HTTP API (`internal/api`)

Fiber v3. `routes/` registers each endpoint only when enabled and validated; `handlers/<endpoint>/` holds the logic; `config/` holds shared option types and validation. `/v1/*` uses a Bearer token, `/v1/events` (SSE) uses a separate events token, and `/livez`, `/readyz`, `/startupz` are unauthenticated. A new endpoint needs a route, a handler, Swagger annotations, regenerated Swagger docs, updated API golden files, and a page under `docs/http-api/endpoints/`.

### Web UI (`internal/api/handlers/ui`)

Enabled with the `ui` endpoint name and served at `/ui/` by the same Fiber server. It is a static single page embedded with `go:embed` (`static/index.html`, `app.css`, `app.js`, Barlow fonts under the OFL): vanilla JS, no build step, no network requests to other hosts.

- It has no data routes of its own. `POST /ui/session` exchanges the HTTP API token for an HttpOnly, SameSite=Strict `access_token` cookie, which `NewAPIAuthMiddleware` already accepts; the page then calls the normal `/v1/*` endpoints. A feature the UI needs from the server therefore means a new or extended `/v1` endpoint, not a UI-only route.
- `GET /ui/session` tells the page which endpoints are enabled so it can hide or explain missing features. Enabling `ui` deliberately does not switch other endpoints on (`update` changes scheduling behaviour).
- `app.js` builds all content with DOM nodes, never HTML strings, and the page is served with a strict CSP (`script-src 'self'; style-src 'self'`), so no inline scripts, inline style attributes, or CDN assets.
- Design language: petrol/fog palette with light and dark themes, Barlow Condensed headlines, and the ribbed "container end" block whose colour plus a text label carry each container's state. The first thing on the Containers view is a plain sentence stating the fleet's status rather than stat tiles. Copy is sentence case and says what a control does.
- Every request counts toward the per-IP `http-api-rate-limit` (60/min by default), so keep the page frugal with polling.

### Notifications (`pkg/notifications`)

Everything is delivered through shoutrrr. Legacy email/Slack/Teams/Gotify settings are converted to shoutrrr URLs (`notify-upgrade` subcommand does the same conversion for users). The notifier hooks the logger so log lines emitted during a scan are batched into the notification; templates receive the session `Report`.

### Dashboard prototype (`dashboard/`)

Standalone prototype, to be superseded by a UI built into the main binary. A dependency-free `net/http` server (stdlib only, Go 1.23, single `main.go` plus embedded `static/index.html`) that proxies a fixed set of Watchtower `/v1` endpoints. How the prototype works:

- It talks only to Watchtower's HTTP API — no Docker socket, and no changes to the updater.
- The Watchtower API token stays server-side; the browser authenticates to the dashboard with HTTP Basic auth.
- Triggering updates is opt-in via `DASHBOARD_ENABLE_UPDATES=true` and must target only the requested container.
- Persistent history, job queues, scheduling, and rollback are not implemented.

Configured through `WATCHTOWER_URL`, `WATCHTOWER_API_TOKEN`, `DASHBOARD_PASSWORD`, `DASHBOARD_USER`, and `DASHBOARD_ENABLE_UPDATES`; see `dashboard/README.md` and `dashboard/compose.example.yml`.

## Conventions

- **No global logger.** Every function that logs takes a `*zerolog.Logger` (first parameter, before `ctx`, or a `Logger` field on a params struct). Tests use `internal/logging/test_logger.go`.
- Commits follow [Conventional Commits](https://www.conventionalcommits.org/) (`cliff.toml` builds the changelog from them). Upstream requires GPG-signed commits.
- Indentation is set by `.editorconfig`: tabs for Go, 2 spaces for general Markdown/YAML/JSON, 4 spaces for Markdown under `docs/` (MkDocs).
- User docs live in `docs/` and are built with MkDocs (`build/mkdocs/mkdocs.yaml`); user-visible behaviour changes are expected to update them.

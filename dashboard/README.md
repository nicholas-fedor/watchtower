# Watchtower web dashboard (initial foundation)

Standalone, dependency-free Go HTTP dashboard built against the existing Watchtower v1 API. This deliberately does **not** mount the Docker socket or modify Watchtower's updater.

## What works
- List watched containers with current image and status returned by Watchtower
- Manually trigger a non-mutating update availability check
- Show available updates from the most recent check (browser memory only)
- View Watchtower's in-memory scan history
- Manually request an update for a single container (opt-in, disabled by default)
- HTTP Basic authentication for all application routes, with Watchtower API token kept server-side

## Setup
1. Enable Watchtower's HTTP API and the **containers, check, history, and update** endpoints through the upstream documented settings. Set an API token. The API must be reachable from the dashboard's network; do not expose it publicly.
2. Create a Docker network shared by Watchtower and this dashboard, for example `docker network create watchtower`, and attach Watchtower to it with the hostname `watchtower` (or set `WATCHTOWER_URL` to its reachable internal address).
3. Copy `compose.example.yml` to `compose.yml`, create a local `.env` containing `WATCHTOWER_API_TOKEN` and `DASHBOARD_PASSWORD`, then run `docker compose up -d --build` from this directory.
4. Access it through a localhost tunnel or authenticated HTTPS reverse proxy. The example binds port 8096 only on loopback. Do **not** expose Basic auth over plain HTTP on a network.
5. Set `DASHBOARD_ENABLE_UPDATES=true` only after testing the read-only features. The Update button requires explicit confirmation.

### Important limitations
- No persistent SQLite history, job queues, scheduled jobs, or rollback yet. These are planned future steps, not implemented functionality.
- Check results are held only in browser memory and clear on reload.
- Upstream history is in-memory and may disappear after restart.
- All Watchtower API endpoint enablement is subject to its own settings and permissions.
- This dashboard uses no Docker socket, so it cannot directly discover excluded/unwatched containers.
- Use a strong unique password. Deploy behind HTTPS with additional authentication for access from other devices.

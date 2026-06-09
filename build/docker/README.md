# Docker build files

This folder holds the Dockerfiles used to build Watchtower. **All commands below must be
run from the repository root** (the build context is the repo root, not this folder).

| File                       | Produces                       | Source of code          | Typical use                           |
|----------------------------|--------------------------------|-------------------------|---------------------------------------|
| `Dockerfile`               | Runnable image (release)       | Pre-built binary        | Official/CI releases (GoReleaser)     |
| `Dockerfile.self-local`    | Runnable image                 | Your local working tree | Build/run an image from local changes |
| `Dockerfile.self-github`   | Runnable image                 | Latest `main` on GitHub | Build an image from upstream          |
| `Dockerfile.native-binary` | **Just the binary** (no image) | Your local working tree | Get a compiled binary onto the host   |

`Dockerfile` consumes a binary built beforehand (by GoReleaser). The `self-*` and
`native-binary` files compile from source inside the container, so they need nothing but
Docker installed.

## Build the binary for local use (`Dockerfile.native-binary`)

`Dockerfile.native-binary` compiles Watchtower inside Docker and exports **only the binary** to the
host using BuildKit's `--output`. No image is added to your local image store.

```bash
docker build . \
  -f build/docker/Dockerfile.native-binary \
  --target export \
  --output type=local,dest=./bin
```

Result: `./bin/watchtower` — a static (`CGO_ENABLED=0`) Linux binary with the version
stamped in from `git describe`.

### Build for a different architecture

Use `buildx` and `--platform`; `TARGETOS`/`TARGETARCH` are wired through automatically:

```bash
docker buildx build . \
  -f build/docker/Dockerfile.native-binary \
  --platform linux/arm64 \
  --target export \
  --output type=local,dest=./bin
```

For multiple architectures at once, point each at its own destination:

```bash
docker buildx build . -f build/docker/Dockerfile.native-binary \
  --platform linux/amd64 --target export --output type=local,dest=./bin/amd64
docker buildx build . -f build/docker/Dockerfile.native-binary \
  --platform linux/arm64 --target export --output type=local,dest=./bin/arm64
```

### Run it

```bash
./bin/watchtower --help
# Watchtower needs the Docker socket to do anything useful:
./bin/watchtower --run-once
```

### Run it on a schedule with systemd

Instead of running Watchtower as a long-lived daemon, you can drop the binary on the host
and have a systemd timer invoke `--run-once` periodically. Install the binary first:

```bash
sudo install -m 0755 ./bin/watchtower /usr/local/bin/watchtower
```

`/etc/systemd/system/watchtower.service` — a `oneshot` unit that runs a single update pass:

```ini
[Unit]
Description=Watchtower update run
Wants=docker.service
After=docker.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/watchtower --run-once --cleanup
```

`/etc/systemd/system/watchtower.timer` — fires the service daily at 4 AM. This mirrors
Watchtower's own `--schedule "0 0 4 * * *"` cron expression; `OnCalendar` is systemd's
equivalent of that schedule:

```ini
[Unit]
Description=Run Watchtower daily at 4 AM

[Timer]
# Equivalent to Watchtower's --schedule "0 0 4 * * *" (daily at 4 AM)
OnCalendar=*-*-* 04:00:00
Persistent=true

[Install]
WantedBy=timers.target
```

Enable and start the timer (not the service — the timer pulls it in):

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now watchtower.timer

# Inspect schedule and last run:
systemctl list-timers watchtower.timer
journalctl -u watchtower.service
```

## Build a runnable image from local changes (`Dockerfile.self-local`)

If you want a runnable image instead of a bare binary:

```bash
docker build . -f build/docker/Dockerfile.self-local -t nickfedor/watchtower
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock nickfedor/watchtower --run-once
```

See the project's [CONTRIBUTING.md](../../CONTRIBUTING.md) for the full release/dev image
workflow (GoReleaser, multi-arch dev images, etc.).

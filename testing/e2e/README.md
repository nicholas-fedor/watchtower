# Watchtower e2e tests

This directory is a nested [Go](https://go.dev/doc/install) module that drives [Watchtower](https://watchtower.nickfedor.com) as a black box.
It is a **control plane**: Fiber v3 + [Huma](https://huma.rocks/) JSON API, HTMX dashboard, [Postgres](https://www.postgresql.org/) for runs/cases, [Loki](https://grafana.com/oss/loki/) for stdout/stderr.
DinD workers are still Testcontainers (Ryuk reaps **compute only**). Postgres and Loki are Docker Compose named volumes — not Ryuk, not host `daemon.json`.

Root `make test` does not run these tests.

Work from this directory unless a command is shown from the repository root.

```bash
cd testing/e2e
go run . serve          # http://127.0.0.1:9472  dashboard + /v1 + /v1/docs
go run . run --topic ratelimit --limit 20
go run . status
```

`--workers 0` (the default) sizes the DinD pool from host CPU and available RAM (2 GiB per worker, cap 8).

## What you need

- Linux with a [Docker Engine](https://docs.docker.com/engine/install/) that can start [privileged containers](https://docs.docker.com/engine/containers/run/#privileged)
- [Go 1.27](https://go.dev/dl/)
- Disk for [`docker:28.0.1-dind`](https://hub.docker.com/_/docker), [`registry:2.8.3`](https://hub.docker.com/_/registry), and the [`scratch`](https://hub.docker.com/_/scratch) images this suite builds

The first run pulls `docker:28.0.1-dind` and `registry:2.8.3` on the host.
Watchtower under test and the inner daemon must not reach [Docker Hub](https://docs.docker.com/docker-hub/), [GHCR](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry), [LSCR](https://docs.linuxserver.io/), or any other public registry.
Auth challenges, rate limits, and digest checks are faked inside DinD.
If a case talks to the public internet, that is a bug in this harness.

[Testcontainers for Go](https://golang.testcontainers.org/) starts [Ryuk](https://github.com/testcontainers/moby-ryuk) to clean up host containers.
Leave it enabled.
Do not set `TESTCONTAINERS_RYUK_DISABLED=true`.
Long runs raise its timeouts to `TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT=5m` and `TESTCONTAINERS_RYUK_RECONNECTION_TIMEOUT=30s`.
Those variables are documented in the Testcontainers [configuration](https://golang.testcontainers.org/features/configuration/) guide.

## Two speeds

While you are changing Watchtower, do not run the full product.
Pick the area you touched, cap the run, and keep the logs.

```bash
go run . list
go run . run --topic ratelimit --limit 20 --keep
```

`go run . list` prints the topic names.
`--topic` selects that slice.
`--limit 20` stops after twenty cases.
Results live in Postgres and Loki. Watch a sitting in the dashboard or `go run . status <run-id>`.

Before you open or merge a PR, run the unbounded product (hours):

```bash
make test-e2e
```

From this directory that is `make run`.
Shard it if you split the work across machines.

## Quick start

Unit tests (no Docker):

```bash
make test
```

Topics and product size (no Docker):

```bash
make list
```

Privileged DinD ping and flag coverage:

```bash
make doctor
```

Unscoped `--run-once` with Watchtower defaults (one echo subject):

```bash
make run-smoke
```

From the Watchtower repository root, smoke is `make test-e2e-smoke` and the full product is `make test-e2e`.
`make clean` here (or `make test-e2e-clean` from the repository root) only removes leftover `artifacts/` dirs from older file-based sittings.

## Commands

The entrypoint is `testing/e2e/main.go`.
The CLI is built with [Cobra](https://cobra.dev/).

```bash
go run . serve
go run . run --topic ratelimit --limit 20 --keep
go run . run [flags]
go run . status [run-id]
go run . cases --run <id> --status fail
go run . logs --run <id> --case <case-id>
go run . cancel --run <id>
go run . run --resume <run-id>
go run . list [--dump-factors]
go run . doctor
go run . replay --case artifacts/<run-id>/cases/<id>
go run . persona --listen :80 --backend http://127.0.0.1:5000 --persona hub
```

`serve` is the long-lived control plane (embeds from `run` if nothing is listening).
`run` queues a sitting on the API (one-off YAML, `--topic`, or unbounded product).
`list` does not start Docker.
`doctor` starts one DinD worker, pings it, and exits.
`replay` does not re-execute a case from `meta.json`.
Use `run --generator file --file testdata/cases/smoke.yaml` for named YAML cases.
`persona` is the fake Hub/GHCR/LSCR proxy used inside DinD.
You do not need to start it by hand for a normal run.

## How a run is built

The default generator is `product`.
It walks a cartesian product of Watchtower flags and fixture dimensions (process shape, packaging, subject kind, registry persona, and so on).
`go run . list --dump-factors` prints the factor names, level counts, and the total cardinality.
That total is enormous.
You are not expected to finish it in one sitting.

`random` draws full vectors forever until `--limit` or Ctrl-C.
Pass `--seed` if you want the same sequence again.

`file` loads YAML from `--file`.
That is how `testdata/cases/smoke.yaml` is run.

Each case is a complete Watchtower configuration plus a fixture.
Watchtower is launched with flags, environment variables, a mix of both, or [secret files](https://watchtower.nickfedor.com/getting-started/docker-secrets/), depending on `config.channel`.
It runs as a container inside DinD or as a host binary pointed at the inner daemon, depending on `packaging`.
Cases that need Hub, GHCR, or LSCR hostnames require Watchtower inside DinD so [`--add-host`](https://docs.docker.com/reference/cli/docker/container/run/#add-host) entries can point those names at the fake registry.
Binary packaging combined with those personas is skipped.

[`--scope`](https://watchtower.nickfedor.com/advanced-features/running-multiple-instances/) is a Watchtower setting under test.
The harness does not inject it to keep containers from leaking.
Cleanup is the DinD container going away.

## Stopping and resuming

There is no pause command.
Ctrl-C stops the CLI wait.
If `serve` is already running, the sitting keeps going until you cancel it.
If `run` started the control plane itself, Ctrl-C stops that process too.

```bash
go run . cancel --run <run-id>
```

Finished cases are stored in [Postgres](https://www.postgresql.org/).
A case that was still running is marked interrupted and will be tried again.

Resume by run UUID (interrupted, canceled, or still queued):

```bash
go run . run --resume <run-id>
```

That re-queues the same sitting. Completed case IDs in the store are skipped.

## Targeting one change

`--topic` is how you exercise one Watchtower area without walking the whole product.
The names match the kind of work that actually lands in this repo (registry 429 pacing, disk-space gates, cleanup after self-update, container selection, HTTP API, and so on).

```bash
go run . list
```

| You just changed                     | Command                                              |
|--------------------------------------|------------------------------------------------------|
| GHCR/LSCR or Hub rate limits         | `go run . run --topic ratelimit --limit 20 --keep`   |
| Registry auth or personas            | `go run . run --topic registry --limit 20 --keep`    |
| `disk-space-max` / `disk-space-warn` | `go run . run --topic disk --limit 20 --keep`        |
| `--cleanup` or leftover images       | `go run . run --topic cleanup --limit 20 --keep`     |
| Watchtower updating itself           | `go run . run --topic self-update --limit 20 --keep` |
| Labels, skip lists, enable/disable   | `go run . run --topic filters --limit 20 --keep`     |
| HTTP API update/check/metrics        | `go run . run --topic http-api --limit 20 --keep`    |
| Lifecycle hooks                      | `go run . run --topic lifecycle --limit 20 --keep`   |
| Stop timeout or custom signals       | `go run . run --topic stop --limit 20 --keep`        |
| Depends-on / rolling restart         | `go run . run --topic depends --limit 20 --keep`     |
| Notifications                        | `go run . run --topic notify --limit 20 --keep`      |
| Porcelain JSON                       | `go run . run --topic porcelain --limit 20 --keep`   |
| Tokens and secret files              | `go run . run --topic secrets --limit 20 --keep`     |
| Run-once, interval, cron             | `go run . run --topic schedule --limit 20 --keep`    |

`--topic` and `--filter` can be combined.
Both must match.
`--filter` is a [Go regular expression](https://pkg.go.dev/regexp) over the case ID, factor names, and factor values.
Use it when no topic is tight enough, or when you already know a factor name from `go run . list --dump-factors`.

```bash
go run . run --topic ratelimit --filter 'lscr' --limit 20 --keep
go run . run --filter 'flag.cleanup' --limit 10 --keep
```

A named YAML case (unscoped `--run-once` only):

```bash
go run . run --generator file --file testdata/cases/smoke.yaml --keep
```

Depends-on chain (stop dependents first, start dependencies first):

```bash
go run . run --generator file --file testdata/cases/depends-chain.yaml --keep
```

Every YAML key the file generator accepts is listed, with a comment above each key, in `testdata/cases/reference.yaml`.
That file is a catalog.
Do not run it as a case.
Copy the keys you need into a new file under `testdata/cases/`.

`--limit N` stops after N executed cases.
`--offset N` skips the first N cases that already passed topic, filter, and shard.

## Splitting work across machines

`--shard i/n` keeps cases whose ID hashes into bucket `i` of `n`.
`i` is 1-based.

```bash
go run . run --workers 4 --shard 1/4
go run . run --workers 4 --shard 2/4
```

Shards are deterministic for a given case ID.
They do not replace `--filter`.
You can combine them.

`--workers` is how many DinD daemons run in parallel on this machine.
`0` (the default) sizes the pool from host CPU and available RAM (2 GiB per worker, cap 8).
`WATCHTOWER_E2E_WORKERS` sets that default when `--workers` is omitted or `0`. Unset means auto.

## `run` flags

| Flag                                | Meaning                                              |
|-------------------------------------|------------------------------------------------------|
| `--workers N`                       | Parallel DinD workers (`0` means auto from host)     |
| `--shard i/n`                       | Keep hash bucket `i` of `n`                          |
| `--offset N`                        | Skip N selected cases                                |
| `--limit N`                         | Stop after N executed cases (0 means no cap)         |
| `--generator product\|random\|file` | How cases are produced (default `product`)           |
| `--seed S`                          | Seed for `random` (default 1)                        |
| `--file PATH`                       | YAML cases when `--generator file`                   |
| `--resume ID`                       | Re-queue an interrupted or canceled sitting by UUID  |
| `--topic NAME`                      | Named development slice (`go run . list`)            |
| `--filter REGEX`                    | Extra regex on case ID, factor name, or factor value |
| `--keep`                            | Keep extra per-case documents (inspect, porcelain)   |

Environment:

- `WATCHTOWER_SOURCE` is the Watchtower repository to compile (default `../..` from this module)
- `WATCHTOWER_IMAGE` is a prebuilt image instead of compiling
- `WATCHTOWER_E2E_WORKERS` is the default worker count (`0` or unset means auto)
- `WATCHTOWER_E2E_KEEP=1` is the same as `--keep`

## Results

Sittings, cases, inspect documents, and events live in [Postgres](https://www.postgresql.org/).
Watchtower stdout and stderr live in [Loki](https://grafana.com/oss/loki/).
Watch a sitting in the dashboard or with `go run . status`, `go run . cases`, and `go run . logs`.

`--keep` retains extra per-case documents on the sitting (inspect and porcelain). Failed cases keep those documents either way.

`make clean` only removes leftover `artifacts/` dirs from older file-based sittings.

## Watchtower image

The default is a scratch image that only contains the binary you just compiled.
`image.source=self-local` builds `build/docker/Dockerfile.self-local` from Watchtower source when you want something closer to a release image.

## What this suite does not do

It is not wired into [GitHub Actions](https://docs.github.com/en/actions).
It will not fall back to the host Docker daemon if DinD cannot start.
It will not pull application images from the public internet.

## Libraries and services

This module is Go 1.27 and does not import Watchtower packages.
Watchtower is compiled from `WATCHTOWER_SOURCE` and treated as an external binary or image.

| Piece                                                                                  | Role                                                                        | Docs                                                                               |
|----------------------------------------------------------------------------------------|-----------------------------------------------------------------------------|------------------------------------------------------------------------------------|
| [Testcontainers for Go](https://golang.testcontainers.org/) v0.44                      | Starts privileged DinD on the host and registers those containers with Ryuk | [User guide](https://golang.testcontainers.org/)                                   |
| [DinD module](https://golang.testcontainers.org/modules/dind/)                         | `docker:28.0.1-dind` worker, inner API at `tcp://…:2375`                    | [Module page](https://golang.testcontainers.org/modules/dind/)                     |
| [Ryuk](https://github.com/testcontainers/moby-ryuk)                                    | Deletes host Testcontainers resources when the run exits                    | [Garbage collector](https://golang.testcontainers.org/features/garbage_collector/) |
| [Moby Engine API client](https://pkg.go.dev/github.com/moby/moby/client)               | Talks to the inner daemon (create, inspect, build, wait)                    | [API types](https://pkg.go.dev/github.com/moby/moby/api)                           |
| [Distribution Registry](https://distribution.github.io/distribution/) `registry:2.8.3` | In-DinD image store the persona proxy fronts                                | [Image](https://hub.docker.com/_/registry)                                         |
| [Cobra](https://cobra.dev/)                                                            | `serve`, `run`, `status`, `cases`, `logs`, `cancel`, `list`, `doctor`       | [Repository](https://github.com/spf13/cobra)                                       |
| [Fiber](https://docs.gofiber.io/) v3                                                   | HTTP server for `/v1` and the dashboard                                     | [Docs](https://docs.gofiber.io/)                                                   |
| [Huma](https://huma.rocks/)                                                            | JSON API and OpenAPI at `/v1`                                               | [Docs](https://huma.rocks/)                                                        |
| [templ](https://templ.guide/)                                                          | Dashboard markup                                                            | [Docs](https://templ.guide/)                                                       |
| [HTMX](https://htmx.org/)                                                              | Dashboard partials                                                          | [Docs](https://htmx.org/docs/)                                                     |
| [Postgres](https://www.postgresql.org/) 18                                             | Runs, cases, inspect documents, and events                                  | [Docs](https://www.postgresql.org/docs/)                                           |
| [Loki](https://grafana.com/oss/loki/)                                                  | Watchtower stdout and stderr                                                | [Docs](https://grafana.com/docs/loki/latest/)                                      |
| [testify](https://github.com/stretchr/testify)                                         | Unit tests for the engine, registry proxy, and assertions                   | [assert](https://pkg.go.dev/github.com/stretchr/testify/assert)                    |
| [go.yaml.in/yaml](https://github.com/yaml/go-yaml)                                     | Named cases under `testdata/cases/`                                         | [v3 module](https://pkg.go.dev/go.yaml.in/yaml/v3)                                 |
| [Go `regexp`](https://pkg.go.dev/regexp)                                               | `--filter`                                                                  | [Package docs](https://pkg.go.dev/regexp)                                          |

Watchtower behavior the suite is exercising is documented on [watchtower.nickfedor.com](https://watchtower.nickfedor.com):

- [Scheduling](https://watchtower.nickfedor.com/configuration/scheduling/) (`run-once`, interval, cron)
- [HTTP API](https://watchtower.nickfedor.com/http-api/overview/)
- [Logging and porcelain](https://watchtower.nickfedor.com/configuration/logging-and-output/)
- [Docker connection](https://watchtower.nickfedor.com/configuration/docker-connection/)
- [Registry and authentication](https://watchtower.nickfedor.com/configuration/registry-and-authentication/)
- [Lifecycle hooks](https://watchtower.nickfedor.com/advanced-features/lifecycle-hooks/)
- [Linked containers](https://watchtower.nickfedor.com/advanced-features/linked-containers/)
- [Remote hosts](https://watchtower.nickfedor.com/advanced-features/remote-hosts/)
- [Private registries](https://watchtower.nickfedor.com/advanced-features/private-registries/)

The fake Hub, GHCR, and LSCR personas copy public challenge and 429 wire formats.
They are not those services.
See [Docker Hub rate limits](https://docs.docker.com/docker-hub/usage/), [GHCR](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry), and [LinuxServer images](https://docs.linuxserver.io/).

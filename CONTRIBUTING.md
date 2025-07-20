## Prerequisites

The nicholas-fedor/watchtower fork of Watchtower is intended to help renew efforts into maintaining and improving the Watchtower project.

## Tools

To contribute code changes to this project you will need the following development tools:

* [Go](https://go.dev/doc/install)
* [Docker](https://docs.docker.com/engine/installation/)

It is highly recommended to have the latest version of Go installed.
You can check for your current Go version as follows:

```bash
go version
```

## Security

You must have GPG signing of Git commits enabled.
GitHub provides excellent resources for how to configure this:

* [Generating a GPG key](https://docs.github.com/en/authentication/managing-commit-signature-verification/generating-a-new-gpg-key#generating-a-gpg-key)
* [Configuring Git for GPG signing](https://docs.github.com/en/authentication/managing-commit-signature-verification/telling-git-about-your-signing-key#telling-git-about-your-gpg-key)
* [GPG signing Git commits](https://docs.github.com/en/authentication/managing-commit-signature-verification/signing-commits)

## Checking out the code

Do not place your code in the Go source path.

```bash
git clone git@github.com:<your fork>/watchtower.git
cd watchtower
```

## Linting

Watchtower uses [Golangci-lint](https://golangci-lint.run/) to help maintain code quality.
It uses a `.golangci.yaml` configuration file in the root directory.
It can be installed locally using the following [instructions](https://golangci-lint.run/welcome/install/#local-installation).

To use the linter, run the following from the root directory:

```bash
golangci-lint run
```

## Testing

### Mocking

[Mockery](https://vektra.github.io/mockery/latest/) is used to generate mock implementations of interfaces.
It is configured using the `.mockery.yaml` file located in the project's root directory.

To generate new mock implementations of Watchtower's interfaces, run the following from the root directory:

```bash
mockery
```

### Executing Unit Tests

To execute Watchtower's unit tests, run the following from the root directory:

```bash
go test ./... -v
```

## Building

### Binary and Archives

#### Using go build

```bash
go build                               # compiles and packages an executable binary, watchtower
go test ./... -v                       # runs tests with verbose output
./watchtower                           # runs the application (outside of a container)
```

If you don't have it enabled, you'll either have to prefix each command with `GO111MODULE=on` or run `export GO111MODULE=on` before running the commands. [You can read more about modules here.](https://github.com/golang/go/wiki/Modules)

For cross-compiling to other architectures (e.g., amd64, arm64, arm/v7, 386, riscv64), set environment variables like `GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0` before running `go build`. Example for arm/v7:

```bash
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -o watchtower-armhf
```

#### Using GoReleaser

To build the Watchtower binary and archives for production releases, use GoReleaser with the `prod.yml` configuration. This handles cross-compilation, versioning, and packaging for multiple architectures (amd64, i386, armhf, arm64v8, riscv64) and OS (Linux, Windows).

Trigger the `release-prod.yaml` workflow manually via GitHub Actions or on a tag push (e.g., `v1.2.3`) for full builds with SBOM and provenance attestations.

For local testing, run GoReleaser in snapshot mode:

```bash
goreleaser release --config build/goreleaser/prod.yml --snapshot --clean
```

This produces binaries in `dist/` (e.g., `dist/watchtower_linux_amd64/watchtower`) and archives (e.g., `watchtower_linux_amd64_1.11.6.tar.gz` if versioned).

### Docker Image

To build Watchtower images, use GoReleaser for multi-architecture support with attestations.

For dev images, trigger the `release-dev.yaml` workflow manually or on main pushes to core files. Locally:

```bash
goreleaser release --config build/goreleaser/dev.yml --snapshot --clean
```

To build a Watchtower image of your own, use the self-contained Dockerfiles in /build/docker/:

* `/build/docker/Dockerfile.self-local` will build an image based on your current local Watchtower files.
* `/build/docker/Dockerfile.self-github` will build an image based on current Watchtower's repository on GitHub.

```bash
docker build . -f build/docker/Dockerfile.self-local -t nickfedor/watchtower # to build an image from local files
```

For multi-architecture dev images (amd64, i386, armhf, arm64v8, riscv64), use Docker Buildx after cross-compiling binaries to `dist/watchtower_linux_{GOARCH}/watchtower` (matching the dev workflow structure). Alternatively, trigger the `release-dev.yaml` workflow manually via GitHub Actions for dev image builds with SBOM and provenance attestations.

For prod images (with binaries/archives), use the prod config as above.

The shared `build/docker/Dockerfile` is used for both, with COPY watchtower /watchtower matching GoReleaser's binary placement.

## Submitting Pull Requests

* Before submitting, ensure you have GPG signed your Git commits.
* All commit messages are expected to follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) guidelines.
* If the pull request is intended to address an issue from either this fork, another fork, or an upstream issue, please ensure to at least add a comment to reference it.
  GitHub automatically generates cross-references, which is incredibly helpful for anyone else maintaining forks of Watchtower or relying upon the upstream repository.

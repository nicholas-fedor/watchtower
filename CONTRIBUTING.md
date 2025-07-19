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

### Binary

To build the Watchtower binary, run the following from the root directory:

```bash
go build                               # compiles and packages an executable binary, watchtower
go test ./... -v                       # runs tests with verbose output
./watchtower                           # runs the application (outside of a container)
```

If you don't have it enabled, you'll either have to prefix each command with `GO111MODULE=on` or run `export GO111MODULE=on` before running the commands. [You can read more about modules here.](https://github.com/golang/go/wiki/Modules)

### Docker Image

To build a Watchtower image of your own, use the self-contained Dockerfiles in /build/docker/:

* `/build/docker/Dockerfile.self-local` will build an image based on your current local Watchtower files.
* `/build/docker/Dockerfile.self-github` will build an image based on current Watchtower's repository on GitHub.

```bash
sudo docker build . -f build/docker/Dockerfile.self-local -t nickfedor/watchtower # to build an image from local files
```

For multi-arch builds, use Docker Buildx and GoReleaser configs in /build/goreleaser/ for prebuilding binaries.

## Submitting Pull Requests

* Before submitting, ensure you have GPG signed your Git commits.
* All commit messages are expected to follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) guidelines.
* If the pull request is intended to address an issue from either this fork, another fork, or an upstream issue, please ensure to at least add a comment to reference it.
  GitHub automatically generates cross-references, which is incredibly helpful for anyone else maintaining forks of Watchtower or relying upon the upstream repository.

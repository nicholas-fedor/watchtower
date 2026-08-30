package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	containerTypes "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/testing/e2e/registry"
)

const (
	// personaImage is the scratch image that runs the Hub/GHCR/LSCR mock.
	personaImage = "e2e/persona:local"
	// personaName is the persona container name inside DinD.
	personaName = "e2e-persona"
)

// BuildPersonaBinary compiles the e2e CLI for linux so it can serve Hub/GHCR/LSCR inside DinD.
//
// Parameters:
//   - ctx: Cancellation.
//   - moduleRoot: testing/e2e directory.
//   - output: Destination path.
//
// Returns:
//   - error: Compile failure.
func BuildPersonaBinary(ctx context.Context, moduleRoot, output string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", output, ".")
	cmd.Dir = moduleRoot

	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")

	raw, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build persona: %w: %s", err, raw)
	}

	return nil
}

// StartPersona puts the registry mock on 127.0.0.1:5000 in front of distribution.
//
// Dockerd push/pull still uses 127.0.0.1:5000. The persona speaks Hub/GHCR/LSCR
// and can inject 429 or quota-no-429 bodies. Watchtower never talks to live Hub.
//
// Parameters:
//   - ctx: Cancellation.
//   - daemon: Inner DinD worker.
//   - binary: Linux e2e CLI binary.
//   - persona: Dialect to speak.
//   - backend: Distribution registry base URL, for example http://172.17.0.2:5000.
//
// Returns:
//   - error: Build, start, or readiness failure.
func StartPersona(ctx context.Context, daemon *Daemon, binary string, persona registry.Persona, backend string) error {
	if persona == "" || persona == registry.PersonaNone {
		persona = registry.PersonaHub
	}

	if !HasImage(ctx, daemon.Client(), personaImage) {
		dockerfile := "FROM scratch\nCOPY persona /persona\nENTRYPOINT [\"/persona\"]\n"

		tarStream, err := ContextTar(dockerfile, "persona", binary)
		if err != nil {
			return err
		}

		build, buildErr := daemon.Client().ImageBuild(ctx, tarStream, client.ImageBuildOptions{
			Tags:       []string{personaImage},
			Dockerfile: "Dockerfile",
			Remove:     true,
		})
		if buildErr != nil {
			return fmt.Errorf("build persona image: %w", buildErr)
		}

		_, _ = io.Copy(io.Discard, build.Body)
		_ = build.Body.Close()
	}

	port, portErr := network.ParsePort("80/tcp")
	if portErr != nil {
		return fmt.Errorf("persona port: %w", portErr)
	}

	created, createErr := daemon.Client().ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &containerTypes.Config{
			Image: personaImage,
			Cmd: []string{
				"persona",
				"--listen", ":80",
				"--backend", backend,
				"--persona", string(persona),
			},
			ExposedPorts: network.PortSet{port: {}},
		},
		HostConfig: &containerTypes.HostConfig{
			PortBindings: network.PortMap{
				port: []network.PortBinding{{HostPort: "5000"}},
			},
		},
		Name: personaName,
	})
	if createErr != nil {
		if isConflict(createErr) {
			return waitRegistry(ctx, daemon)
		}

		return fmt.Errorf("create persona: %w", createErr)
	}

	_, startErr := daemon.Client().ContainerStart(ctx, created.ID, client.ContainerStartOptions{})
	if startErr != nil {
		return fmt.Errorf("start persona: %w", startErr)
	}

	netErr := attachToRegistryNet(ctx, daemon.Client(), created.ID)
	if netErr != nil {
		return netErr
	}

	return waitRegistry(ctx, daemon)
}

// SetPersonaFault arms a registry fault on the in-DinD persona proxy.
//
// Parameters:
//   - ctx: Cancellation.
//   - daemon: Inner DinD worker.
//   - fault: Fault kind.
//
// Returns:
//   - error: Control request failure.
func SetPersonaFault(ctx context.Context, daemon *Daemon, fault registry.Fault) error {
	if fault == "" || fault == registry.FaultNone {
		return nil
	}

	body := `{"fault":"` + string(fault) + `","after":0}`

	out, err := daemon.Exec(ctx, []string{
		"wget", "-O", "-",
		"--header=Content-Type: application/json",
		"--post-data=" + body,
		"http://127.0.0.1:5000/e2e-control/fault",
	})
	if err != nil {
		return fmt.Errorf("set persona fault: %w", err)
	}

	if !strings.Contains(out, "armed") {
		return fmt.Errorf("%w: unexpected response %q", errPersonaFault, out)
	}

	if err != nil {
		return fmt.Errorf("set persona fault: %w", err)
	}

	return nil
}

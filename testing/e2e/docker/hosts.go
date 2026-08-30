package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/testing/e2e/registry"
)

// ExtraHosts maps Hub/GHCR/LSCR names onto the persona proxy IP for the inner daemon
// and the Watchtower container. Image refs keep real DNS names so Watchtower's
// Hub/LSCR/GHCR branches fire. Do not rewrite refs to fake:5000 for those personas.
//
// Parameters:
//   - persona: Registry dialect.
//   - proxyIP: Inner-network IP of the persona proxy.
//
// Returns:
//   - []string: extra_hosts entries in hostname:ip form.
func ExtraHosts(persona registry.Persona, proxyIP string) []string {
	names := registry.HostsFor(persona)

	hosts := make([]string, 0, len(names))
	for _, name := range names {
		hosts = append(hosts, name+":"+proxyIP)
	}

	return hosts
}

// PersonaProxyIP is the inner IPv4 of the e2e-persona container.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//
// Returns:
//   - string: IPv4 address.
//   - error: Inspect failure or missing address.
func PersonaProxyIP(ctx context.Context, cli *client.Client) (string, error) {
	view, err := cli.ContainerInspect(ctx, personaName, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect persona: %w", err)
	}

	ip := containerIP(view)
	if ip == "" {
		return "", errNoContainerIP
	}

	return ip, nil
}

// AppendHosts writes extra_hosts entries into the DinD container's /etc/hosts.
//
// Dockerd resolves lscr.io/ghcr.io/docker.io for ImagePull.
// Watchtower's own extra_hosts only cover its process, not the daemon.
//
// Parameters:
//   - ctx: Cancellation.
//   - entries: hostname:ip pairs from ExtraHosts.
//
// Returns:
//   - error: Exec failure.
func (d *Daemon) AppendHosts(ctx context.Context, entries []string) error {
	if len(entries) == 0 {
		return nil
	}

	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		host, ip, ok := strings.Cut(entry, ":")
		if !ok || host == "" || ip == "" {
			continue
		}

		lines = append(lines, ip+" "+host)
	}

	if len(lines) == 0 {
		return nil
	}

	script := "printf '%s\\n' " + strings.Join(quoteArgs(lines), " ") + " >> /etc/hosts"
	_, err := d.Exec(ctx, []string{"sh", "-c", script})
	if err != nil {
		return fmt.Errorf("append /etc/hosts: %w", err)
	}

	return nil
}

func quoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = "'" + strings.ReplaceAll(arg, "'", `'"'"'`) + "'"
	}

	return out
}

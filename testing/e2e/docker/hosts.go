package docker

import "github.com/nicholas-fedor/watchtower/testing/e2e/registry"

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

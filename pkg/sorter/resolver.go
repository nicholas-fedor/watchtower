package sorter

import (
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// linkResolver resolves dependency links to the containers they name.
type linkResolver struct {
	// matchIDSet holds the identifiers and aliases of the graph's members.
	matchIDSet map[string]bool
	// aliasToCanonical maps each alias of a member to its identifier.
	aliasToCanonical map[string]string
	// watchtowers maps the exact names of monitored Watchtower containers to them.
	watchtowers map[string]types.Container
	// unmonitored maps the exact names of unmonitored containers to them.
	unmonitored map[string]types.Container
	// watchtowerAliases maps the identifier without its replica number of each
	// Watchtower container that is the only one with that identifier to it.
	watchtowerAliases map[string]types.Container
}

// resolve returns the containers a link names.
//
// A link that names a member exactly resolves to that member. Otherwise, a
// link that names a Watchtower container exactly, or an unmonitored container
// exactly, names only that container. Otherwise a link that names a Watchtower
// container without its replica number, when it is the only such Watchtower
// container, names it. Otherwise the link is matched to members as
// findMatchingIdentifiersInSet matches it, by replica or service name.
//
// Parameters:
//   - link: A normalized link from Container.Links.
//
// Returns:
//   - []string: Identifiers of the members the link names.
//   - types.Container: The Watchtower container the link names, or nil.
//   - bool: Whether the link names any container, monitored or not.
func (r *linkResolver) resolve(link string) ([]string, types.Container, bool) {
	if !r.matchIDSet[link] {
		if watchtower, ok := r.watchtowers[link]; ok {
			return nil, watchtower, true
		}

		if _, ok := r.unmonitored[link]; ok {
			return nil, nil, true
		}

		if watchtower, ok := r.watchtowerAliases[link]; ok {
			return nil, watchtower, true
		}
	}

	keys := resolveLinkToCanonicalKeys(link, r.matchIDSet, r.aliasToCanonical)

	return keys, nil, len(keys) > 0
}

// unresolvedDependsOn returns the Watchtower depends-on label entries of c that
// name no container, or a service that more than one container could be. An
// entry naming c itself is a self-dependency, which the update reports on its
// own.
//
// Parameters:
//   - log: Process logger.
//   - c: A monitored container.
//   - self: The identifier of c in the graph, or empty when c is not a member.
//
// Returns:
//   - []UnresolvedLink: The entries that name no container.
func (r *linkResolver) unresolvedDependsOn(log *zerolog.Logger, c types.Container, self string) []UnresolvedLink {
	concrete, ok := c.(*container.Container)
	if !ok {
		return nil
	}

	var unresolved []UnresolvedLink

	for _, link := range container.GetLinksFromWatchtowerLabel(concrete, log) {
		if link == c.Name() || (self != "" && link == self) {
			continue
		}

		if _, _, named := r.resolve(link); !named {
			unresolved = append(unresolved, UnresolvedLink{Container: c, Link: link})
		}
	}

	return unresolved
}

// namesPerContainer is the number of exact names of a container: its
// identifier, its name, and its ID.
const namesPerContainer = 3

// exactNames maps the identifier, name, and ID of each container to it, as a
// link names a container exactly.
//
// Parameters:
//   - containers: Containers to index.
//
// Returns:
//   - map[string]types.Container: Container for each exact name.
func exactNames(containers []types.Container) map[string]types.Container {
	names := make(map[string]types.Container, len(containers)*namesPerContainer)

	for _, c := range containers {
		for _, name := range []string{
			util.NormalizeContainerName(container.ResolveContainerIdentifier(c)),
			util.NormalizeContainerName(c.Name()),
			string(c.ID()),
		} {
			if _, taken := names[name]; name != "" && !taken {
				names[name] = c
			}
		}
	}

	return names
}

// watchtowerAliases maps each Watchtower container's identifier without its
// replica number to it, when only one Watchtower container has that identifier
// and no Watchtower container has that exact name. A Compose depends_on entry
// links to a service that way, and the name identifies one container, as a
// replica match does for members.
//
// Parameters:
//   - watchtowers: Watchtower containers to index.
//   - names: The exact names of the Watchtower containers.
//
// Returns:
//   - map[string]types.Container: Container for each alias.
func watchtowerAliases(watchtowers []types.Container, names map[string]types.Container) map[string]types.Container {
	aliases := make(map[string]types.Container, len(watchtowers))
	replicas := make(map[string]int, len(watchtowers))

	for _, c := range watchtowers {
		identifier := util.NormalizeContainerName(container.ResolveContainerIdentifier(c))
		if service := replicaService(identifier); service != identifier {
			replicas[service]++
		}
	}

	for _, c := range watchtowers {
		identifier := util.NormalizeContainerName(container.ResolveContainerIdentifier(c))

		service := replicaService(identifier)
		if _, taken := names[service]; service != identifier && replicas[service] == 1 && !taken {
			aliases[service] = c
		}
	}

	return aliases
}

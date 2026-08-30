package engine

const (
	// InnerRegistryHost is the loopback registry dockerd uses inside DinD.
	InnerRegistryHost = "127.0.0.1:5000"
	// SubjectRepository is the dummy subject image path.
	SubjectRepository = "e2e/app"
	// SubjectTag is the dummy subject tag.
	SubjectTag = "latest"
	// SkipRepository is the image name used by filter.stack monitor-skip.
	SkipRepository = "e2e/skip"
	// SubjectNamePattern matches harness-prefixed subject container names.
	SubjectNamePattern = ".*-subject(-[0-9]+)?$"
	// DecoyNamePattern matches harness-prefixed decoy container names.
	DecoyNamePattern = ".*e2e-decoy$"
)

// SubjectImageRef is the image name running subjects are created with.
//
// Returns:
//   - string: 127.0.0.1:5000/e2e/app:latest
func SubjectImageRef() string {
	return InnerRegistryHost + "/" + SubjectRepository + ":" + SubjectTag
}

// SkipImageRef is the skip-list image for filter.stack monitor-skip.
//
// Returns:
//   - string: 127.0.0.1:5000/e2e/skip:latest
func SkipImageRef() string {
	return InnerRegistryHost + "/" + SkipRepository + ":" + SubjectTag
}

// ImageRefForPersona is the name subjects run under so Watchtower hits the fake registry.
//
// Hub/GHCR/LSCR keep public DNS names (extra_hosts hijack). none/private stay on the inner registry.
//
// Parameters:
//   - persona: registry.persona level.
//
// Returns:
//   - string: Image reference for CreateSubject and monitor filters.
func ImageRefForPersona(persona string) string {
	switch persona {
	case "hub":
		return "docker.io/" + SubjectRepository + ":" + SubjectTag
	case "ghcr":
		return "ghcr.io/" + SubjectRepository + ":" + SubjectTag
	case "lscr":
		return "lscr.io/" + SubjectRepository + ":" + SubjectTag
	default:
		return SubjectImageRef()
	}
}

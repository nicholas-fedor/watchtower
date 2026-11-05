package git

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nicholas-fedor/watchtower/internal/git/semver"
)

const (
	// DefaultRef is used when a mapping or label omits a Git ref.
	DefaultRef = "main"
	// DefaultPolicy is used when a mapping or label omits a semver policy.
	DefaultPolicy = semver.PolicyNone
)

var (
	// ErrInvalidGitImage indicates a --git-image value could not be parsed.
	ErrInvalidGitImage = errors.New("invalid --git-image value")
	// ErrInvalidComposeProject indicates a --compose-project value is not name=/path.
	ErrInvalidComposeProject = errors.New("invalid --compose-project value")
)

// Image is a non-secret Git association for one image, set with the
// git-image configuration option.
type Image struct {
	// Repo is the Git repository URL.
	Repo string `json:"repo"`
	// Ref is the branch or tag to follow.
	Ref string `json:"ref"`
	// Policy is the semver policy for release tags.
	Policy string `json:"policy"`
	// Dockerfile is the Dockerfile path relative to the build context.
	Dockerfile string `json:"dockerfile,omitempty"`
	// Context is the subdirectory of the repository used as the build context.
	Context string `json:"context,omitempty"`
}

// Defaults holds the credential-free Git settings that decide whether and how
// a container is associated with a Git repository. Settings without
// credentials can be passed to the update packages.
type Defaults struct {
	// Images maps exact image names (including tag) to Git associations.
	Images map[string]Image
	// DefaultRef is the ref used when a mapping or label omits one.
	DefaultRef string
	// SemverPolicy is the semver policy used when a mapping or label omits one.
	SemverPolicy string
	// Dockerfile is the process-wide default Dockerfile path relative to the build context.
	Dockerfile string
	// Context is the process-wide default subdirectory of a Git URL context used as the Docker build context.
	Context string
	// Enable is the process-wide watcher default (--git-enable / WATCHTOWER_GIT_ENABLE).
	Enable bool
}

// Git holds process-wide Git metadata and watcher settings.
type Git struct {
	// Defaults holds the credential-free association settings.
	Defaults

	// ComposeProjects maps Compose project name to a directory inside Watchtower.
	ComposeProjects map[string]string
	// Token is the HTTPS auth token. Never projected onto the update params.
	Token string
	// Username is the basic-auth username. Never projected onto the update params.
	Username string
	// Password is the basic-auth password. Never projected onto the update params.
	Password string
	// SSHKeyPath is the path to an SSH private key. Never projected onto the update params.
	SSHKeyPath string
	// SSHKnownHosts is the path to an SSH known_hosts file. Never projected onto the update params.
	SSHKnownHosts string
	// CABundle is the PEM CA bundle for Git HTTPS. Never projected onto the update params.
	CABundle []byte

	// Timeout is the Git network timeout (--git-timeout / WATCHTOWER_GIT_TIMEOUT).
	Timeout time.Duration
	// InsecureSkipTLS skips TLS verification for Git HTTPS.
	InsecureSkipTLS bool
	// ComposeStash saves and restores local Compose project files around checkout.
	ComposeStash bool
}

// parseImageMapping parses one --git-image value of the form image=repo[#ref][@policy].
//
// Policy suffixes are recognized only when they are a known policy name so
// userinfo in SSH URLs (git@host:path) is not treated as a policy.
//
// Parameters:
//   - raw: Raw mapping string.
//
// Returns:
//   - image: Exact image name including tag.
//   - mapping: Parsed repo, ref, and policy.
//   - error: Non-nil when the value is malformed or the policy is unknown.
func parseImageMapping(raw string) (string, Image, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", Image{}, fmt.Errorf("%w: empty value", ErrInvalidGitImage)
	}

	image, rest, ok := strings.Cut(raw, "=")
	if !ok {
		return "", Image{}, fmt.Errorf("%w: %q (want image=repo[#ref][@policy])", ErrInvalidGitImage, raw)
	}

	image = strings.TrimSpace(image)
	rest = strings.TrimSpace(rest)

	if image == "" || rest == "" {
		return "", Image{}, fmt.Errorf("%w: %q (want image=repo[#ref][@policy])", ErrInvalidGitImage, raw)
	}

	policy := DefaultPolicy

	if at := strings.LastIndex(rest, "@"); at >= 0 {
		suffix := strings.TrimSpace(rest[at+1:])
		if parsed := semver.ParsePolicy(suffix); parsed != "" {
			policy = parsed
			rest = rest[:at]
		} else if isPolicyToken(suffix) {
			return "", Image{}, fmt.Errorf("%w: %q", semver.ErrInvalidPolicy, suffix)
		}
	}

	ref := DefaultRef
	if hash := strings.LastIndex(rest, "#"); hash >= 0 {
		ref = strings.TrimSpace(rest[hash+1:])
		rest = rest[:hash]

		if ref == "" {
			ref = DefaultRef
		}
	}

	repo := strings.TrimSpace(rest)
	if repo == "" {
		return "", Image{}, fmt.Errorf("%w: %q (empty repo)", ErrInvalidGitImage, raw)
	}

	return image, Image{
		Repo:   repo,
		Ref:    ref,
		Policy: policy,
	}, nil
}

// ParseImageMappings parses repeatable --git-image values.
//
// Duplicate image keys keep the last mapping.
//
// Parameters:
//   - values: Raw mapping strings.
//
// Returns:
//   - map[string]Image: Image name to mapping.
//   - error: Non-nil when any value is invalid.
func ParseImageMappings(values []string) (map[string]Image, error) {
	if len(values) == 0 {
		return map[string]Image{}, nil
	}

	images := make(map[string]Image, len(values))

	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		image, mapping, err := parseImageMapping(raw)
		if err != nil {
			return nil, err
		}

		images[image] = mapping
	}

	return images, nil
}

// isPolicyToken reports whether s looks like an explicit @policy suffix.
//
// Userinfo in SSH URLs (git@host) is not treated as a policy.
//
// Parameters:
//   - s: Text after @ in a git-image mapping.
//
// Returns:
//   - bool: True when s is a bare policy name.
func isPolicyToken(s string) bool {
	if s == "" {
		return false
	}

	return !strings.ContainsAny(s, "/:@")
}

// ParseComposeProjects parses repeatable --compose-project values of the form name=/path.
//
// Parameters:
//   - values: Raw name=/path strings.
//
// Returns:
//   - map[string]string: Project name to directory.
//   - error: Non-nil when a value is not name=/path.
func ParseComposeProjects(values []string) (map[string]string, error) {
	out := make(map[string]string, len(values))

	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		name, dir, ok := strings.Cut(raw, "=")
		name = strings.TrimSpace(name)
		dir = strings.TrimSpace(dir)

		if !ok || name == "" || dir == "" {
			return nil, fmt.Errorf("%w: %q", ErrInvalidComposeProject, raw)
		}

		out[name] = dir
	}

	return out, nil
}

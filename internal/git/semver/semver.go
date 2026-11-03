package semver

// Git semver-policy names used by labels, --git-image mappings, and UpdateParams.
const (
	// PolicyNone follows a single branch or tag ref without advancing tags.
	PolicyNone = "none"
	// PolicyPatch advances to newer patch releases of the same minor version.
	PolicyPatch = "patch"
	// PolicyMinor advances to newer minor or patch releases of the same major version.
	PolicyMinor = "minor"
	// PolicyMajor advances to any newer semantic version.
	PolicyMajor = "major"
)

// ValidPolicy reports whether policy is one of none, patch, minor, or major.
//
// Parameters:
//   - policy: Policy name to check.
//
// Returns:
//   - bool: True when policy is recognized.
func ValidPolicy(policy string) bool {
	switch policy {
	case PolicyNone, PolicyPatch, PolicyMinor, PolicyMajor:
		return true
	default:
		return false
	}
}

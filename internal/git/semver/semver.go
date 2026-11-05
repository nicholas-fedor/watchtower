package semver

import (
	"errors"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
)

// Git semver-policy names used by labels, --git-image mappings, and the update params.
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

// Layout of the dot-separated parts of a semantic version.
const (
	// partCount is the number of dot-separated major, minor, and patch parts.
	partCount = 3
	// patchIndex is the position of the patch part.
	patchIndex = 2
)

// ErrInvalidPolicy indicates a semver policy that is not none, patch, minor,
// or major.
var ErrInvalidPolicy = errors.New("invalid git semver policy")

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

// ParsePolicy returns the policy that raw names.
//
// Matching ignores surrounding whitespace and letter case.
//
// Parameters:
//   - raw: Policy name as written in a label or setting.
//
// Returns:
//   - string: The policy in lower case. It is empty when raw names no policy.
func ParsePolicy(raw string) string {
	policy := strings.ToLower(strings.TrimSpace(raw))
	if !ValidPolicy(policy) {
		return ""
	}

	return policy
}

// SelectTag returns the highest policy-allowed tag newer than lastTag.
//
// Non-semver tags and pre-releases are ignored. Build metadata does not
// affect ordering. The none policy and unknown policies select nothing. When
// no tag qualifies, it returns an empty name and false.
//
// Parameters:
//   - lastTag: Last observed tag (empty selects the highest allowed tag).
//   - tags: Remote tag names.
//   - policy: none/patch/minor/major.
//
// Returns:
//   - string: Selected tag name as advertised by the remote.
//   - bool: True when a newer allowed tag exists.
func SelectTag(lastTag string, tags []string, policy string) (string, bool) {
	if policy == PolicyNone || !ValidPolicy(policy) {
		return "", false
	}

	last := canonicalize(Baseline(lastTag))

	allowed := tags
	if last != "" {
		allowed = slices.DeleteFunc(slices.Clone(tags), func(tag string) bool {
			return !allowedBump(last, canonicalize(tag), policy)
		})
	}

	best := HighestRelease(allowed)

	return best, best != ""
}

// HighestRelease returns the highest release tag in tags.
//
// Tags are compared as semantic versions, with or without a leading v.
// Non-semver tags and pre-releases are skipped. Build metadata does not affect
// ordering. Of tags that compare equal, the name that sorts first wins. The
// result therefore does not depend on the order of tags.
//
// Parameters:
//   - tags: Tag names in any order.
//
// Returns:
//   - string: The highest release tag as given. It is empty when there is none.
func HighestRelease(tags []string) string {
	bestName := ""
	bestCanon := ""

	for _, tag := range tags {
		canon := canonicalize(tag)
		if !semver.IsValid(canon) || semver.Prerelease(canon) != "" {
			continue
		}

		order := 1
		if bestCanon != "" {
			order = semver.Compare(canon, bestCanon)
		}

		if order > 0 || (order == 0 && tag < bestName) {
			bestCanon = canon
			bestName = tag
		}
	}

	return bestName
}

// allowedBump reports whether candidate is a newer release allowed by policy.
//
// Parameters:
//   - current: Canonical last tag.
//   - candidate: Canonical candidate tag.
//   - policy: patch, minor, or major.
//
// Returns:
//   - bool: True when candidate is newer and within the policy window.
func allowedBump(current, candidate, policy string) bool {
	if semver.Compare(candidate, current) <= 0 {
		return false
	}

	switch policy {
	case PolicyPatch:
		return semver.MajorMinor(current) == semver.MajorMinor(candidate)
	case PolicyMinor:
		return semver.Major(current) == semver.Major(candidate)
	case PolicyMajor:
		return true
	default:
		return false
	}
}

// canonicalize returns a semver string golang.org/x/mod/semver accepts.
//
// Parameters:
//   - tag: Raw tag name.
//
// Returns:
//   - string: Tag, optionally prefixed with v, or the original string if invalid.
func canonicalize(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}

	if semver.IsValid(tag) {
		return tag
	}

	if semver.IsValid("v" + tag) {
		return "v" + tag
	}

	return tag
}

// Parts returns major, minor, and patch from a tag.
//
// Parameters:
//   - tag: Version tag, with or without a v prefix.
//
// Returns:
//   - string: Major component, or empty.
//   - string: Minor component, or empty.
//   - string: Patch component without prerelease suffix, or empty.
func Parts(tag string) (string, string, string) {
	canonical := canonicalize(tag)
	if !semver.IsValid(canonical) {
		return "", "", ""
	}

	trimmed := strings.TrimPrefix(semver.Canonical(canonical), "v")
	parts := strings.SplitN(trimmed, ".", partCount)

	major, minor, patch := "", "", ""
	if len(parts) > 0 {
		major = parts[0]
	}

	if len(parts) > 1 {
		minor = parts[1]
	}

	if len(parts) > patchIndex {
		patch, _, _ = strings.Cut(parts[patchIndex], "-")
	}

	return major, minor, patch
}

// Baseline returns tag, trimmed, when it is a semver tag.
//
// Pre-releases count as semver tags. Non-semver stamps such as nightly are not
// a tag baseline.
//
// Parameters:
//   - tag: Last observed tag or image version.
//
// Returns:
//   - string: The trimmed tag. It is empty when tag is not semver.
func Baseline(tag string) string {
	tag = strings.TrimSpace(tag)
	if !semver.IsValid(canonicalize(tag)) {
		return ""
	}

	return tag
}

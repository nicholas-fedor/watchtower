package control

import "github.com/nicholas-fedor/watchtower/testing/e2e/store"

// SkipCompleted adapts store.CompletedIDs to the scheduler resume set.
type SkipCompleted map[string]store.CaseStatus

// Has reports whether the case already finished.
//
// Parameters:
//   - caseID: Case identifier.
//
// Returns:
//   - bool: True when the ID is in the completed set.
func (s SkipCompleted) Has(caseID string) bool {
	_, ok := s[caseID]

	return ok
}

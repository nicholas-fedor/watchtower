package engine

// UncoveredFlags returns RegisterAll flag names missing from Model() coverage.
//
// Returns:
//   - []string: Uncovered long flag names, empty when coverage is complete.
func UncoveredFlags() []string {
	covered := CoveredFlags()
	missing := make([]string, 0)

	for _, name := range FlagNames() {
		if _, ok := covered[name]; !ok {
			missing = append(missing, name)
		}
	}

	return missing
}

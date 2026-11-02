package format

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

var testFuzzLog = func() *zerolog.Logger {
	l := zerolog.Nop()

	return &l
}()

// FuzzNew fuzzes New to ensure robust parsing of template strings.
func FuzzNew(f *testing.F) {
	// Add seeded inputs with valid templates, malformed syntax, and edge cases
	f.Add("{{.}}")
	f.Add("{{range .}}{{.Message}}{{end}}")
	f.Add("porcelain.v1.summary-no-log")
	f.Add("{{ intentionalSyntaxError")
	f.Add("")
	f.Add(strings.Repeat("a", 1000))

	f.Fuzz(func(_ *testing.T, tplString string) {
		//nolint:godox
		// TODO: Remove legacy mode fuzzing when legacy notification types are removed.
		_, _ = New(testFuzzLog, tplString, true)
		_, _ = New(testFuzzLog, tplString, false)
	})
}

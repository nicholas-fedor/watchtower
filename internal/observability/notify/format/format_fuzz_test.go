package format

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
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

// FuzzNotificationText checks the bound on untrusted notification text.
//
// The result is valid UTF-8 of at most 1,024 characters. It equals the
// printable line of the text when that fits. Otherwise it keeps the line's
// first 511 and last 512 characters around an ellipsis. Shortening it again
// changes nothing.
//
// The seed corpus covers:
//   - short text
//   - text at and just over the limit
//   - long text of mixed character widths
//   - long text with escape sequences and line breaks
func FuzzNotificationText(f *testing.F) {
	for _, seed := range []string{
		"pull access denied",
		strings.Repeat("x", 1024),
		strings.Repeat("x", 1025),
		strings.Repeat("\u00e9\u4e16", 700),
		strings.Repeat("a\u00e9\u4e16\U0001f600", 300),
		strings.Repeat("\x1b[31mline\n", 300),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, text string) {
		got := notificationText(text)

		if !utf8.ValidString(got) || utf8.RuneCountInString(got) > 1024 {
			t.Fatalf("notificationText(%q) = %q, not valid text of at most 1,024 characters", text, got)
		}

		// Slicing the line's characters is an independent way to find the
		// expected cut.
		line := []rune(logging.PrintableLine(text))
		if len(line) <= 1024 {
			if got != string(line) {
				t.Fatalf("notificationText(%q) = %q, want the printable line %q", text, got, string(line))
			}
		} else if want := string(line[:511]) + "\u2026" + string(line[len(line)-512:]); got != want {
			t.Fatalf("notificationText(%q) = %q, want %q", text, got, want)
		}

		if again := notificationText(got); again != got {
			t.Fatalf("shortening %q again gave %q", got, again)
		}
	})
}

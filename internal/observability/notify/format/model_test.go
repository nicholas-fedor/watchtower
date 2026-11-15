package format

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLevelString_WarnMapsToWarning ensures WarnLevel renders as the legacy
// template string "warning" so custom templates comparing .Level stay compatible.
func TestLevelString_WarnMapsToWarning(t *testing.T) {
	t.Parallel()

	require.Equal(t, "warning", LevelString(zerolog.WarnLevel))
	require.Equal(t, "info", LevelString(zerolog.InfoLevel))
	require.Equal(t, "error", LevelString(zerolog.ErrorLevel))

	// Non-legacy template path: compare .Level with the legacy "warning" string.
	const tpl = `{{ range .Entries }}{{ if eq .Level "warning" }}hit{{ end }}{{ end }}`

	data := Data{
		Entries: []*Entry{
			{Level: LevelString(zerolog.WarnLevel), Message: "caution"},
		},
	}
	require.Equal(t, "hit", getTemplatedResult(t, tpl, false, data))
}

// Test_notificationText verifies that untrusted text is cleaned and that text
// longer than 1,024 characters keeps its first 511 and last 512 characters,
// counted as characters rather than bytes.
func Test_notificationText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "short text unchanged", text: "pull access denied", want: "pull access denied"},
		{name: "cleaned", text: "a\x1b[31mb\nc", want: "ab | c"},
		{name: "exactly at the limit unchanged", text: strings.Repeat("x", 1024), want: strings.Repeat("x", 1024)},
		{
			name: "start and end kept",
			text: "S" + strings.Repeat("x", 3000) + "E",
			want: "S" + strings.Repeat("x", 510) + "\u2026" + strings.Repeat("x", 511) + "E",
		},
		{
			name: "one character over the limit",
			text: strings.Repeat("x", 1025),
			want: strings.Repeat("x", 511) + "\u2026" + strings.Repeat("x", 512),
		},
		{
			name: "mixed widths cut on characters",
			text: strings.Repeat("a\u00e9\u4e16\U0001f600", 400),
			want: strings.Repeat("a\u00e9\u4e16\U0001f600", 127) + "a\u00e9\u4e16" + "\u2026" + strings.Repeat("a\u00e9\u4e16\U0001f600", 128),
		},
		{
			name: "multi-byte characters counted as characters",
			text: strings.Repeat("\u00e9", 1025),
			want: strings.Repeat("\u00e9", 511) + "\u2026" + strings.Repeat("\u00e9", 512),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, notificationText(tt.text))
		})
	}
}

// TestTemplate_Render_longText verifies that a registry error carrying a long
// HTML page reaches a notification shortened, with the start that names the
// failure and the end of the page.
func TestTemplate_Render_longText(t *testing.T) {
	t.Parallel()

	page := "toomanyrequests: <html>" + strings.Repeat("<p>padding</p>", 400) + "</html>"

	data := Data{
		Entries: []*Entry{{
			Message: "Could not check container for updates",
			Data:    map[string]any{"error": page},
			Level:   LevelString(zerolog.WarnLevel),
		}},
	}

	output := getTemplatedResult(t, `{{range .Entries}}{{index .Data "error"}}{{end}}`, false, data)

	assert.Equal(t, page[:511]+"\u2026"+page[len(page)-512:], output)
}

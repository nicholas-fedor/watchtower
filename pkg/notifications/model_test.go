package notifications

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// TestLevelToString_WarnMapsToWarning ensures WarnLevel renders as the legacy
// template string "warning" so custom templates comparing .Level stay compatible.
func TestLevelToString_WarnMapsToWarning(t *testing.T) {
	t.Parallel()

	require.Equal(t, "warning", levelToString(zerolog.WarnLevel))
	require.Equal(t, "info", levelToString(zerolog.InfoLevel))
	require.Equal(t, "error", levelToString(zerolog.ErrorLevel))

	// Non-legacy template path: compare .Level with the legacy "warning" string.
	const tpl = `{{ range .Entries }}{{ if eq .Level "warning" }}hit{{ end }}{{ end }}`

	data := notificationData{
		Entries: []*notificationEntry{
			{Level: levelToString(zerolog.WarnLevel), Message: "caution"},
		},
	}
	require.Equal(t, "hit", getTemplatedResult(t, tpl, false, data))
}

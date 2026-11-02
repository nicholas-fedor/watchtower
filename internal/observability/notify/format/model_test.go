package format

import (
	"testing"

	"github.com/rs/zerolog"
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

package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToJSON(t *testing.T) {
	t.Parallel()

	got := toJSON(map[string]any{"title": "Title"})
	assert.Contains(t, got, `"title": "Title"`)
}

func TestFormatRFC1123(t *testing.T) {
	t.Parallel()

	tests := []struct {
		give string
		want string
	}{
		{give: "2026-01-02T03:04:05Z", want: "Fri, 02 Jan 2026 03:04:05 UTC"},
		{give: "not-a-timestamp", want: "not-a-timestamp"},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, formatRFC1123(tt.give))
		})
	}
}

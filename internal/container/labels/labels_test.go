package labels

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNormalizeScope verifies that an empty scope becomes "none" and that any
// other scope, "none" included, is kept.
func TestNormalizeScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		scope string
		want  string
	}{
		{name: "empty", scope: "", want: NoScope},
		{name: "none", scope: "none", want: NoScope},
		{name: "named", scope: "prod", want: "prod"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, NormalizeScope(tt.scope))
		})
	}
}

package spec

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegister_RangeDeclarations verifies that registration rejects a range
// on a setting that is not an integer, and an integer default outside its
// declared range, so a mistaken spec fails when it is registered instead of
// silently doing nothing or failing every load.
func TestRegister_RangeDeclarations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    FlagSpec
		wantErr error
	}{
		{
			name: "integer default within its range",
			spec: FlagSpec{Name: "interval", Kind: KindInt, Default: 86400, Min: 1},
		},
		{
			name:    "minimum on a duration setting",
			spec:    FlagSpec{Name: "stop-timeout", Kind: KindDuration, Default: time.Second, Min: 1},
			wantErr: errRangeOnNonInteger,
		},
		{
			name:    "maximum on a string setting",
			spec:    FlagSpec{Name: "host", Kind: KindString, Default: "", Max: 10},
			wantErr: errRangeOnNonInteger,
		},
		{
			name:    "integer default below its minimum",
			spec:    FlagSpec{Name: "interval", Kind: KindInt, Default: 0, Min: 1},
			wantErr: errDefaultOutOfRange,
		},
		{
			name:    "integer default above its maximum",
			spec:    FlagSpec{Name: "port", Kind: KindInt, Default: 70000, Max: 65535},
			wantErr: errDefaultOutOfRange,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := register(pflag.NewFlagSet("test", pflag.ContinueOnError), []FlagSpec{tt.spec})
			if tt.wantErr == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tt.wantErr)
			assert.ErrorContains(t, err, tt.spec.Name)
		})
	}
}

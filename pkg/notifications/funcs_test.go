package notifications

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatDiskSpace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "int64 gigabytes", value: int64(10_000_000_000), want: "10 GB"},
		{name: "int megabytes", value: 2_000_000, want: "2 MB"},
		{name: "float64 gigabytes", value: float64(8_000_000_000), want: "8 GB"},
		{name: "json number", value: json.Number("40000000000"), want: "40 GB"},
		{name: "zero", value: int64(0), want: "0 B"},
		{name: "nil", value: nil, want: "unknown"},
		{name: "string", value: "10GB", want: "unknown"},
		{name: "uint64 overflow", value: uint64(math.MaxInt64) + 1, want: "unknown"},
		{name: "float64 nan", value: math.NaN(), want: "unknown"},
		{name: "float64 max int64", value: float64(1 << 63), want: "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, formatDiskSpace(tc.value))
		})
	}
}

package units

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDiskSpace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{name: "empty string", input: "", want: 0},
		{name: "zero string", input: "0", want: 0},
		{name: "whitespace only", input: "   ", want: 0},
		{name: "bare bytes", input: "1024", want: 1024},
		{name: "explicit bytes", input: "1024B", want: 1024},
		{name: "lowercase b", input: "1024b", want: 1024},
		{name: "kilobyte short", input: "1K", want: 1000},
		{name: "kilobyte", input: "1KB", want: 1000},
		{name: "megabyte", input: "2MB", want: 2_000_000},
		{name: "megabyte short", input: "2M", want: 2_000_000},
		{name: "gigabyte", input: "40GB", want: 40_000_000_000},
		{name: "gigabyte short", input: "40G", want: 40_000_000_000},
		{name: "terabyte", input: "1TB", want: 1_000_000_000_000},
		{name: "petabyte", input: "1PB", want: 1_000_000_000_000_000},
		{name: "kibibyte", input: "1KiB", want: 1024},
		{name: "mebibyte", input: "1MiB", want: 1024 * 1024},
		{name: "gibibyte", input: "1GiB", want: 1024 * 1024 * 1024},
		{name: "tebibyte", input: "1TiB", want: 1024 * 1024 * 1024 * 1024},
		{name: "pebibyte", input: "1PiB", want: 1024 * 1024 * 1024 * 1024 * 1024},
		{name: "decimal gigabytes", input: "1.5GB", want: 1_500_000_000},
		{name: "exact thousandth of a kilobyte", input: "0.001KB", want: 1},
		{name: "max int64 bytes", input: "9223372036854775807", want: 9223372036854775807},
		{name: "overflow int64 bytes", input: "9223372036854775808", wantErr: true},
		{name: "overflow pebibytes", input: "8192PiB", wantErr: true},
		{name: "lowercase unit", input: "10mb", want: 10_000_000},
		{name: "mixed case unit", input: "10Mb", want: 10_000_000},
		{name: "space before unit", input: "40 GB", want: 40_000_000_000},
		{name: "leading space", input: "  8GiB", want: 8 * 1024 * 1024 * 1024},
		{name: "negative", input: "-1GB", wantErr: true},
		{name: "invalid number", input: "abc", wantErr: true},
		{name: "invalid unit", input: "5x", wantErr: true},
		{name: "percent not accepted", input: "80%", wantErr: true},
		{name: "empty number", input: "GB", wantErr: true},
		{name: "decimal binary unit", input: "0.5KiB", want: 512},
		{name: "leading decimal point", input: ".5KB", want: 500},
		{name: "trailing decimal point", input: "5.KB", want: 5000},
		{name: "exponent", input: "1.5e3GB", wantErr: true},
		{name: "exponent without decimal point", input: "1e3", wantErr: true},
		{name: "hexadecimal float", input: "0x1.8p1KB", wantErr: true},
		{name: "fraction", input: "3/2GB", wantErr: true},
		{name: "two decimal points", input: "1.2.3GB", wantErr: true},
		{name: "lone decimal point", input: ".GB", wantErr: true},
		{name: "plus sign", input: "+15GB", wantErr: true},
		{name: "plus sign on a decimal", input: "+1.5GB", wantErr: true},
		{name: "digit separator", input: "1_000", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseDiskSpace(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				assert.Zero(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFormatDiskSpace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{name: "zero", bytes: 0, want: "0 B"},
		{name: "bytes", bytes: 512, want: "512 B"},
		{name: "999 bytes", bytes: 999, want: "999 B"},
		{name: "exact kilobyte", bytes: 1000, want: "1 KB"},
		{name: "fractional kilobyte", bytes: 1536, want: "1.54 KB"},
		{name: "exact megabyte", bytes: 2_000_000, want: "2 MB"},
		{name: "exact gigabyte", bytes: 10_000_000_000, want: "10 GB"},
		{name: "warning gigabytes", bytes: 8_000_000_000, want: "8 GB"},
		{name: "decimal gigabyte", bytes: 1_500_000_000, want: "1.5 GB"},
		{name: "exact terabyte", bytes: 1_000_000_000_000, want: "1 TB"},
		{name: "exact petabyte", bytes: 1_000_000_000_000_000, want: "1 PB"},
		{name: "negative gigabyte", bytes: -10_000_000_000, want: "-10 GB"},
		{name: "min int64", bytes: math.MinInt64, want: "-9223.37 PB"},
		{name: "rounds up to the next unit", bytes: 999_999, want: "1 MB"},
		{name: "largest value kept in kilobytes", bytes: 999_994, want: "999.99 KB"},
		{name: "megabytes round up to a gigabyte", bytes: 999_999_999, want: "1 GB"},
		{name: "gigabytes round up to a terabyte", bytes: 999_999_999_999, want: "1 TB"},
		{name: "terabytes round up to a petabyte", bytes: 999_999_999_999_999, want: "1 PB"},
		{name: "negative value rounds up too", bytes: -999_999, want: "-1 MB"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, FormatDiskSpace(tc.bytes))
		})
	}
}

// TestParseDiskSpacePercent verifies that a percentage number is digits with
// at most one decimal point, and that NaN, infinities, signs, and exponents are
// rejected.
func TestParseDiskSpacePercent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    float64
		wantErr bool
	}{
		{input: "80", want: 80},
		{input: "62.5", want: 62.5},
		{input: " 100 ", want: 100},
		{input: "0", want: 0},
		{input: "NaN", wantErr: true},
		{input: "Inf", wantErr: true},
		{input: "8e1", wantErr: true},
		{input: "+80", wantErr: true},
		{input: "-5", wantErr: true},
		{input: "0x50p0", wantErr: true},
		{input: "1.2.3", wantErr: true},
		{input: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			got, err := ParseDiskSpacePercent(tc.input)
			if tc.wantErr {
				require.ErrorIs(t, err, errInvalidDiskSpaceNumber)

				return
			}

			require.NoError(t, err)
			assert.InDelta(t, tc.want, got, 0)
		})
	}
}

package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAPIEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []string
		want    []string
		wantErr error
	}{
		{
			name:  "empty",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty strings filtered",
			input: []string{"", " ", "  "},
			want:  nil,
		},
		{
			name:  "single",
			input: []string{"metrics"},
			want:  []string{EndpointMetrics},
		},
		{
			name:  "multiple pre-split",
			input: []string{"Health", "UPDATE", "metrics"},
			want:  []string{EndpointHealth, EndpointUpdate, EndpointMetrics},
		},
		{
			name:  "comma-separated single element",
			input: []string{"health,update,metrics"},
			want:  []string{EndpointHealth, EndpointUpdate, EndpointMetrics},
		},
		{
			name:  "space-separated single element",
			input: []string{"health update metrics"},
			want:  []string{EndpointHealth, EndpointUpdate, EndpointMetrics},
		},
		{
			name:  "mixed separators in one element",
			input: []string{"health, update metrics"},
			want:  []string{EndpointHealth, EndpointUpdate, EndpointMetrics},
		},
		{
			name:  "newline-separated single element",
			input: []string{"health\nmetrics\n"},
			want:  []string{EndpointHealth, EndpointMetrics},
		},
		{
			name:  "CRLF and tab separators",
			input: []string{"health\r\n\tupdate,\r\nmetrics"},
			want:  []string{EndpointHealth, EndpointUpdate, EndpointMetrics},
		},
		{
			name:  "non-ASCII whitespace separator",
			input: []string{"health\u00a0metrics"},
			want:  []string{EndpointHealth, EndpointMetrics},
		},
		{
			name:  "duplicates ignored",
			input: []string{"metrics", "metrics", "health"},
			want:  []string{EndpointHealth, EndpointMetrics},
		},
		{
			name:  "all alone",
			input: []string{"all"},
			want:  allEndpointNames,
		},
		{
			name:  "all with spaces",
			input: []string{" ALL "},
			want:  allEndpointNames,
		},
		{
			name:  "all repeated",
			input: []string{"all,all", "ALL"},
			want:  allEndpointNames,
		},
		{
			name:    "all with other names",
			input:   []string{"all", "metrics"},
			wantErr: errAllMustBeAlone,
		},
		{
			name:    "all repeated with another name",
			input:   []string{"all", "all", "metrics"},
			wantErr: errAllMustBeAlone,
		},
		{
			name:    "unknown",
			input:   []string{"metrics", "nope"},
			wantErr: errUnknownEndpoint,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseAPIEndpoints(tt.input)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)

			if tt.want == nil {
				assert.True(t, got.Empty())

				return
			}

			for _, name := range tt.want {
				assert.True(t, got.Contains(name), "expected %s", name)
			}

			assert.Len(t, got, len(tt.want))
		})
	}
}

func TestEndpointsFromLegacyMainFlags(t *testing.T) {
	t.Parallel()

	endpointMap := parseLegacyOptions(true, true, false)
	assert.True(t, endpointMap.Contains(EndpointUpdate))
	assert.True(t, endpointMap.Contains(EndpointMetrics))
	assert.False(t, endpointMap.Contains(EndpointContainers))
	assert.False(t, endpointMap.Contains(EndpointHealth))
}

func TestFormatEndpoints(t *testing.T) {
	t.Parallel()

	assert.Empty(t, formatEndpoints(nil))
	assert.Empty(t, formatEndpoints(EnabledEndpoints{}))

	endpointMap := EnabledEndpoints{
		EndpointMetrics: {},
		EndpointUpdate:  {},
		EndpointHealth:  {},
	}
	// Order follows allEndpointNames.
	assert.Equal(t, "health,update,metrics", formatEndpoints(endpointMap))
}

func TestResolveEndpoints(t *testing.T) {
	t.Parallel()

	t.Run("endpoints only", func(t *testing.T) {
		t.Parallel()

		endpointMap, err := ResolveEndpoints([]string{"health", "metrics"}, false, false, false)
		require.NoError(t, err)
		assert.True(t, endpointMap.Contains(EndpointHealth))
		assert.True(t, endpointMap.Contains(EndpointMetrics))
		assert.False(t, endpointMap.Contains(EndpointUpdate))
	})

	t.Run("all", func(t *testing.T) {
		t.Parallel()

		endpointMap, err := ResolveEndpoints([]string{"all"}, false, false, false)
		require.NoError(t, err)
		assert.Len(t, endpointMap, len(allEndpointNames))
	})

	t.Run("legacy only", func(t *testing.T) {
		t.Parallel()

		endpointMap, err := ResolveEndpoints(nil, true, false, true)
		require.NoError(t, err)
		assert.True(t, endpointMap.Contains(EndpointUpdate))
		assert.True(t, endpointMap.Contains(EndpointContainers))
		assert.False(t, endpointMap.Contains(EndpointMetrics))
	})

	t.Run("neither", func(t *testing.T) {
		t.Parallel()

		endpointMap, err := ResolveEndpoints(nil, false, false, false)
		require.NoError(t, err)
		assert.True(t, endpointMap.Empty())
	})

	t.Run("union allowlist and legacy with dedupe", func(t *testing.T) {
		t.Parallel()

		// metrics from allowlist + update from legacy → both; metrics not duplicated.
		endpointMap, err := ResolveEndpoints([]string{"metrics", "health"}, true, true, false)
		require.NoError(t, err)
		assert.True(t, endpointMap.Contains(EndpointMetrics))
		assert.True(t, endpointMap.Contains(EndpointHealth))
		assert.True(t, endpointMap.Contains(EndpointUpdate))
		assert.False(t, endpointMap.Contains(EndpointContainers))
		assert.Len(t, endpointMap, 3)
	})

	t.Run("invalid endpoints", func(t *testing.T) {
		t.Parallel()

		_, err := ResolveEndpoints([]string{"bogus"}, false, false, false)
		require.ErrorIs(t, err, errUnknownEndpoint)
	})
}

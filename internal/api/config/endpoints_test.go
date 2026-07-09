package config

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
			name:  "duplicates ignored",
			input: []string{"metrics", "metrics", "health"},
			want:  []string{EndpointHealth, EndpointMetrics},
		},
		{
			name:  "all alone",
			input: []string{"all"},
			want:  AllEndpointNames,
		},
		{
			name:  "all with spaces",
			input: []string{" ALL "},
			want:  AllEndpointNames,
		},
		{
			name:    "all with other names",
			input:   []string{"all", "metrics"},
			wantErr: ErrAllMustBeAlone,
		},
		{
			name:    "unknown",
			input:   []string{"metrics", "nope"},
			wantErr: ErrUnknownEndpoint,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseAPIEndpoints(tt.input)
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
				assert.True(t, got.Has(name), "expected %s", name)
			}

			assert.Len(t, got, len(tt.want))
		})
	}
}

func TestEndpointsFromLegacyMainFlags(t *testing.T) {
	t.Parallel()

	set := EndpointsFromLegacyMainFlags(true, true, false)
	assert.True(t, set.Has(EndpointUpdate))
	assert.True(t, set.Has(EndpointMetrics))
	assert.False(t, set.Has(EndpointContainers))
	assert.False(t, set.Has(EndpointHealth))
}

func TestFormatEndpoints(t *testing.T) {
	t.Parallel()

	assert.Empty(t, FormatEndpoints(nil))
	assert.Empty(t, FormatEndpoints(EndpointSet{}))

	set := EndpointSet{
		EndpointMetrics: {},
		EndpointUpdate:  {},
		EndpointHealth:  {},
	}
	// Order follows AllEndpointNames.
	assert.Equal(t, "health,update,metrics", FormatEndpoints(set))
}

func TestResolveEndpoints(t *testing.T) {
	t.Parallel()

	t.Run("endpoints only", func(t *testing.T) {
		t.Parallel()

		set, err := ResolveEndpoints([]string{"health", "metrics"}, false, false, false)
		require.NoError(t, err)
		assert.True(t, set.Has(EndpointHealth))
		assert.True(t, set.Has(EndpointMetrics))
		assert.False(t, set.Has(EndpointUpdate))
	})

	t.Run("all", func(t *testing.T) {
		t.Parallel()

		set, err := ResolveEndpoints([]string{"all"}, false, false, false)
		require.NoError(t, err)
		assert.Len(t, set, len(AllEndpointNames))
	})

	t.Run("legacy only", func(t *testing.T) {
		t.Parallel()

		set, err := ResolveEndpoints(nil, true, false, true)
		require.NoError(t, err)
		assert.True(t, set.Has(EndpointUpdate))
		assert.True(t, set.Has(EndpointContainers))
		assert.False(t, set.Has(EndpointMetrics))
	})

	t.Run("neither", func(t *testing.T) {
		t.Parallel()

		set, err := ResolveEndpoints(nil, false, false, false)
		require.NoError(t, err)
		assert.True(t, set.Empty())
	})

	t.Run("union allowlist and legacy with dedupe", func(t *testing.T) {
		t.Parallel()

		// metrics from allowlist + update from legacy → both; metrics not duplicated.
		set, err := ResolveEndpoints([]string{"metrics", "health"}, true, true, false)
		require.NoError(t, err)
		assert.True(t, set.Has(EndpointMetrics))
		assert.True(t, set.Has(EndpointHealth))
		assert.True(t, set.Has(EndpointUpdate))
		assert.False(t, set.Has(EndpointContainers))
		assert.Len(t, set, 3)
	})

	t.Run("invalid endpoints", func(t *testing.T) {
		t.Parallel()

		_, err := ResolveEndpoints([]string{"bogus"}, false, false, false)
		require.ErrorIs(t, err, ErrUnknownEndpoint)
	})
}

func TestApplyEndpoints(t *testing.T) {
	t.Parallel()

	set, err := ParseAPIEndpoints([]string{"update", "events", "swagger"})
	require.NoError(t, err)

	var opts Options
	ApplyEndpoints(&opts, set)

	assert.True(t, opts.EnableUpdateAPI)
	assert.True(t, opts.EnableEventsAPI)
	assert.True(t, opts.EnableSwaggerAPI)
	assert.False(t, opts.EnableMetricsAPI)
	assert.False(t, opts.EnableHealthAPI)
}

func TestApplyEndpointsToBools(t *testing.T) {
	t.Parallel()

	set := allEndpoints()
	h, u, m, c, ch, hi, im, cfg, ev, sw := ApplyEndpointsToBools(set)
	assert.True(t, h && u && m && c && ch && hi && im && cfg && ev && sw)

	var empty Options
	ApplyEndpoints(&empty, EndpointSet{})
	assert.False(t, empty.EnableHealthAPI)
	assert.False(t, empty.EnableUpdateAPI)
	assert.False(t, empty.EnableMetricsAPI)
}

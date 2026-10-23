package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/settings/config/api"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestPopulateRouteFlags(t *testing.T) {
	t.Parallel()

	endpointMap, err := api.ResolveEndpoints([]string{"all"}, false, false, false)
	require.NoError(t, err)

	var cfg types.RunConfig
	setEndpointConfig(endpointMap, &cfg)
	assert.True(t, cfg.EnableHealthAPI)
	assert.True(t, cfg.EnableUpdateAPI)
	assert.True(t, cfg.EnableMetricsAPI)
	assert.True(t, cfg.EnableContainersAPI)
	assert.True(t, cfg.EnableCheckAPI)
	assert.True(t, cfg.EnableHistoryAPI)
	assert.True(t, cfg.EnableImagesAPI)
	assert.True(t, cfg.EnableConfigAPI)
	assert.True(t, cfg.EnableEventsAPI)
	assert.True(t, cfg.EnableSwaggerAPI)

	var empty types.RunConfig
	setEndpointConfig(api.EnabledEndpoints{}, &empty)
	assert.False(t, empty.EnableHealthAPI)
	assert.False(t, empty.EnableUpdateAPI)
	assert.False(t, empty.EnableMetricsAPI)
}

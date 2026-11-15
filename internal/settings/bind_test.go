package settings

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// TestBindAll_unregisteredFlag verifies that BindAll reports a spec whose flag
// is not registered on the flag set.
func TestBindAll_unregisteredFlag(t *testing.T) {
	t.Parallel()

	err := BindAll(viper.New(), pflag.NewFlagSet("watchtower", pflag.ContinueOnError), AllSpecs())
	require.ErrorIs(t, err, errFlagNotRegistered)
}

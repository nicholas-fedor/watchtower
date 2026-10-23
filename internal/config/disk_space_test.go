package config

import (
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/internal/logging"
)

func TestLoad_DiskSpaceThresholds(t *testing.T) {
	t.Run("both absolute", func(t *testing.T) {
		cfg := newLoadedCommand(t, map[string]string{
			"WATCHTOWER_DISK_SPACE_MAX":  "40GB",
			"WATCHTOWER_DISK_SPACE_WARN": "30GB",
		})
		assert.Equal(t, int64(40_000_000_000), cfg.Update.DiskSpaceMaxBytes)
		assert.Equal(t, int64(30_000_000_000), cfg.Update.DiskSpaceWarnBytes)
		assert.Equal(t, int64(40_000_000_000), cfg.UpdateParams(RunOverrides{}).DiskSpaceMax)
		assert.Equal(t, int64(30_000_000_000), cfg.UpdateParams(RunOverrides{}).DiskSpaceWarn)
	})

	t.Run("warn percent of max", func(t *testing.T) {
		cfg := newLoadedCommand(t, map[string]string{
			"WATCHTOWER_DISK_SPACE_MAX":  "40GB",
			"WATCHTOWER_DISK_SPACE_WARN": "80%",
		})
		assert.Equal(t, int64(40_000_000_000), cfg.Update.DiskSpaceMaxBytes)
		assert.Equal(t, int64(32_000_000_000), cfg.Update.DiskSpaceWarnBytes)
	})

	t.Run("warn only", func(t *testing.T) {
		cfg := newLoadedCommand(t, map[string]string{
			"WATCHTOWER_DISK_SPACE_WARN": "20GiB",
		})
		assert.Equal(t, int64(0), cfg.Update.DiskSpaceMaxBytes)
		assert.Equal(t, int64(20*1024*1024*1024), cfg.Update.DiskSpaceWarnBytes)
	})

	t.Run("max only", func(t *testing.T) {
		cfg := newLoadedCommand(t, map[string]string{
			"WATCHTOWER_DISK_SPACE_MAX": "10GiB",
		})
		assert.Equal(t, int64(10*1024*1024*1024), cfg.Update.DiskSpaceMaxBytes)
		assert.Equal(t, int64(0), cfg.Update.DiskSpaceWarnBytes)
	})

	t.Run("unset", func(t *testing.T) {
		cfg := newLoadedCommand(t, nil)
		assert.Equal(t, int64(0), cfg.Update.DiskSpaceMaxBytes)
		assert.Equal(t, int64(0), cfg.Update.DiskSpaceWarnBytes)
	})
}

func TestLoad_DiskSpaceValidation(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want error
	}{
		{
			name: "percent warn without max",
			env:  map[string]string{"WATCHTOWER_DISK_SPACE_WARN": "80%"},
			want: errDiskSpacePercentWithoutMax,
		},
		{
			name: "percent max",
			env:  map[string]string{"WATCHTOWER_DISK_SPACE_MAX": "80%"},
			want: errDiskSpaceMaxPercent,
		},
		{
			name: "warn not below max",
			env: map[string]string{
				"WATCHTOWER_DISK_SPACE_MAX":  "10GB",
				"WATCHTOWER_DISK_SPACE_WARN": "10GB",
			},
			want: errDiskSpaceWarnNotBelowMax,
		},
		{
			name: "percent out of range",
			env: map[string]string{
				"WATCHTOWER_DISK_SPACE_MAX":  "10GB",
				"WATCHTOWER_DISK_SPACE_WARN": "0%",
			},
			want: errDiskSpacePercentOutOfRange,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadDiskSpaceEnv(t, tc.env)
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

// TestLoad_DiskSpaceWarnPercentFormat verifies that a warning percentage must
// be digits with at most one decimal point, so values such as NaN, which would
// pass the range check, and exponents are rejected.
func TestLoad_DiskSpaceWarnPercentFormat(t *testing.T) {
	for _, pct := range []string{"NaN%", "Inf%", "8e1%", "+80%", "0x50p0%", "80.5.1%"} {
		t.Run(pct, func(t *testing.T) {
			_, err := loadDiskSpaceEnv(t, map[string]string{
				"WATCHTOWER_DISK_SPACE_MAX":  "10GB",
				"WATCHTOWER_DISK_SPACE_WARN": pct,
			})
			require.ErrorContains(t, err, "invalid percentage")
		})
	}

	cfg := newLoadedCommand(t, map[string]string{
		"WATCHTOWER_DISK_SPACE_MAX":  "10GB",
		"WATCHTOWER_DISK_SPACE_WARN": "62.5%",
	})
	assert.Equal(t, int64(6_250_000_000), cfg.Update.DiskSpaceWarnBytes)
}

// loadDiskSpaceEnv loads the configuration from a command registered with
// every flag and the given environment.
//
// Parameters:
//   - t: Test that owns the environment.
//   - env: Environment variables to set.
//
// Returns:
//   - Config: Loaded configuration.
//   - error: The error Load returned.
func loadDiskSpaceEnv(t *testing.T, env map[string]string) (Config, error) {
	t.Helper()

	for k, v := range env {
		t.Setenv(k, v)
	}

	cmd := &cobra.Command{Use: "watchtower"}

	flags.RegisterAll(cmd)
	require.NoError(t, cmd.ParseFlags(nil))

	flagSet := cmd.PersistentFlags()
	require.NoError(t, flags.ApplyEnvToFlags(flagSet, flags.AllSpecs()))

	log := logging.New(io.Discard, logging.InfoLevel)
	flags.ProcessFlagAliases(log, flagSet)
	flags.GetSecretsFromFiles(log, cmd)

	return Load(newTestLogger(t), cmd, nil)
}

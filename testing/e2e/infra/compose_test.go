package infra

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("WATCHTOWER_E2E_DATABASE_URL", "")
	t.Setenv("WATCHTOWER_E2E_LOKI_URL", "")
	t.Setenv("WATCHTOWER_E2E_LISTEN", "")
	t.Setenv("WATCHTOWER_E2E_TOKEN", "")

	env := FromEnv()
	require.Equal(t, DefaultDatabaseURL, env.DatabaseURL)
	require.Equal(t, DefaultLokiURL, env.LokiURL)
	require.Equal(t, DefaultListen, env.Listen)
	require.Empty(t, env.Token)
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("WATCHTOWER_E2E_DATABASE_URL", "postgres://x")
	t.Setenv("WATCHTOWER_E2E_LOKI_URL", "http://loki")
	t.Setenv("WATCHTOWER_E2E_LISTEN", "127.0.0.1:9")
	t.Setenv("WATCHTOWER_E2E_TOKEN", "secret")

	env := FromEnv()
	require.Equal(t, "postgres://x", env.DatabaseURL)
	require.Equal(t, "http://loki", env.LokiURL)
	require.Equal(t, "127.0.0.1:9", env.Listen)
	require.Equal(t, "secret", env.Token)
}

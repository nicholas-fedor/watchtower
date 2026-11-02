package upgrade

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
)

// Test_writeEnvFile verifies that a file that is not fully written is removed,
// because it holds credential-bearing URLs.
func Test_writeEnvFile(t *testing.T) {
	t.Parallel()

	errSync := errors.New("sync failed")

	tests := []struct {
		name string
		// syncFile replaces (*os.File).Sync.
		syncFile func(*os.File) error
		wantErr  error
	}{
		{
			name:     "sync fails",
			syncFile: func(*os.File) error { return errSync },
			wantErr:  errSyncTempFile,
		},
		{
			// Closing the file during the sync makes the final close fail.
			name:     "close fails",
			syncFile: (*os.File).Close,
			wantErr:  errCloseTempFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			line := "WATCHTOWER_NOTIFICATION_URL=discord://token@webhookid"

			path, err := writeEnvFile(new(zerolog.Nop()), dir, line, tt.syncFile)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, path)

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Empty(t, entries, "a partly written file is removed")
		})
	}
}

// TestRemoveEnvFile verifies that the environment file is removed and the
// removal is reported. A file that cannot be removed is reported as a warning,
// since the command has already handed the file to the user.
func TestRemoveEnvFile(t *testing.T) {
	t.Parallel()

	t.Run("removed", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "watchtower-notif-urls-test")
		require.NoError(t, os.WriteFile(path, []byte("WATCHTOWER_NOTIFICATION_URL=\n"), 0o600))

		log, buf := logcapture.NewJSON(zerolog.InfoLevel)
		RemoveEnvFile(log, path)

		assert.NoFileExists(t, path)
		assert.Equal(t, []string{"Environment file removed"}, logcapture.Messages(t, buf.String(), "info"))
	})

	t.Run("cannot be removed", func(t *testing.T) {
		t.Parallel()

		log, buf := logcapture.NewJSON(zerolog.InfoLevel)
		RemoveEnvFile(log, filepath.Join(t.TempDir(), "missing"))

		assert.Equal(t, []string{"Failed to remove temporary file"}, logcapture.Messages(t, buf.String(), "warn"))
	})
}

// TestWriteEnvFile_URLs verifies that the configured URLs are written on one
// line, separated by spaces. An unknown legacy notification type fails before
// any file is written.
func TestWriteEnvFile_URLs(t *testing.T) {
	t.Parallel()

	t.Run("configured URLs", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		cfg := notifyConfig.Notify{URLs: []string{"logger://", "generic://example.com/hook"}}

		path, err := WriteEnvFile(new(zerolog.Nop()), dir, cfg)
		require.NoError(t, err)
		assert.Equal(t, dir, filepath.Dir(path))

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "WATCHTOWER_NOTIFICATION_URL=logger:// generic://example.com/hook\n", string(data))
	})

	t.Run("unknown legacy type", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		cfg := notifyConfig.Notify{LegacyTypes: []string{"carrier-pigeon"}}

		path, err := WriteEnvFile(new(zerolog.Nop()), dir, cfg)
		require.ErrorContains(t, err, "build notification URLs")
		assert.Empty(t, path)

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})
}

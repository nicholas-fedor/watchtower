package upgrade

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/observability/notify"
	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
)

// Errors for writing the environment file.
var (
	// errCreateTempFile indicates a failure to create a temporary file for notification URLs.
	errCreateTempFile = errors.New("failed to create output file")
	// errWriteTempFile indicates a failure to write notification URLs to the temporary file.
	errWriteTempFile = errors.New("failed to write to output file")
	// errSyncTempFile indicates a failure to sync the temporary file to disk.
	errSyncTempFile = errors.New("failed to sync output file")
	// errCloseTempFile indicates a failure to close the temporary file after writing it.
	errCloseTempFile = errors.New("failed to close output file")
)

// WriteEnvFile writes the notification URLs from cfg, including those converted
// from legacy notification types, to a new temporary file in dir. The file holds
// one WATCHTOWER_NOTIFICATION_URL line with the URLs separated by spaces.
//
// Parameters:
//   - log: Logger for the notification types found, the file path and failures.
//   - dir: Directory in which to create the file.
//   - cfg: Resolved notification settings.
//
// Returns:
//   - string: Path of the written and closed file.
//   - error: Non-nil if the URLs cannot be built or the file cannot be written.
func WriteEnvFile(log *zerolog.Logger, dir string, cfg notifyConfig.Notify) (string, error) {
	urls, err := notify.BuildURLs(log, cfg)
	if err != nil {
		return "", fmt.Errorf("build notification URLs: %w", err)
	}

	// Log the identified notification types (e.g., "email, slack") to inform the user of what configurations are being upgraded.
	log.Info().
		Str("notifiers", strings.Join(cfg.LegacyTypes, ", ")).
		Msg("Found notification config(s)")

	// Shoutrrr reads several URLs from one variable, separated by spaces.
	line := "WATCHTOWER_NOTIFICATION_URL=" + strings.Join(urls, " ")

	return writeEnvFile(log, dir, line, (*os.File).Sync)
}

// RemoveEnvFile removes the environment file and logs the outcome. A failure is
// logged as a warning, since the file was already handed to the user.
//
// Parameters:
//   - log: Logger for the outcome.
//   - path: Path of the environment file.
func RemoveEnvFile(log *zerolog.Logger, path string) {
	if removeFile(log, path) {
		log.Info().
			Str("file", path).
			Msg("Environment file removed")
	}
}

// writeEnvFile writes the environment line to a new temporary file in dir.
//
// The file holds credential-bearing notification URLs, so a file that cannot be written, synced, and closed is
// removed before the error is returned.
//
// Parameters:
//   - log: Logger for the file path and failures.
//   - dir: Directory in which to create the file.
//   - line: Environment line to write, without the trailing newline.
//   - syncFile: Flushes the file to disk. Production passes (*os.File).Sync.
//
// Returns:
//   - string: Path of the written and closed file.
//   - error: errCreateTempFile, errWriteTempFile, errSyncTempFile, or errCloseTempFile wrapping the cause.
func writeEnvFile(
	log *zerolog.Logger,
	dir, line string,
	syncFile func(*os.File) error,
) (string, error) {
	outFile, err := os.CreateTemp(dir, "watchtower-notif-urls-*")
	if err != nil {
		log.Debug().Err(err).Msg("Temporary file creation failed")

		return "", fmt.Errorf("%w: %w", errCreateTempFile, err)
	}

	log.Info().
		Str("file", outFile.Name()).
		Msg("Writing notification URLs")

	_, err = fmt.Fprintln(outFile, line)
	if err != nil {
		err = fmt.Errorf("%w: %w", errWriteTempFile, err)
	} else {
		err = syncFile(outFile)
		if err != nil {
			err = fmt.Errorf("%w: %w", errSyncTempFile, err)
		}
	}

	// Close the file even after a failure, because Windows does not allow removing an open file.
	closeErr := outFile.Close()
	if err == nil && closeErr != nil {
		err = fmt.Errorf("%w: %w", errCloseTempFile, closeErr)
	}

	if err == nil {
		return outFile.Name(), nil
	}

	log.Debug().
		Err(err).
		Str("file", outFile.Name()).
		Msg("Failed to write temporary file")

	removeFile(log, outFile.Name())

	return "", err
}

// removeFile removes the file at path and reports whether it did. It logs a
// warning when it cannot.
//
// Parameters:
//   - log: Logger for the failure.
//   - path: Path of the file to remove.
//
// Returns:
//   - bool: True if the file was removed.
func removeFile(log *zerolog.Logger, path string) bool {
	err := os.Remove(path)
	if err != nil {
		log.Warn().
			Err(err).
			Str("file", path).
			Msg("Failed to remove temporary file")

		return false
	}

	return true
}

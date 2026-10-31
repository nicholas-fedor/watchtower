package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/testutil/golden"
)

// Constants for the notify-upgrade tests.
const (
	// notifyUpgradeFilePattern matches the environment file notify-upgrade writes.
	notifyUpgradeFilePattern = "watchtower-notif-urls-*"
	// notifyUpgradeExitHelperEnv marks the test binary re-executed to run a
	// failing notify-upgrade. Empty means the parent process.
	notifyUpgradeExitHelperEnv = "WATCHTOWER_CMD_NOTIFY_UPGRADE_EXIT_HELPER"
)

// newNotifyUpgradeCommand returns a notify-upgrade command under a new root
// command, configured only by args.
func newNotifyUpgradeCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()

	sub := &cobra.Command{Use: "notify-upgrade"}
	newTestRootCommand(t, args...).AddCommand(sub)

	return sub
}

// notifyUpgradeFiles returns the environment files in the working directory.
func notifyUpgradeFiles(t *testing.T) []string {
	t.Helper()

	files, err := filepath.Glob(notifyUpgradeFilePattern)
	require.NoError(t, err)

	return files
}

// notifyUpgradeRun is the outcome of running notify-upgrade until its wait
// times out.
type notifyUpgradeRun struct {
	// content is the environment file's content while the command waited.
	content string
	// err is the error the command returned.
	err error
	// remaining lists the environment files left after the command returned.
	remaining []string
}

// runNotifyUpgradeUntilTimeout runs notify-upgrade in a synctest bubble from a
// new working directory and returns its outcome. The run happens in a subtest
// so the working directory is restored before the caller compares goldens.
func runNotifyUpgradeUntilTimeout(t *testing.T, args ...string) notifyUpgradeRun {
	t.Helper()

	var run notifyUpgradeRun

	t.Run("run", func(t *testing.T) {
		t.Chdir(t.TempDir())

		cmd := newNotifyUpgradeCommand(t, args...)

		synctest.Test(t, func(t *testing.T) {
			done := make(chan error, 1)

			go func() {
				_, err := runNotifyUpgradeE(cmd, nil, new(zerolog.Nop()))
				done <- err
			}()

			// The command writes the file and then waits for a signal or timeout.
			synctest.Wait()

			select {
			case err := <-done:
				require.FailNow(t, "notify-upgrade returned before waiting", "error: %v", err)
			default:
			}

			files := notifyUpgradeFiles(t)
			require.Len(t, files, 1, "the command writes one environment file while it waits")

			data, err := os.ReadFile(files[0])
			require.NoError(t, err)

			run.content = string(data)

			time.Sleep(cleanupTimeout)

			run.err = <-done
		})

		run.remaining = notifyUpgradeFiles(t)
	})

	return run
}

// TestNotifyUpgrade_LegacyNotifiers snapshots the environment file written for
// every legacy notifier type alongside an existing notification URL, and
// verifies the file is removed when the command's wait times out. The snapshot
// records how each legacy setting is converted, including the Slack channel and
// the Gotify TLS setting.
func TestNotifyUpgrade_LegacyNotifiers(t *testing.T) {
	run := runNotifyUpgradeUntilTimeout(t,
		"--notifications", "email,slack,msteams,gotify",
		"--notification-url", "discord://token@webhookid",
		"--notification-email-from", "watchtower@example.com",
		"--notification-email-to", "admin@example.com",
		"--notification-email-server", "smtp.example.com",
		"--notification-email-server-port", "587",
		"--notification-email-server-user", "mailer",
		"--notification-email-server-password", "mail-password",
		"--notification-email-subjecttag", "prod",
		"--notification-slack-hook-url", "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX",
		"--notification-slack-identifier", "watchtower-bot",
		"--notification-slack-icon-emoji", ":whale:",
		"--notification-slack-channel", "#ops",
		"--notification-msteams-hook", "https://example.webhook.office.com/webhookb2/aaaa@bbbb/IncomingWebhook/cccc/dddd",
		"--notification-gotify-url", "https://gotify.example.com",
		"--notification-gotify-token", "gotify-token",
		"--notification-gotify-tls-skip-verify",
	)
	require.NoError(t, run.err)

	golden.Assert(t, "notify-upgrade-env", []byte(run.content))
	assert.Empty(t, run.remaining, "the environment file is removed after the wait")
}

// TestNotifyUpgrade_WithoutLegacyNotifiers records the environment file when
// no legacy notifier is configured.
func TestNotifyUpgrade_WithoutLegacyNotifiers(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "nothing configured",
			want: "WATCHTOWER_NOTIFICATION_URL=\n",
		},
		{
			name: "existing notification URLs only",
			args: []string{
				"--notification-url", "discord://token@webhookid",
				"--notification-url", "logger://",
			},
			want: "WATCHTOWER_NOTIFICATION_URL=discord://token@webhookid logger://\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := runNotifyUpgradeUntilTimeout(t, tt.args...)
			require.NoError(t, run.err)
			assert.Equal(t, tt.want, run.content)
			assert.Empty(t, run.remaining)
		})
	}
}

// TestNotifyUpgrade_Failures verifies the errors returned before the command
// waits, and that no environment file is left behind.
func TestNotifyUpgrade_Failures(t *testing.T) {
	tests := []struct {
		name string
		args []string
		// readOnlyDir makes the working directory read-only so the
		// environment file cannot be created.
		readOnlyDir bool
		wantErr     error
		wantMessage string
	}{
		{
			name:        "invalid log format",
			args:        []string{"--log-format", "bogus"},
			wantMessage: "setup logging",
		},
		{
			name: "invalid legacy notifier settings",
			args: []string{
				"--notifications", "slack",
				"--notification-slack-hook-url", "https://hooks.slack.com/services/not-a-token",
			},
			wantMessage: "build notification URLs",
		},
		{
			name:        "working directory is not writable",
			readOnlyDir: true,
			wantErr:     errCreateTempFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)

			if tt.readOnlyDir {
				if runtime.GOOS == "windows" || os.Geteuid() == 0 {
					t.Skip("needs a directory the current user cannot write to")
				}

				require.NoError(t, os.Chmod(dir, 0o500))
				t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
			}

			cmd := newNotifyUpgradeCommand(t, tt.args...)

			_, err := runNotifyUpgradeE(cmd, nil, new(zerolog.Nop()))
			require.Error(t, err)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}

			assert.Contains(t, err.Error(), tt.wantMessage)
			assert.Empty(t, notifyUpgradeFiles(t))
		})
	}
}

// TestRunNotifyUpgrade_ExitsOnFailure verifies that a failed notify-upgrade
// ends the process with exit code 1, so scripts and container runtimes can
// tell that no environment file was produced. The command runs in a
// re-executed test binary because the failure ends the process.
func TestRunNotifyUpgrade_ExitsOnFailure(t *testing.T) {
	if os.Getenv(notifyUpgradeExitHelperEnv) != "" {
		t.Chdir(t.TempDir())

		proc := &process{log: logging.New(os.Stderr, logging.InfoLevel)}
		proc.exit(proc.runNotifyUpgrade(newNotifyUpgradeCommand(t,
			"--notifications", "slack",
			"--notification-slack-hook-url", "https://hooks.slack.com/services/not-a-token",
		), nil))

		// The failure must end the process. Reaching here means it did not.
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunNotifyUpgrade_ExitsOnFailure$", "-test.v=false")

	cmd.Env = append(os.Environ(), notifyUpgradeExitHelperEnv+"=1")

	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError

	require.ErrorAs(t, err, &exitErr, "output:\n%s", out)
	assert.Equal(t, 1, exitErr.ExitCode(), "output:\n%s", out)
	assert.Contains(t, string(out), "Notification upgrade failed")
	assert.Contains(t, string(out), "build notification URLs")
}

// TestWriteNotificationEnvFile verifies that the environment file is kept only
// when it is fully written, because it holds credential-bearing URLs.
func TestWriteNotificationEnvFile(t *testing.T) {
	t.Parallel()

	errSync := errors.New("sync failed")

	tests := []struct {
		name string
		// syncFile replaces (*os.File).Sync.
		syncFile func(*os.File) error
		wantErr  error
	}{
		{
			name:     "written",
			syncFile: (*os.File).Sync,
		},
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

			path, err := writeNotificationEnvFile(new(zerolog.Nop()), dir, line, tt.syncFile)

			entries, readErr := os.ReadDir(dir)
			require.NoError(t, readErr)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, path)
				assert.Empty(t, entries, "a partly written file is removed")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, dir, filepath.Dir(path))

			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, line+"\n", string(data))
		})
	}
}

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// tailBuffer is an [io.Writer] that keeps the last bytes written to it.
type tailBuffer struct {
	data    []byte // The kept bytes, at most limit of them.
	limit   int    // How many of the last bytes to keep, at least 4 to hold any character.
	dropped bool   // Whether earlier bytes were dropped.
}

// maxExecOutput is how much of the end of a command's output is kept (2 KiB).
//
// The end is where a failure is usually explained. The output reaches logs and
// notifications. The limit keeps a noisy or hostile command from flooding them.
const maxExecOutput = 2 << 10

// droppedOutputMarker starts output whose beginning was dropped.
const droppedOutputMarker = "…"

// Parts of terminal escape sequences.
const (
	// escape is ESC. It starts 7-bit terminal escape sequences.
	escape = '\x1b'
	// c1Offset is what a C1 control adds to its 7-bit introducer, as CSI is '[' plus 0x40.
	c1Offset = 0x40
	// lastIntermediateByte is the highest intermediate byte of an escape sequence.
	lastIntermediateByte = 0x2f
	// lastParameterByte is the highest parameter byte of a CSI sequence.
	lastParameterByte = 0x3f
	// stringTerminator is ESC \. It ends OSC, DCS, SOS, PM and APC strings.
	stringTerminator = "\x1b\\"
	// c1StringTerminator is the 8-bit form of the string terminator.
	c1StringTerminator = "\u009c"
)

// Errors for executing commands in containers.
var (
	// errCreateExecFailed indicates a failure to create an exec instance in a container.
	errCreateExecFailed = errors.New("failed to create exec instance")
	// errStartExecFailed indicates a failure to start an exec instance in a container.
	errStartExecFailed = errors.New("failed to start exec instance")
	// errReadExecOutputFailed indicates a failure to read output from an exec instance.
	errReadExecOutputFailed = errors.New("failed to read exec output")
	// errInspectExecFailed indicates a failure to inspect an exec instance's status.
	errInspectExecFailed = errors.New("failed to inspect exec instance")
	// errCommandFailed indicates a command executed in a container failed with a non-zero exit code.
	errCommandFailed = errors.New("command execution failed")
)

// ExecuteCommand runs a command inside a container and evaluates its result.
//
// The command's output is written inside the container and is untrusted text.
// When the command exits with a code other than 0 or 75, the error ends with
// the end of that output, made printable. The timeout bounds the whole run of
// the command, from its start to its exit.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Container to execute command in.
//   - command: Command to execute.
//   - timeout: Minutes to wait before timeout (0 for no timeout).
//   - uid: UID to run command as (-1 to use container default).
//   - gid: GID to run command as (-1 to use container default).
//
// Returns:
//   - bool: True if updates should be skipped, false otherwise.
//   - error: Non-nil if execution fails, nil on success.
func (c *client) ExecuteCommand(
	ctx context.Context,
	container types.Container,
	command string,
	timeout int,
	uid int,
	gid int,
) (bool, error) {
	clogVal := c.logger().With().
		Str("container_id", string(container.ID())).
		Logger()
	clog := &clogVal

	// Generate JSON metadata for the container.
	metadataJSON, err := generateContainerMetadata(container)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to generate container metadata")

		return false, err
	}

	// Set User if UID or GID are specified (non-zero).
	var user string

	switch {
	case uid > 0 && gid > 0:
		user = fmt.Sprintf("%d:%d", uid, gid)
	case uid > 0:
		user = strconv.Itoa(uid)
	case gid > 0:
		user = fmt.Sprintf(":%d", gid)
	}

	if user != "" {
		clog.Debug().
			Str("user", user).
			Msg("Setting exec user")
	}

	// Set up exec configuration with command and metadata.
	clog.Debug().
		Str("command", command).
		Msg("Creating exec instance")
	execConfig := dockerClient.ExecCreateOptions{
		TTY:          true,
		Cmd:          []string{"sh", "-c", command},
		Env:          []string{"WT_CONTAINER=" + metadataJSON},
		User:         user,
		AttachStdout: true,
		AttachStderr: true,
	}

	// Create the exec instance using the parent context.
	exec, err := c.api.ExecCreate(
		ctx,
		string(container.ID()),
		execConfig,
	)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to create exec instance")

		return false, fmt.Errorf("%w: %w", errCreateExecFailed, err)
	}

	// The timeout bounds the attach, the output read and the exit wait.
	execCtx := ctx

	if timeout > 0 {
		var cancel context.CancelFunc

		execCtx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Minute)
		defer cancel()
	}

	// ExecAttach starts the exec and streams its output.
	clog.Debug().
		Str("exec_id", exec.ID).
		Msg("Starting exec instance")

	attached, err := c.api.ExecAttach(execCtx, exec.ID, dockerClient.ExecAttachOptions{TTY: true})
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to start exec instance")

		return false, fmt.Errorf("%w: %w", errStartExecFailed, err)
	}

	output, err := readExecOutput(execCtx, attached)
	if err != nil {
		clog.Warn().
			Err(err).
			Msg("Failed to capture command output")
	}

	// Wait for completion and evaluate result. The error already says whether
	// the inspect failed, the wait ended, or the command failed.
	skipUpdate, err := c.waitForExec(execCtx, exec.ID, output)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Command did not complete successfully")

		return true, err
	}

	clog.Debug().
		Str("command", command).
		Str("output", output).
		Bool("skip_update", skipUpdate).
		Msg("Executed command")

	return skipUpdate, nil
}

// Write keeps the end of written and of what was written before.
//
// Parameters:
//   - written: Bytes to write.
//
// Returns:
//   - int: Always len(written).
//   - error: Always nil.
func (b *tailBuffer) Write(written []byte) (int, error) {
	b.data = append(b.data, written...)

	// The kept bytes start on a whole character. A character split by the trim
	// is dropped.
	if len(b.data) > b.limit {
		b.data = append(b.data[:0], lastCharacters(b.data, b.limit)...)
		b.dropped = true
	}

	return len(written), nil
}

// readExecOutput reads the output of a started exec instance and closes its stream.
//
// The whole output is read. A command never blocks on a full stream. Its end is
// kept, made printable and put on one line. The user information of URLs, such
// as https://user:token@host, is replaced by xxxxx. Query values are not. A
// leading "…" marks a dropped beginning.
//
// Parameters:
//   - ctx: Context bounding the read.
//   - response: The attach result that streams the exec's output.
//
// Returns:
//   - string: The kept output as one printable line.
//   - error: Non-nil if reading fails or ctx ends first, nil on success.
func readExecOutput(ctx context.Context, response dockerClient.ExecAttachResult) (string, error) {
	defer response.Close()

	kept := tailBuffer{data: nil, limit: maxExecOutput, dropped: false}
	done := make(chan error, 1)

	// When ctx ends first, the deferred Close closes the stream. That ends a
	// read that is still blocked.
	go func() {
		_, err := io.Copy(&kept, response.Reader)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("%w: %w", errReadExecOutputFailed, err)
		}
	case <-ctx.Done():
		return "", fmt.Errorf("%w: %w", errReadExecOutputFailed, ctx.Err())
	}

	// URL user information is replaced after cleaning. An escape sequence then
	// cannot split a URL to hide it.
	output := oneLine(redact.Userinfo(printableOutput(string(kept.data)), ""))
	if kept.dropped {
		output = droppedOutputMarker + output
	}

	return output, nil
}

// waitForExec waits for an exec instance to complete, or for ctx to end.
//
// Parameters:
//   - ctx: Context bounding the wait, including the command's timeout.
//   - execID: ID of the exec instance.
//   - execOutput: Captured output for error reporting.
//
// Returns:
//   - bool: True if updates should be skipped (exit code 75), false otherwise.
//   - error: Non-nil if inspection fails, the command errors, or ctx ends first.
func (c *client) waitForExec(
	ctx context.Context,
	execID string,
	execOutput string,
) (bool, error) {
	const ExTempFail = 75

	clogVal := c.logger().With().
		Str("exec_id", execID).
		Logger()
	clog := &clogVal

	// Poll exec status until completion.
	for {
		execInspect, err := c.api.ExecInspect(ctx, execID, dockerClient.ExecInspectOptions{})
		if err != nil {
			clog.Debug().
				Err(err).
				Msg("Failed to inspect exec instance")

			return false, fmt.Errorf("%w: %w", errInspectExecFailed, err)
		}

		clog.Debug().
			Int("exit_code", execInspect.ExitCode).
			Bool("running", execInspect.Running).
			Msg("Checked exec status")

		if execInspect.Running {
			select {
			case <-time.After(1 * time.Second):
				continue
			case <-ctx.Done():
				return false, fmt.Errorf("exec canceled: %w", ctx.Err())
			}
		}

		// Handle specific exit codes.
		if execInspect.ExitCode == ExTempFail {
			return true, nil // Skip updates on temporary failure.
		}

		if execInspect.ExitCode > 0 {
			err := fmt.Errorf("%w with exit code %d", errCommandFailed, execInspect.ExitCode)
			if execOutput != "" {
				err = fmt.Errorf("%w: %s", err, execOutput)
			}

			clog.Debug().
				Err(err).
				Msg("Command execution failed")

			return false, err
		}

		break
	}

	return false, nil
}

// lastCharacters returns the end of text, at most limit bytes, starting on a whole character.
//
// Parameters:
//   - text: Bytes to shorten.
//   - limit: Most bytes to return.
//
// Returns:
//   - []byte: text when it fits within limit. Otherwise its last bytes without a partial first character.
func lastCharacters(text []byte, limit int) []byte {
	if len(text) <= limit {
		return text
	}

	start := len(text) - limit
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}

	return text[start:]
}

// printableOutput removes from command output what could change how a terminal or notification shows it.
//
// Output written inside a container is untrusted. Terminal escape sequences
// could retitle or reprogram a terminal that shows the text. Invisible format
// characters could hide text or make it read differently than it is. Removed:
//   - terminal escape sequences in 7-bit and 8-bit form, such as colours,
//     window titles and clipboard writes
//   - control characters other than newline and tab
//   - Unicode format characters, such as bidirectional controls, zero-width
//     characters and tag characters
//
// A lone carriage return becomes a newline. One before a newline is dropped.
// The Unicode line and paragraph separators also become newlines, since some
// services show them as line breaks. Invalid UTF-8 becomes U+FFFD.
//
// Parameters:
//   - raw: Output as the command wrote it.
//
// Returns:
//   - string: The printable text.
func printableOutput(raw string) string {
	var out strings.Builder

	out.Grow(len(raw))

	for index := 0; index < len(raw); {
		char, size := utf8.DecodeRuneInString(raw[index:])
		index += size

		switch {
		case char == '\r':
			if !strings.HasPrefix(raw[index:], "\n") {
				out.WriteByte('\n')
			}
		case unicode.In(char, unicode.Zl, unicode.Zp):
			out.WriteByte('\n')
		case char == escape || (char >= 0x80 && char <= 0x9f):
			index = escapeSequenceEnd(raw, index, char)
		case (unicode.IsControl(char) && char != '\n' && char != '\t') || unicode.Is(unicode.Cf, char):
			// Dropped.
		default:
			out.WriteRune(char)
		}
	}

	return out.String()
}

// oneLine joins the non-blank lines of text with " | ".
//
// Notifications put each log entry on a line of its own. Output kept on one
// line cannot start a line that reads like another Watchtower message.
//
// Parameters:
//   - text: Text that may span lines.
//
// Returns:
//   - string: The lines, each trimmed, joined on one line.
func oneLine(text string) string {
	var lines []string

	for line := range strings.Lines(text) {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}

	return strings.Join(lines, " | ")
}

// escapeSequenceEnd returns where the escape sequence that introducer starts ends in raw.
//
// Parameters:
//   - raw: Text that holds the sequence.
//   - index: Position just after the introducer.
//   - introducer: ESC or an 8-bit C1 control.
//
// Returns:
//   - int: Position just after the sequence. The end of raw when it is unterminated.
func escapeSequenceEnd(raw string, index int, introducer rune) int {
	// The sequence's kind is its 7-bit introducer, such as '[' for CSI. A C1
	// control is the 7-bit introducer plus 0x40.
	kind := introducer - c1Offset

	if introducer == escape {
		kind = escape

		if index < len(raw) && raw[index] >= '@' && raw[index] <= '_' {
			kind = rune(raw[index])
			index++
		}
	}

	// The highest parameter or intermediate byte. A final byte above it ends
	// the sequence.
	last := byte(lastIntermediateByte)

	switch kind {
	case '[':
		// CSI, such as a colour, also takes parameter bytes.
		last = lastParameterByte
	case ']', 'P', 'X', '^', '_':
		// OSC, DCS, SOS, PM and APC strings end with BEL, ESC \ or the 8-bit
		// string terminator.
		for index < len(raw) {
			switch {
			case raw[index] == '\a':
				return index + 1
			case strings.HasPrefix(raw[index:], stringTerminator):
				return index + len(stringTerminator)
			case strings.HasPrefix(raw[index:], c1StringTerminator):
				return index + len(c1StringTerminator)
			default:
				index++
			}
		}

		return index
	case escape:
		// Other escape sequences take intermediate bytes.
	default:
		// Other C1 controls stand alone.
		return index
	}

	for index < len(raw) && raw[index] >= 0x20 && raw[index] <= last {
		index++
	}

	if index < len(raw) && raw[index] > last && raw[index] <= 0x7e {
		index++
	}

	return index
}

// generateContainerMetadata creates a JSON-formatted string of container metadata.
//
// Parameters:
//   - container: Container object to extract metadata from.
//
// Returns:
//   - string: JSON string containing metadata (e.g., name, ID, image name, stop signal, labels).
//   - error: Non-nil if JSON marshaling fails, nil otherwise.
func generateContainerMetadata(container types.Container) (string, error) {
	// Filter Watchtower-specific labels to reduce JSON size
	labels := make(map[string]string)

	containerInfo := container.ContainerInfo()
	if containerInfo != nil &&
		containerInfo.Config != nil {
		for key, value := range containerInfo.Config.Labels {
			if strings.HasPrefix(key, "com.centurylinklabs.watchtower.") {
				labels[key] = value
			}
		}
	}

	metadata := struct {
		Name       string            `json:"name"`
		ID         string            `json:"id"`
		ImageName  string            `json:"image_name"`
		StopSignal string            `json:"stop_signal"`
		Labels     map[string]string `json:"labels"`
	}{
		Name:       container.Name(),
		ID:         string(container.ID()),
		ImageName:  container.ImageName(),
		StopSignal: container.StopSignal(),
		Labels:     labels,
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("failed to marshal container metadata: %w", err)
	}

	return string(metadataJSON), nil
}

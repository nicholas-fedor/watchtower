package client

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
)

// TestExecuteCommand_TimeoutBoundsOutputCapture verifies that a command's
// timeout also ends the read of its output, so a hook that never exits or closes
// its output stops at the timeout instead of blocking the update.
func TestExecuteCommand_TimeoutBoundsOutputCapture(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const execID = "hanging-exec-id"

		api := newDockerAPI(t)
		api.EXPECT().ExecCreate(mock.Anything, mockContainerID, mock.Anything).
			Return(dockerClient.ExecCreateResult{ID: execID}, nil).Once()
		// The attach starts the exec. Its output never arrives and the stream
		// never closes.
		api.EXPECT().ExecAttach(mock.Anything, execID, dockerClient.ExecAttachOptions{TTY: true}).
			Return(newExecOutput(t, nil), nil).Once()
		api.EXPECT().ExecInspect(mock.Anything, execID, dockerClient.ExecInspectOptions{}).
			Return(dockerClient.ExecInspectResult{ID: execID, Running: true}, nil).Once()

		start := time.Now()

		_, err := (&client{log: newTestLogger(t), api: api}).
			ExecuteCommand(t.Context(), modelfixture.Container(), "sleep infinity", 1, 0, 0)

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, time.Minute, time.Since(start))
	})
}

// TestTailBuffer verifies that the buffer keeps only the last bytes written to
// it and reports whether it dropped earlier ones.
func TestTailBuffer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		want        string
		chunks      []string
		limit       int
		wantDropped bool
	}{
		{name: "within the limit", chunks: []string{"ab", "c"}, limit: 4, want: "abc"},
		{name: "exactly the limit", chunks: []string{"ab", "cd"}, limit: 4, want: "abcd"},
		{
			name:        "trims more than once",
			chunks:      []string{"ab", "cdef", "g", "hijklmnop", "qrstu", "v"},
			limit:       4,
			want:        "stuv",
			wantDropped: true,
		},
		// "é" is two bytes. The last three bytes of "abcéde" start inside it.
		{name: "split character dropped", chunks: []string{"abcé", "de"}, limit: 3, want: "de", wantDropped: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buffer := tailBuffer{data: nil, limit: tt.limit, dropped: false}

			for _, chunk := range tt.chunks {
				written, err := buffer.Write([]byte(chunk))
				require.NoError(t, err)
				assert.Len(t, chunk, written)
			}

			assert.Equal(t, tt.want, string(buffer.data))
			assert.Equal(t, tt.wantDropped, buffer.dropped)
		})
	}
}

// TestOneLine verifies that text is put on one line with its non-blank lines
// trimmed and joined.
func TestOneLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "empty", text: "", want: ""},
		{name: "one line", text: "  done  ", want: "done"},
		{name: "several lines", text: "a\nb\n", want: "a | b"},
		{name: "blank lines dropped", text: "\n a \n\n\t\nb", want: "a | b"},
		{name: "tab inside a line kept", text: "key\tvalue", want: "key\tvalue"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, oneLine(tt.text))
		})
	}
}

// TestPrintableOutput verifies that command output keeps its text and loses what could change how it shows.
func TestPrintableOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "plain text", raw: "héllo wörld", want: "héllo wörld"},
		{name: "newline and tab", raw: "a\tb\nc", want: "a\tb\nc"},
		{name: "carriage return before newline", raw: "a\r\nb", want: "a\nb"},
		{name: "lone carriage return", raw: "10%\r100%", want: "10%\n100%"},
		{name: "colour", raw: "\x1b[1;31mred\x1b[0m", want: "red"},
		{name: "cursor movement", raw: "\x1b[2K\x1b[1Gdone", want: "done"},
		{name: "window title ended by BEL", raw: "\x1b]0;title\atext", want: "text"},
		{name: "clipboard write ended by ST", raw: "\x1b]52;c;aGk=\x1b\\text", want: "text"},
		{name: "hyperlink", raw: "\x1b]8;;https://example.com\x1b\\click\x1b]8;;\x1b\\", want: "click"},
		{name: "device control string", raw: "\x1bPq#0\x1b\\after", want: "after"},
		{name: "8-bit control sequence", raw: "\u009b31mred", want: "red"},
		{name: "8-bit operating system command", raw: "\u009d0;title\u009cafter", want: "after"},
		{name: "character set selection", raw: "\x1b(Btext", want: "text"},
		{name: "two-character escape", raw: "\x1bcreset", want: "reset"},
		{name: "other C0 controls and DEL", raw: "a\x00b\ac\bd\x7fe", want: "abcde"},
		{name: "other C1 control", raw: "a\u0085b", want: "ab"},
		{
			name: "bidirectional controls",
			raw:  "abc" + string(rune(0x202e)) + "def" + string(rune(0x2066)) + "x" + string(rune(0x2069)),
			want: "abcdefx",
		},
		{name: "invalid UTF-8", raw: "a\xffb", want: "a\uFFFDb"},
		{name: "overlong-encoded escape", raw: "\xc0\x9b[31mred", want: "\uFFFD\uFFFD[31mred"},
		{name: "raw 8-bit control sequence byte", raw: "\x9b31mred", want: "\uFFFD31mred"},
		{name: "encoded surrogate half", raw: "a\xed\xa0\x80b", want: "a\uFFFD\uFFFD\uFFFDb"},
		{name: "zero-width space and joiner", raw: "pass" + string(rune(0x200b)) + "word" + string(rune(0x200d)), want: "password"},
		{name: "byte order mark and soft hyphen", raw: string(rune(0xfeff)) + "up" + string(rune(0xad)) + "date", want: "update"},
		{
			name: "tag characters",
			raw:  "ok" + string(rune(0xe0001)) + string(rune(0xe0068)) + string(rune(0xe0069)) + string(rune(0xe007f)),
			want: "ok",
		},
		{name: "unterminated string", raw: "a\x1b]0;never ends", want: "a"},
		{name: "trailing escape", raw: "abc\x1b", want: "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, printableOutput(tt.raw))
		})
	}
}

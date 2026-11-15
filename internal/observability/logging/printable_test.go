package logging

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPrintableLine verifies that text is put on one line with its non-blank
// lines trimmed and joined, and loses what could change how it shows.
func TestPrintableLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "empty", text: "", want: ""},
		{name: "one line", text: "  done  ", want: "done"},
		{name: "printable ASCII unchanged", text: "nginx:1.27 https://example.com/a?b=c", want: "nginx:1.27 https://example.com/a?b=c"},
		{name: "edge tabs trimmed", text: "\tdone\t", want: "done"},
		{name: "several lines", text: "a\nb\n", want: "a | b"},
		{name: "blank lines dropped", text: "\n a \n\n\t\nb", want: "a | b"},
		{name: "tab inside a line kept", text: "key\tvalue", want: "key\tvalue"},
		{name: "carriage returns end lines", text: "10%\r100%\r\ndone", want: "10% | 100% | done"},
		{name: "line separator ends a line", text: "a" + string(rune(0x2028)) + "b", want: "a | b"},
		{name: "escape sequence removed", text: "\x1b]0;title\a\x1b[31mred\x1b[0m", want: "red"},
		{name: "forged line kept on the line", text: "pull failed\nFound new image", want: "pull failed | Found new image"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, PrintableLine(tt.text))
		})
	}
}

// Test_printable verifies that text keeps its characters and line breaks and
// loses what could change how it shows.
func Test_printable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "plain text", raw: "héllo wörld", want: "héllo wörld"},
		{name: "newline and tab", raw: "a\tb\nc", want: "a\tb\nc"},
		{name: "carriage return before newline", raw: "a\r\nb", want: "a\n\nb"},
		{name: "lone carriage return", raw: "10%\r100%", want: "10%\n100%"},
		{
			name: "line and paragraph separators",
			raw:  "a" + string(rune(0x2028)) + "b" + string(rune(0x2029)) + "c",
			want: "a\nb\nc",
		},
		{name: "colour", raw: "\x1b[1;31mred\x1b[0m", want: "red"},
		{name: "cursor movement", raw: "\x1b[2K\x1b[1Gdone", want: "done"},
		{name: "window title ended by BEL", raw: "\x1b]0;title\atext", want: "text"},
		{name: "clipboard write ended by ST", raw: "\x1b]52;c;aGk=\x1b\\text", want: "text"},
		{name: "hyperlink", raw: "\x1b]8;;https://example.com\x1b\\click\x1b]8;;\x1b\\", want: "click"},
		{name: "device control string", raw: "\x1bPq#0\x1b\\after", want: "after"},
		{name: "start of string past BEL", raw: "\x1bXa\ab\x1b\\done", want: "done"},
		{name: "8-bit start of string past BEL", raw: "\u0098a\ab\u009cdone", want: "done"},
		{name: "application command past BEL", raw: "\x1b_a\ab\x1b\\done", want: "done"},
		{name: "8-bit window title ended by BEL", raw: "\u009d0;title\atext", want: "text"},
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
		{name: "invalid UTF-8", raw: "a\xffb", want: "a�b"},
		{name: "overlong-encoded escape", raw: "\xc0\x9b[31mred", want: "��[31mred"},
		{name: "raw 8-bit control sequence byte", raw: "\x9b31mred", want: "�31mred"},
		{name: "encoded surrogate half", raw: "a\xed\xa0\x80b", want: "a���b"},
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

			assert.Equal(t, tt.want, printable(tt.raw))
		})
	}
}

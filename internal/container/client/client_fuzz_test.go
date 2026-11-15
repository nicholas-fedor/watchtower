package client

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
)

// FuzzTailBuffer checks what the buffer keeps when output arrives in chunks.
//
// The kept bytes are an end of the output, at most the limit long. A drop is
// reported exactly when the output was longer than the limit. For valid UTF-8
// output, writing in chunks keeps the same bytes as keeping the end of the
// whole output at once. Invalid UTF-8 has no character boundaries to agree on.
//
// The limit is at least 4 bytes, the longest UTF-8 character, as the buffer
// requires.
//
// The seed corpus covers empty output, output within and past the limit, and
// multi-byte and invalid UTF-8. The regression seeds under testdata/fuzz are a
// run of stray continuation bytes and a 4-byte character written a byte at a
// time. The fuzzer picks the chunk sizes and the limit.
func FuzzTailBuffer(f *testing.F) {
	for _, seed := range []string{"", "abc", "abcdefghijklmnop", "ééééé", "a\xffbc\xc3"} {
		f.Add([]byte(seed), []byte{1, 2, 3}, uint8(4))
	}

	f.Fuzz(func(t *testing.T, output, chunkSizes []byte, limit uint8) {
		keep := int(limit%16) + utf8.UTFMax
		buffer := tailBuffer{data: nil, limit: keep, dropped: false}

		rest := output
		for index := 0; len(rest) > 0; index++ {
			size := len(rest)
			if len(chunkSizes) > 0 {
				size = min(int(chunkSizes[index%len(chunkSizes)])%8+1, len(rest))
			}

			written, err := buffer.Write(rest[:size])
			if err != nil || written != size {
				t.Fatalf("Write(%q) = %d, %v", rest[:size], written, err)
			}

			rest = rest[size:]
		}

		if len(buffer.data) > keep || !bytes.HasSuffix(output, buffer.data) {
			t.Fatalf("kept %q is not an end of %q within %d bytes", buffer.data, output, keep)
		}

		if want := lastCharacters(output, keep); utf8.Valid(output) && !bytes.Equal(buffer.data, want) {
			t.Fatalf("kept %q, want %q", buffer.data, want)
		}

		if buffer.dropped != (len(output) > keep) {
			t.Fatalf("dropped = %t for %d bytes and limit %d", buffer.dropped, len(output), keep)
		}
	})
}

// FuzzReadExecOutput checks the output a hook's stream turns into.
//
// The output is valid UTF-8 and printable on one line, whatever the command
// wrote, so making it printable again changes nothing.
//
// The seed corpus covers empty output, several lines, escape sequences,
// invalid UTF-8, a credentialed URL and output past the 2 KiB limit.
func FuzzReadExecOutput(f *testing.F) {
	for _, seed := range []string{
		"",
		"line one\nline two\r\n",
		"line one" + string(rune(0x2028)) + "line two",
		"\x1b]0;title\a\x1b[31mred\x1b[0m\r100%",
		"a\xffb\xc0\x9b",
		"clone https://bot:ghp_abc@github.com/org/app.git failed",
		strings.Repeat("x", 3000) + "\nend",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, raw []byte) {
		output, err := readExecOutput(t.Context(), newExecOutput(t, func(conn net.Conn) {
			_, _ = conn.Write(raw)
		}))
		if err != nil {
			t.Fatalf("readExecOutput: %v", err)
		}

		if again := logging.PrintableLine(output); again != output {
			t.Fatalf("output %q is not printable on one line, cleaning gives %q", output, again)
		}
	})
}

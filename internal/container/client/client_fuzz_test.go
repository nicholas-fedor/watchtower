package client

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// FuzzPrintableOutput checks the invariants of cleaned command output.
//
// The output is valid UTF-8. It has no control characters other than newline
// and tab. It has no Unicode format characters or line and paragraph
// separators. Cleaning it again changes nothing. Printable input comes back
// unchanged.
//
// The seed corpus covers plain text, ANSI colours, window titles, clipboard
// writes, hyperlinks, device control strings, 8-bit introducers, carriage
// returns, bidirectional controls, invalid UTF-8 and unterminated sequences.
func FuzzPrintableOutput(f *testing.F) {
	for _, seed := range []string{
		"plain text",
		"\x1b[1;31mred\x1b[0m",
		"\x1b]0;title\a",
		"\x1b]52;c;aGk=\x1b\\",
		"\x1b]8;;https://example.com\x1b\\click\x1b]8;;\x1b\\",
		"\x1bPq#0\x1b\\",
		"\u009b31m\u009d0;t\u009c",
		"a\r\nb\rc",
		"abc" + string(rune(0x202e)) + "def",
		"a\xffb",
		"\x1b]0;never ends",
		"\x1b",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		out := printableOutput(raw)

		if !utf8.ValidString(out) {
			t.Fatalf("output is not valid UTF-8: %q", out)
		}

		for _, char := range out {
			if needsCleaning(char) {
				t.Fatalf("output keeps %U: %q", char, out)
			}
		}

		if again := printableOutput(out); again != out {
			t.Fatalf("cleaning again changed %q to %q", out, again)
		}

		if utf8.ValidString(raw) && !strings.ContainsFunc(raw, needsCleaning) && out != raw {
			t.Fatalf("printable text changed from %q to %q", raw, out)
		}
	})
}

// FuzzPrintableOutput_RemovesSequences checks that an escape sequence inserted
// into printable text is removed without taking any of that text with it.
//
// The seed corpus covers empty and plain text and text with newlines, tabs and
// multi-byte characters. The fuzzer picks the sequence and where it goes.
func FuzzPrintableOutput_RemovesSequences(f *testing.F) {
	sequences := []string{
		"\x1b[31m",
		"\x1b[1;2;3H",
		"\x1b[?25l",
		"\x1b]0;title\a",
		"\x1b]52;c;aGk=\x1b\\",
		"\x1b]8;;https://example.com\x1b\\",
		"\x1bPq#0\x1b\\",
		"\x1b_payload\x1b\\",
		"\x1b(B",
		"\x1bc",
		"\x1b7",
		"\u009b31m",
		"\u009d0;title\u009c",
		"\u0085",
		"\u200b",
		"\ufeff",
		"\U000e0041",
	}

	for _, seed := range []string{"", "plain text", "a\tb\nc", "héllo wörld", "@[]^_"} {
		f.Add(seed, uint8(0), uint16(0))
	}

	f.Fuzz(func(t *testing.T, text string, which uint8, where uint16) {
		clean := printableOutput(text)
		sequence := sequences[int(which)%len(sequences)]

		// Insert at a character boundary of the clean text.
		boundaries := []int{0}

		for index := range clean {
			if index > 0 {
				boundaries = append(boundaries, index)
			}
		}

		boundaries = append(boundaries, len(clean))
		at := boundaries[int(where)%len(boundaries)]

		raw := clean[:at] + sequence + clean[at:]
		if out := printableOutput(raw); out != clean {
			t.Fatalf("inserting %q at %d gave %q, want %q", sequence, at, out, clean)
		}
	})
}

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
// The output is valid UTF-8 on one line, whatever the command wrote. It has no
// control characters other than tab. It has no Unicode format characters or
// line and paragraph separators.
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

		if !utf8.ValidString(output) {
			t.Fatalf("output is not valid UTF-8: %q", output)
		}

		for _, char := range output {
			if char == '\n' || needsCleaning(char) {
				t.Fatalf("output keeps %U: %q", char, output)
			}
		}
	})
}

// needsCleaning reports whether printableOutput changes char.
//
// Parameters:
//   - char: Character to check.
//
// Returns:
//   - bool: True for a Unicode format character, a line or paragraph separator,
//     or a control character other than newline and tab.
func needsCleaning(char rune) bool {
	return (unicode.IsControl(char) && char != '\n' && char != '\t') ||
		unicode.In(char, unicode.Cf, unicode.Zl, unicode.Zp)
}

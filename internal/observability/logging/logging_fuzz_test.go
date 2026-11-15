package logging

import (
	"bytes"
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/rs/zerolog"
)

// FuzzPrintable checks the invariants of printable text.
//
// The result is valid UTF-8. It has no control characters other than newline
// and tab. It has no Unicode format characters or line and paragraph
// separators. Cleaning it again changes nothing. Printable input comes back
// unchanged.
//
// The seed corpus covers plain text, ANSI colours, window titles, clipboard
// writes, hyperlinks, device control strings, 8-bit introducers, carriage
// returns, bidirectional controls, invalid UTF-8 and unterminated sequences.
func FuzzPrintable(f *testing.F) {
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
		out := printable(raw)

		if !utf8.ValidString(out) {
			t.Fatalf("output is not valid UTF-8: %q", out)
		}

		for _, char := range out {
			if needsCleaning(char) {
				t.Fatalf("output keeps %U: %q", char, out)
			}
		}

		if again := printable(out); again != out {
			t.Fatalf("cleaning again changed %q to %q", out, again)
		}

		if utf8.ValidString(raw) && !strings.ContainsFunc(raw, needsCleaning) && out != raw {
			t.Fatalf("printable text changed from %q to %q", raw, out)
		}
	})
}

// FuzzPrintable_removesSequences checks that an escape sequence inserted into
// printable text is removed without taking any of that text with it.
//
// The seed corpus covers empty and plain text and text with newlines, tabs and
// multi-byte characters. The fuzzer picks the sequence and where it goes.
func FuzzPrintable_removesSequences(f *testing.F) {
	sequences := []string{
		"\x1b[31m",
		"\x1b[1;2;3H",
		"\x1b[?25l",
		"\x1b]0;title\a",
		"\x1b]52;c;aGk=\x1b\\",
		"\x1b]8;;https://example.com\x1b\\",
		"\x1bPq#0\x1b\\",
		"\x1b_payload\x1b\\",
		"\x1bXa\ab\x1b\\",
		"\u0098a\ab\u009c",
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
		clean := printable(text)
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
		if out := printable(raw); out != clean {
			t.Fatalf("inserting %q at %d gave %q, want %q", sequence, at, out, clean)
		}
	})
}

// FuzzJSONWriter checks that the JSON writer escapes what zerolog leaves raw
// without changing any value.
//
// The writer takes zerolog's event in full. What it writes holds no character
// beyond ASCII that printable text never holds, and decodes to the same values
// as the event.
//
// The seed corpus covers plain and multi-byte text, C1 controls, bidirectional
// and zero-width characters, line separators, tag characters and invalid
// UTF-8.
func FuzzJSONWriter(f *testing.F) {
	for _, seed := range []string{
		"plain text",
		"héllo wörld",
		"\u009b31mred\u0085",
		"abc" + string(rune(0x202e)) + "def" + string(rune(0x200b)),
		"a" + string(rune(0x2028)) + "b" + string(rune(0x2029)),
		"ok" + string(rune(0xe0041)),
		"a\xffb\xc2",
		"\x1b]0;title\a",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, text string) {
		var raw, escaped bytes.Buffer

		log := zerolog.New(&raw)
		log.Warn().Str("error", text).Str(text, "value").Msg(text)

		n, err := JSONWriter(&escaped).Write(raw.Bytes())
		if err != nil || n != raw.Len() {
			t.Fatalf("Write(%q) = %d, %v, want %d, nil", raw.String(), n, err, raw.Len())
		}

		for _, char := range escaped.String() {
			if char > unicode.MaxASCII && needsCleaning(char) {
				t.Fatalf("output keeps %U: %q", char, escaped.String())
			}
		}

		var want, got map[string]any

		err = json.Unmarshal(raw.Bytes(), &want)
		if err != nil {
			t.Fatalf("zerolog wrote invalid JSON %q: %v", raw.String(), err)
		}

		err = json.Unmarshal(escaped.Bytes(), &got)
		if err != nil {
			t.Fatalf("output %q does not decode: %v", escaped.String(), err)
		}

		// Every value is a string, so the maps compare with ==.
		if !maps.Equal(got, want) {
			t.Fatalf("output decodes to %q, want %q", got, want)
		}
	})
}

// FuzzConsoleWriters checks that the logfmt and pretty formats write no
// character that changes how text shows.
//
// Each format logs the fuzzed text as the message, the time, an error list, a
// string field and a field name. Its line ends with a newline and holds no
// other control character, Unicode format character, or line or paragraph
// separator. Pretty output shows non-empty text without such a character
// unquoted.
//
// The seed corpus covers plain and multi-byte text, terminal escape sequences,
// C1 controls, bidirectional and zero-width characters, line breaks, tabs,
// DEL and invalid UTF-8.
func FuzzConsoleWriters(f *testing.F) {
	for _, seed := range []string{
		"plain text",
		"héllo wörld",
		"\x1b]0;title\a\x1b[31mred",
		"\u009b31mred\u0085",
		"abc" + string(rune(0x202e)) + "def" + string(rune(0x200b)),
		"a\nb\rc" + string(rune(0x2028)) + "d",
		"a\tb",
		"a\x7fb",
		"a\xffb",
	} {
		f.Add(seed)
	}

	// raw reports whether char may not appear in a line before its newline.
	raw := func(char rune) bool {
		return char == '\n' || char == '\t' || needsCleaning(char)
	}

	f.Fuzz(func(t *testing.T, text string) {
		lines := make(map[string]string)

		for _, format := range []struct {
			name   string
			writer zerolog.ConsoleWriter
		}{
			{name: "logfmt", writer: LogfmtWriter(nil)},
			{name: "pretty", writer: newPrettyWriter(true)},
		} {
			var out bytes.Buffer

			format.writer.Out = &out

			log := zerolog.New(format.writer)
			log.Warn().Str(zerolog.TimestampFieldName, text).Strs("error", []string{text}).Str("image", text).
				Str("key "+text, "value").Msg(text)

			line, ended := strings.CutSuffix(out.String(), "\n")
			if !ended {
				t.Fatalf("%s line does not end with a newline: %q", format.name, out.String())
			}

			if index := strings.IndexFunc(line, raw); index >= 0 {
				char, _ := utf8.DecodeRuneInString(line[index:])
				t.Fatalf("%s line keeps %U: %q", format.name, char, line)
			}

			lines[format.name] = line
		}

		if text != "" && utf8.ValidString(text) && !strings.ContainsFunc(text, raw) &&
			!strings.Contains(lines["pretty"], "WRN "+text+" error=") {
			t.Fatalf("pretty line quotes or changes %q: %q", text, lines["pretty"])
		}
	})
}

// FuzzPrintableLine checks the invariants of a printable line.
//
// The result is valid UTF-8 on one line. It has no control characters other
// than tab, and no Unicode format characters or line and paragraph separators.
// It neither starts nor ends with white space. Cleaning it again changes
// nothing. It equals the trimmed non-blank lines of the printable text joined
// with " | ", whether or not the text was printable ASCII to begin with.
//
// The seed corpus covers empty text, several lines with blank ones, carriage
// returns, escape sequences, invalid UTF-8 and surrounding white space.
func FuzzPrintableLine(f *testing.F) {
	for _, seed := range []string{
		"",
		"line one\nline two\r\n",
		"\n a \n\n\t\nb",
		"line one" + string(rune(0x2028)) + "line two",
		"\x1b]0;title\a\x1b[31mred\x1b[0m\r100%",
		"a\xffb\xc0\x9b",
		"  padded\t",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		out := PrintableLine(raw)

		if !utf8.ValidString(out) {
			t.Fatalf("output is not valid UTF-8: %q", out)
		}

		for _, char := range out {
			if char == '\n' || needsCleaning(char) {
				t.Fatalf("output keeps %U: %q", char, out)
			}
		}

		if strings.TrimSpace(out) != out {
			t.Fatalf("output has surrounding white space: %q", out)
		}

		if again := PrintableLine(out); again != out {
			t.Fatalf("cleaning again changed %q to %q", out, again)
		}

		var lines []string

		for line := range strings.Lines(printable(raw)) {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				lines = append(lines, trimmed)
			}
		}

		if want := strings.Join(lines, " | "); out != want {
			t.Fatalf("PrintableLine(%q) = %q, want %q", raw, out, want)
		}
	})
}

// needsCleaning reports whether char is one that printable text never holds.
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

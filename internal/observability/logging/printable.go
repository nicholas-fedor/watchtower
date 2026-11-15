package logging

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

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

// lineSeparator joins the lines of text that [PrintableLine] puts on one line.
const lineSeparator = " | "

// PrintableLine returns text as one line that cannot change how a terminal or
// notification shows it.
//
// Text from outside Watchtower is untrusted, such as command output, image
// annotations and error messages from registries. Terminal escape sequences
// could retitle or reprogram a terminal that shows the text. Invisible format
// characters could hide text or make it read differently than it is. A line
// break could start a line that reads like another Watchtower message.
// Removed:
//   - terminal escape sequences in 7-bit and 8-bit form, such as colours,
//     window titles and clipboard writes
//   - control characters other than tab
//   - Unicode format characters, such as bidirectional controls, zero-width
//     characters and tag characters
//
// Carriage returns and the Unicode line and paragraph separators end lines, as
// newlines do, since some services show them as line breaks. The non-blank
// lines are trimmed and joined with " | ". Invalid UTF-8 becomes U+FFFD.
// Cleaning the result again changes nothing.
//
// Parameters:
//   - text: Text as its source wrote it.
//
// Returns:
//   - string: The printable text on one line.
func PrintableLine(text string) string {
	// Most text, such as names, versions and URLs, is printable ASCII already.
	if isPrintableASCIILine(text) {
		return text
	}

	var lines []string

	for line := range strings.Lines(printable(text)) {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}

	return strings.Join(lines, lineSeparator)
}

// printable returns text without what [PrintableLine] removes, with each line
// break as a newline.
//
// Parameters:
//   - text: Text as its source wrote it.
//
// Returns:
//   - string: The printable text.
func printable(text string) string {
	var out strings.Builder

	out.Grow(len(text))

	for index := 0; index < len(text); {
		char, size := utf8.DecodeRuneInString(text[index:])
		index += size

		switch {
		case char == '\r' || unicode.In(char, unicode.Zl, unicode.Zp):
			out.WriteByte('\n')
		case char == escape || (char >= 0x80 && char <= 0x9f):
			index = escapeSequenceEnd(text, index, char)
		case changesDisplay(char) && char != '\n' && char != '\t':
			// Dropped.
		default:
			out.WriteRune(char)
		}
	}

	return out.String()
}

// isPrintableASCIILine reports whether [PrintableLine] returns text unchanged
// because it is printable ASCII on one line.
//
// Parameters:
//   - text: Text to check.
//
// Returns:
//   - bool: True when text holds only printable ASCII characters and tabs and
//     neither starts nor ends with a space or tab.
func isPrintableASCIILine(text string) bool {
	if text == "" {
		return true
	}

	if strings.ContainsRune(" \t", rune(text[0])) || strings.ContainsRune(" \t", rune(text[len(text)-1])) {
		return false
	}

	for index := range len(text) {
		if char := text[index]; (char < ' ' && char != '\t') || char > '~' {
			return false
		}
	}

	return true
}

// changesDisplay reports whether char can change how the text around it shows.
//
// Parameters:
//   - char: Character to check.
//
// Returns:
//   - bool: True for a control character, a Unicode format character, or a
//     line or paragraph separator.
func changesDisplay(char rune) bool {
	// No ASCII character is a format character or a separator, so the common
	// ASCII case needs only the control check.
	if char < utf8.RuneSelf {
		return unicode.IsControl(char)
	}

	return unicode.IsControl(char) || unicode.In(char, unicode.Cf, unicode.Zl, unicode.Zp)
}

// escapeSequenceEnd returns where the escape sequence that introducer starts ends in text.
//
// Parameters:
//   - text: Text that holds the sequence.
//   - index: Position just after the introducer.
//   - introducer: ESC or an 8-bit C1 control.
//
// Returns:
//   - int: Position just after the sequence. The end of text when it is unterminated.
func escapeSequenceEnd(text string, index int, introducer rune) int {
	// The sequence's kind is its 7-bit introducer, such as '[' for CSI. A C1
	// control is the 7-bit introducer plus 0x40.
	kind := introducer - c1Offset

	if introducer == escape {
		kind = escape

		if index < len(text) && text[index] >= '@' && text[index] <= '_' {
			kind = rune(text[index])
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
		// OSC, DCS, SOS, PM and APC strings end with ESC \ or the 8-bit string
		// terminator. Terminals also end an OSC string with BEL, as xterm does
		// for older applications, but no other string.
		for index < len(text) {
			switch {
			case text[index] == '\a' && kind == ']':
				return index + 1
			case strings.HasPrefix(text[index:], stringTerminator):
				return index + len(stringTerminator)
			case strings.HasPrefix(text[index:], c1StringTerminator):
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

	for index < len(text) && text[index] >= 0x20 && text[index] <= last {
		index++
	}

	if index < len(text) && text[index] > last && text[index] <= 0x7e {
		index++
	}

	return index
}

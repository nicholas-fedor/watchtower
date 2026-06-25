package api

import (
	"io"
	"net"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sirupsen/logrus"
)

// normalizeHost strips IPv6 brackets and zone identifiers for ParseIP.
func normalizeHost(host string) string {
	host = strings.TrimPrefix(host, "[")

	host = strings.TrimSuffix(host, "]")
	if i := strings.Index(host, "%"); i >= 0 {
		host = host[:i]
	}

	return host
}

// FuzzGetAPIAddr fuzzes the GetAPIAddr function which formats host:port strings.
// It tests that the function never panics and produces valid output for any
// combination of host and port strings, including IPv4, IPv6, hostnames,
// and edge cases like empty strings, special characters, and very long inputs.
func FuzzGetAPIAddr(f *testing.F) {
	f.Add("localhost", "8080")
	f.Add("127.0.0.1", "80")
	f.Add("::1", "443")
	f.Add("2001:db8::1", "9090")
	f.Add("", "")
	f.Add("myhost.example.com", "3000")
	f.Add("fe80::1%eth0", "8080")
	f.Add("[::1]", "8080")
	f.Add("192.168.1.1", "")
	f.Add("", "8080")
	f.Add("host with spaces", "8080")
	f.Add("host:with:colons", "8080")
	f.Add("a", "b")

	f.Fuzz(func(t *testing.T, host, port string) {
		result := GetAPIAddr(host, port)

		// Invariant 1: result must contain the port
		if port != "" && !strings.HasSuffix(result, ":"+port) {
			t.Errorf("result %q does not end with :%q", result, port)
		}

		// Invariant 2: result must not be empty if port is non-empty
		if port != "" && result == "" {
			t.Errorf("empty result for host=%q port=%q", host, port)
		}

		// Invariant 3: IPv6 addresses should be wrapped in brackets
		normalized := normalizeHost(host)
		if strings.Contains(host, ":") && net.ParseIP(normalized) != nil {
			if !strings.HasPrefix(result, "[") {
				t.Errorf("IPv6 address %q not wrapped in brackets: %q", host, result)
			}
		}

		// Invariant 4: IPv4 addresses should not have bracket wrapping added
		if !strings.Contains(host, ":") && net.ParseIP(host) != nil {
			expected := host + ":" + port
			if result != expected {
				t.Errorf("IPv4 address %q with port %q: got %q, want %q", host, port, result, expected)
			}
		}

		// Invariant 5: result should be valid UTF-8 if inputs are valid UTF-8
		if utf8.ValidString(host) && utf8.ValidString(port) && !utf8.ValidString(result) {
			t.Errorf("result is not valid UTF-8 for valid inputs: host=%q port=%q result=%q", host, port, result)
		}
	})
}

// FuzzLogrusWriterWrite fuzzes the logrusWriter.Write method which processes
// byte slices by stripping trailing newlines. It tests that the method never
// panics and returns the correct byte count for any input.
func FuzzLogrusWriterWrite(f *testing.F) {
	f.Add([]byte("hello world\n"))
	f.Add([]byte("hello world"))
	f.Add([]byte(""))
	f.Add([]byte("\n"))
	f.Add([]byte("\n\n\n"))
	f.Add([]byte("test message  \n"))
	f.Add([]byte("unicode: \xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e\n"))
	f.Add([]byte("null\x00byte\n"))
	f.Add([]byte("very long string " + strings.Repeat("x", 1000) + "\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		logger := logrus.New()
		logger.SetOutput(io.Discard)

		w := &logrusWriter{logger: logger}

		n, err := w.Write(data)

		if n != len(data) {
			t.Errorf("Write() returned %d, want %d", n, len(data))
		}

		if err != nil {
			t.Errorf("Write() returned unexpected error: %v", err)
		}
	})
}

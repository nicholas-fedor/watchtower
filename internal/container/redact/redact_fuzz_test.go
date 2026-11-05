package redact

import (
	"net/url"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
)

// FuzzRedactedRemote verifies that credential redaction never panics and
// replaces a parseable password with xxxxx. Any userinfo left in a remote that
// does not parse must be the redacted form.
func FuzzRedactedRemote(f *testing.F) {
	f.Add("https://user:secret@github.com/org/app.git#abc")
	f.Add("https://github.com/org/app.git")
	f.Add("://bad")
	f.Add("")
	f.Add("https://user@github.com/org/app.git")
	f.Add("https://x-access-token:ghp_xxx@github.com/org/app.git#deadbeef:src")
	f.Add("https://user:secret@github.com/org/app.git\x7f")
	f.Add("https://user:p@ss@github.com/org/app.git")

	f.Fuzz(func(t *testing.T, remote string) {
		got := Remote(remote)

		parsed, err := url.Parse(remote)
		if err != nil {
			for _, userinfo := range userinfoInError.FindAllString(got, -1) {
				assert.Equal(t, redactedUserinfo, userinfo)
			}

			return
		}

		if parsed.User == nil {
			assert.Equal(t, remote, got)

			return
		}

		if _, ok := parsed.User.Password(); !ok {
			return
		}

		out, err := url.Parse(got)
		if err != nil || out.User == nil {
			return
		}

		pw, ok := out.User.Password()
		if !ok {
			return
		}

		assert.Equal(t, "xxxxx", pw)
		assert.Equal(t, "xxxxx", out.User.Username())
	})
}

// FuzzQueryKeyWords verifies that splitting a query parameter name never
// panics and loses nothing but separators. Each word is non-empty and lowercase
// with no separators. The words joined in order equal the lowercased name
// without its separators. The seeds cover separated, camelCase, and acronym
// names.
func FuzzQueryKeyWords(f *testing.F) {
	f.Add("access_token")
	f.Add("accessToken")
	f.Add("APIToken")
	f.Add("client-secret.v2")
	f.Add("__")
	f.Add("ÜberToken")
	f.Add("")

	f.Fuzz(func(t *testing.T, key string) {
		words := queryKeyWords(key)

		for _, word := range words {
			assert.NotEmpty(t, word)
			assert.Equal(t, strings.ToLower(word), word)
			assert.False(t, strings.ContainsFunc(word, isQueryKeySeparator), word)
		}

		want := strings.ToLower(strings.Map(func(r rune) rune {
			if isQueryKeySeparator(r) {
				return -1
			}

			return r
		}, key))

		assert.Equal(t, want, strings.Join(words, ""))
	})
}

// FuzzText verifies that redacting command output never panics and changes
// nothing on a second pass. A credential value written as a header line,
// name=value pair, JSON field or URL password is always replaced.
//
// The seed corpus covers plain text, each credential form and values with
// quotes, equals signs and at signs. The regression seed under testdata/fuzz is
// a value with a form feed. No credential form can carry one.
func FuzzText(f *testing.F) {
	for _, seed := range []string{"plain", "abc123", `p@ss"w=rd`, "ghp_abc", ""} {
		f.Add("prefix line", seed)
	}

	f.Fuzz(func(t *testing.T, text, secret string) {
		once := Text(text)
		if twice := Text(once); twice != once {
			t.Fatalf("second pass changed %q to %q", once, twice)
		}

		// A value that redaction can recognize is one word without quotes,
		// separators, whitespace or control characters. Shorter values could
		// appear inside "xxxxx" itself.
		if len(secret) < 6 || strings.ContainsAny(secret, "\"'\\&;,@/:?#=xX") ||
			strings.ContainsFunc(secret, func(char rune) bool { return unicode.IsSpace(char) || unicode.IsControl(char) }) {
			return
		}

		for _, form := range []string{
			"Authorization: Bearer " + secret,
			"> X-Auth-Token: " + secret,
			"DB_PASSWORD=" + secret,
			`{"secret": "` + secret + `"}`,
			"https://bot:" + secret + "@example.com/repo.git",
		} {
			if out := Text(text + "\n" + form); strings.Contains(out, secret) && !strings.Contains(text, secret) {
				t.Fatalf("%q kept the credential: %q", form, out)
			}
		}
	})
}

package redact

import (
	"net/url"
	"testing"

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

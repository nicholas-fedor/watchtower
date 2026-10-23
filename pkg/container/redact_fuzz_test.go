package container

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// FuzzRedactedRemote verifies credential redaction never panics and that a
// parseable password is replaced with xxxxx.
func FuzzRedactedRemote(f *testing.F) {
	f.Add("https://user:secret@github.com/org/app.git#abc")
	f.Add("https://github.com/org/app.git")
	f.Add("://bad")
	f.Add("")
	f.Add("https://user@github.com/org/app.git")
	f.Add("https://x-access-token:ghp_xxx@github.com/org/app.git#deadbeef:src")

	f.Fuzz(func(t *testing.T, remote string) {
		got := redactedRemote(remote)

		parsed, err := url.Parse(remote)
		if err != nil || parsed.User == nil {
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

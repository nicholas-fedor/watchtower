package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseBind covers the bind forms inspect reports, including a Windows
// drive letter and a one-letter Podman volume name. It also covers entries
// that have no source or destination.
func TestParseBind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		bind string
		want Bind
		ok   bool
	}{
		{name: "host path", bind: "/host/data:/data", want: Bind{Source: "/host/data", Destination: "/data", Options: ""}, ok: true},
		{name: "named volume with options", bind: "cache:/var/cache:ro,z", want: Bind{Source: "cache", Destination: "/var/cache", Options: "ro,z"}, ok: true},
		{name: "windows drive letter", bind: `C:\data:/data:ro`, want: Bind{Source: `C:\data`, Destination: "/data", Options: "ro"}, ok: true},
		{name: "one-letter volume", bind: "v:/data", want: Bind{Source: "v", Destination: "/data", Options: ""}, ok: true},
		{name: "one-letter volume with options", bind: "v:/data:ro", want: Bind{Source: "v", Destination: "/data", Options: "ro"}, ok: true},
		{name: "no colon", bind: "nocolon", want: Bind{}, ok: false},
		{name: "empty source", bind: ":/data", want: Bind{}, ok: false},
		{name: "empty destination", bind: "/host/data:", want: Bind{}, ok: false},
		{name: "drive letter alone", bind: "C:", want: Bind{}, ok: false},
		{name: "empty", bind: "", want: Bind{}, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := ParseBind(tt.bind)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestBind_ReadOnly covers the ro option alone, among other options, and
// absent.
func TestBind_ReadOnly(t *testing.T) {
	t.Parallel()

	assert.False(t, Bind{Source: "/host", Destination: "/data", Options: ""}.ReadOnly())
	assert.False(t, Bind{Source: "/host", Destination: "/data", Options: "rw,z"}.ReadOnly())
	assert.True(t, Bind{Source: "/host", Destination: "/data", Options: "ro"}.ReadOnly())
	assert.True(t, Bind{Source: "/host", Destination: "/data", Options: "z,ro"}.ReadOnly())
}

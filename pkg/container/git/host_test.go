package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAPIOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "https", raw: "https://git.example.com:3000", want: "https://git.example.com:3000"},
		{name: "trailing slash", raw: "https://git.example.com/", want: "https://git.example.com"},
		{name: "hostname only", raw: "gitlab.internal", want: "https://gitlab.internal"},
		{name: "path prefix", raw: "https://git.example.com/gitlab", want: "https://git.example.com/gitlab"},
		{name: "http scheme", raw: "http://git.example.com", want: "http://git.example.com"},
		{name: "ipv6", raw: "https://[2001:db8::1]", want: "https://[2001:db8::1]"},
		{name: "strips userinfo", raw: "https://user:pass@git.example.com", want: "https://git.example.com"},
		{name: "empty", wantErr: true},
		{name: "ssh scheme", raw: "ssh://git.example.com", wantErr: true},
		{name: "host equals type", raw: "git.example.com=gitea", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseAPIOrigin(tt.raw)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidHost)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.String())
		})
	}
}

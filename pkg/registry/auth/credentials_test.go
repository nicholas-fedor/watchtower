package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTransformAuth(t *testing.T) {
	tests := []struct {
		name         string
		registryAuth string
		want         string
	}{
		{
			name:         "valid base64 encoded JSON credentials",
			registryAuth: "eyJ1c2VybmFtZSI6InRlc3R1c2VyIiwicGFzc3dvcmQiOiJ0ZXN0cGFzcyJ9",
			want:         "dGVzdHVzZXI6dGVzdHBhc3M=",
		},
		{
			name:         "empty string returns empty",
			registryAuth: "",
			want:         "",
		},
		{
			name:         "valid base64 but invalid JSON returns original input",
			registryAuth: "aGVsbG8=",
			want:         "aGVsbG8=",
		},
		{
			name:         "invalid base64 returns original input",
			registryAuth: "not-valid-base64!!!",
			want:         "not-valid-base64!!!",
		},
		{
			name:         "valid base64 JSON with only username returns original",
			registryAuth: "eyJ1c2VybmFtZSI6InVzZXIifQ==",
			want:         "eyJ1c2VybmFtZSI6InVzZXIifQ==",
		},
		{
			name:         "valid base64 JSON with only password returns original",
			registryAuth: "eyJwYXNzd29yZCI6InBhc3MifQ==",
			want:         "eyJwYXNzd29yZCI6InBhc3MifQ==",
		},
		{
			name:         "valid base64 empty JSON object returns original",
			registryAuth: "e30=",
			want:         "e30=",
		},
		{
			name:         "base64 of plain text returns original input",
			registryAuth: "dGVzdA==",
			want:         "dGVzdA==",
		},
		{
			name:         "base64 with null username returns original",
			registryAuth: "eyJ1c2VybmFtZSI6bnVsbCwgcGFzc3dvcmQiOiJwYXNzIn0=",
			want:         "eyJ1c2VybmFtZSI6bnVsbCwgcGFzc3dvcmQiOiJwYXNzIn0=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TransformAuth(tt.registryAuth)
			assert.Equal(t, tt.want, got)
		})
	}
}

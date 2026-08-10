package container

import (
	"context"
	"net/http"
	"regexp"
	"testing"

	"github.com/onsi/gomega/ghttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerRegistry "github.com/moby/moby/api/types/registry"
	dockerSystem "github.com/moby/moby/api/types/system"
	dockerClient "github.com/moby/moby/client"
)

func Test_imageClient_buildMirrorEndpoints(t *testing.T) {
	tests := []struct {
		name string
		info *dockerSystem.Info
		want []string
	}{
		{
			name: "nil info returns nil",
			info: nil,
			want: nil,
		},
		{
			name: "nil RegistryConfig returns nil",
			info: &dockerSystem.Info{},
			want: nil,
		},
		{
			name: "no mirrors configured returns nil",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{},
				},
			},
			want: nil,
		},
		{
			name: "global mirrors applied to docker hub image",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://mirror.example.com"},
				},
			},
			want: []string{"https://mirror.example.com", ""},
		},
		{
			name: "non-hub image uses global mirrors",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://global-mirror.example.com"},
				},
			},
			want: []string{"https://global-mirror.example.com", ""},
		},
		{
			name: "multiple mirrors tried in order",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{
						"https://primary-mirror.example.com",
						"https://backup-mirror.example.com",
					},
				},
			},
			want: []string{
				"https://primary-mirror.example.com",
				"https://backup-mirror.example.com",
				"",
			},
		},
		{
			name: "whitespace-only mirrors are skipped",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"  ", "https://mirror.example.com", "   "},
				},
			},
			want: []string{"https://mirror.example.com", ""},
		},
		{
			name: "empty mirrors are skipped",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"", "https://mirror.example.com", ""},
				},
			},
			want: []string{"https://mirror.example.com", ""},
		},
		{
			name: "canonical host always appended as final fallback",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://mirror.example.com"},
				},
			},
			want: []string{"https://mirror.example.com", ""},
		},
		{
			name: "mirror URL with path and query is kept verbatim",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://mirror.example.com/v2/?foo=bar#baz"},
				},
			},
			want: []string{"https://mirror.example.com/v2/?foo=bar#baz", ""},
		},
		{
			name: "ipv6 mirror address is supported",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://[2001:db8::1]:5000"},
				},
			},
			want: []string{"https://[2001:db8::1]:5000", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := imageClient{log: testLog()}
			got := c.buildMirrorEndpoints(tt.info)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Test_newImageClient_nilLogger_noMirrors verifies resolveRegistryMirrorConfig does not
// panic when the image client logger is nil and Docker reports no registry mirrors.
func Test_newImageClient_nilLogger_noMirrors(t *testing.T) {
	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	var infoPath string

	server.AppendHandlers(APIVersionPingHandler())
	server.RouteToHandler("GET", regexp.MustCompile(`^/v\d+(\.\d+)*/info$`), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))

		if r != nil {
			infoPath = r.URL.Path
		}
	})

	api, err := dockerClient.New(
		dockerClient.WithHost(server.URL()),
		dockerClient.WithHTTPClient(server.HTTPTestServer.Client()),
	)
	require.NoError(t, err)

	c := newImageClient(api, nil)

	assert.NotPanics(t, func() {
		got := c.resolveRegistryMirrorConfig(context.Background())
		assert.Nil(t, got)
	})

	assert.Regexp(t, `^/v\d+(\.\d+)*/info$`, infoPath, "resolveRegistryMirrorConfig must call the versioned /info endpoint")
}

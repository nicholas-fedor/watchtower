package container

import (
	"testing"

	"github.com/stretchr/testify/assert"

	dockerRegistry "github.com/moby/moby/api/types/registry"
	dockerSystem "github.com/moby/moby/api/types/system"
)

func Test_imageClient_buildMirrorEndpoints(t *testing.T) {
	tests := []struct {
		name           string
		containerImage string
		info           *dockerSystem.Info
		want           []string
	}{
		{
			name:           "nil info returns nil",
			containerImage: "docker.io/library/nginx:latest",
			info:           nil,
			want:           nil,
		},
		{
			name:           "nil RegistryConfig returns nil",
			containerImage: "docker.io/library/nginx:latest",
			info:           &dockerSystem.Info{},
			want:           nil,
		},
		{
			name:           "no mirrors configured returns nil",
			containerImage: "docker.io/library/nginx:latest",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors:      []string{},
					IndexConfigs: map[string]*dockerRegistry.IndexInfo{},
				},
			},
			want: nil,
		},
		{
			name:           "global mirrors applied to docker hub image",
			containerImage: "docker.io/library/nginx:latest",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://mirror.example.com"},
				},
			},
			want: []string{"https://mirror.example.com", ""},
		},
		{
			name:           "per-registry mirrors take precedence over global",
			containerImage: "docker.io/library/nginx:latest",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://global-mirror.example.com"},
					IndexConfigs: map[string]*dockerRegistry.IndexInfo{
						"docker.io": {
							Mirrors: []string{"https://docker-hub-mirror.example.com"},
						},
					},
				},
			},
			want: []string{"https://docker-hub-mirror.example.com", ""},
		},
		{
			name:           "per-registry mirrors used when no global mirrors",
			containerImage: "docker.io/library/nginx:latest",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					IndexConfigs: map[string]*dockerRegistry.IndexInfo{
						"docker.io": {
							Mirrors: []string{"https://docker-hub-mirror.example.com"},
						},
					},
				},
			},
			want: []string{"https://docker-hub-mirror.example.com", ""},
		},
		{
			name:           "non-hub image uses global mirrors",
			containerImage: "ghcr.io/owner/image:latest",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://global-mirror.example.com"},
				},
			},
			want: []string{"https://global-mirror.example.com", ""},
		},
		{
			name:           "non-hub image with per-registry mirror uses dedicated mirror",
			containerImage: "ghcr.io/owner/image:latest",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://global-mirror.example.com"},
					IndexConfigs: map[string]*dockerRegistry.IndexInfo{
						"ghcr.io": {
							Mirrors: []string{"https://ghcr-mirror.example.com"},
						},
					},
				},
			},
			want: []string{"https://ghcr-mirror.example.com", ""},
		},
		{
			name:           "non-hub image without dedicated mirror falls back to global",
			containerImage: "ghcr.io/owner/image:latest",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://global-mirror.example.com"},
					IndexConfigs: map[string]*dockerRegistry.IndexInfo{
						"docker.io": {
							Mirrors: []string{"https://docker-hub-mirror.example.com"},
						},
					},
				},
			},
			want: []string{"https://global-mirror.example.com", ""},
		},
		{
			name:           "multiple mirrors tried in order",
			containerImage: "docker.io/library/nginx:latest",
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
			name:           "whitespace-only mirrors are skipped",
			containerImage: "docker.io/library/nginx:latest",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"  ", "https://mirror.example.com", "   "},
				},
			},
			want: []string{"https://mirror.example.com", ""},
		},
		{
			name:           "empty mirrors are skipped",
			containerImage: "docker.io/library/nginx:latest",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"", "https://mirror.example.com", ""},
				},
			},
			want: []string{"https://mirror.example.com", ""},
		},
		{
			name:           "canonical host always appended as final fallback",
			containerImage: "docker.io/library/nginx:latest",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://mirror.example.com"},
				},
			},
			want: []string{"https://mirror.example.com", ""},
		},
		{
			name:           "invalid image name returns nil",
			containerImage: "!!!invalid:reference",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://mirror.example.com"},
				},
			},
			want: nil,
		},
		{
			name:           "per-registry with empty mirrors falls back to global",
			containerImage: "docker.io/library/nginx:latest",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://global.example.com"},
					IndexConfigs: map[string]*dockerRegistry.IndexInfo{
						"docker.io": {Mirrors: []string{}},
					},
				},
			},
			want: []string{"https://global.example.com", ""},
		},
		{
			name:           "per-registry with empty mirrors and no global returns nil",
			containerImage: "docker.io/library/nginx:latest",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{},
					IndexConfigs: map[string]*dockerRegistry.IndexInfo{
						"docker.io": {Mirrors: []string{}},
					},
				},
			},
			want: nil,
		},
		{
			name:           "mirror URL with path and query is kept verbatim",
			containerImage: "docker.io/library/nginx:latest",
			info: &dockerSystem.Info{
				RegistryConfig: &dockerRegistry.ServiceConfig{
					Mirrors: []string{"https://mirror.example.com/v2/?foo=bar#baz"},
				},
			},
			want: []string{"https://mirror.example.com/v2/?foo=bar#baz", ""},
		},
		{
			name:           "ipv6 mirror address is supported",
			containerImage: "docker.io/library/nginx:latest",
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
			c := imageClient{}
			container := MockContainer(WithImageName(tt.containerImage))
			got := c.buildMirrorEndpoints(container, tt.info)
			assert.Equal(t, tt.want, got)
		})
	}
}

package urlutil

import "testing"

func TestBuildRegistryEndpointURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		endpoint      string
		resourcePath  string
		defaultScheme string
		expected      string
		wantErr       bool
	}{
		{
			name:          "adds default scheme to bare host",
			endpoint:      "mirror.example.com",
			resourcePath:  "/v2/",
			defaultScheme: "https",
			expected:      "https://mirror.example.com/v2/",
		},
		{
			name:          "preserves explicit scheme and joins path",
			endpoint:      "http://mirror.example.com/cache",
			resourcePath:  "/v2/library/nginx/manifests/latest",
			defaultScheme: "https",
			expected:      "http://mirror.example.com/cache/v2/library/nginx/manifests/latest",
		},
		{
			name:          "handles trailing slash on endpoint path",
			endpoint:      "https://mirror.example.com/cache/",
			resourcePath:  "/v2/",
			defaultScheme: "https",
			expected:      "https://mirror.example.com/cache/v2/",
		},
		{
			name:          "fails without host",
			endpoint:      "/cache-only",
			resourcePath:  "/v2/",
			defaultScheme: "https",
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := BuildRegistryEndpointURL(tt.endpoint, tt.resourcePath, tt.defaultScheme)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.String() != tt.expected {
				t.Fatalf("got %q, want %q", got.String(), tt.expected)
			}
		})
	}
}

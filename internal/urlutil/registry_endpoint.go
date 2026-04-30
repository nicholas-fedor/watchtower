package urlutil

import (
	"fmt"
	"net/url"
	"strings"
)

// BuildRegistryEndpointURL builds a registry endpoint URL for a resource path.
// If the endpoint omits a scheme, defaultScheme is applied.
func BuildRegistryEndpointURL(registryEndpoint, resourcePath, defaultScheme string) (*url.URL, error) {
	endpointURL, err := url.Parse(registryEndpoint)
	if err != nil {
		return nil, err
	}

	if endpointURL.Scheme == "" && endpointURL.Host == "" && endpointURL.Path != "" {
		endpointURL, err = url.Parse(defaultScheme + "://" + registryEndpoint)
		if err != nil {
			return nil, err
		}
	}

	if endpointURL.Scheme == "" {
		endpointURL.Scheme = defaultScheme
	}

	if endpointURL.Host == "" {
		return nil, fmt.Errorf("missing host")
	}

	endpointURL.Path = joinURLPath(endpointURL.Path, resourcePath)
	endpointURL.RawPath = ""
	endpointURL.RawQuery = ""
	endpointURL.Fragment = ""

	return endpointURL, nil
}

func joinURLPath(basePath, resourcePath string) string {
	switch {
	case basePath == "":
		return resourcePath
	case strings.HasSuffix(basePath, "/") && strings.HasPrefix(resourcePath, "/"):
		return basePath + strings.TrimPrefix(resourcePath, "/")
	case !strings.HasSuffix(basePath, "/") && !strings.HasPrefix(resourcePath, "/"):
		return basePath + "/" + resourcePath
	default:
		return basePath + resourcePath
	}
}

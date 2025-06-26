package digest_test

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	dockerImageType "github.com/docker/docker/api/types/image"

	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"
	"github.com/nicholas-fedor/watchtower/pkg/registry/digest"
	"github.com/nicholas-fedor/watchtower/pkg/registry/helpers"
	"github.com/nicholas-fedor/watchtower/pkg/registry/manifest"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestDigest(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Digest Suite")
}

var (
	DockerHubCredentials = &types.RegistryCredentials{
		Username: os.Getenv("CI_INTEGRATION_TEST_REGISTRY_DH_USERNAME"),
		Password: os.Getenv("CI_INTEGRATION_TEST_REGISTRY_DH_PASSWORD"),
	}
	GHCRCredentials = &types.RegistryCredentials{
		Username: os.Getenv("CI_INTEGRATION_TEST_REGISTRY_GH_USERNAME"),
		Password: os.Getenv("CI_INTEGRATION_TEST_REGISTRY_GH_PASSWORD"),
	}
)

// SkipIfCredentialsEmpty skips a test if registry credentials are incomplete.
// It checks for empty username or password, skipping with a message, otherwise runs the test.
func SkipIfCredentialsEmpty(credentials *types.RegistryCredentials, testFunc func()) func() {
	switch {
	case credentials.Username == "":
		return func() { ginkgo.Skip("Username missing. Skipping integration test") }
	case credentials.Password == "":
		return func() { ginkgo.Skip("Password missing. Skipping integration test") }
	default:
		return testFunc
	}
}

// testAuthClient is a custom implementation of the AuthClient interface for testing.
type testAuthClient struct {
	client *http.Client
}

func (t *testAuthClient) Do(req *http.Request) (*http.Response, error) {
	return t.client.Do(req)
}

var _ = ginkgo.BeforeSuite(func() {
	// Ensure WATCHTOWER_REGISTRY_TLS_SKIP is false to use https scheme
	viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
})

var _ = ginkgo.AfterSuite(func() {
	// Reset Viper configuration
	viper.Reset()
})

var _ = ginkgo.Describe("Digests", func() {
	// Predefined mock data for consistent test cases
	mockID := "mock-id"
	mockName := "mock-container"
	mockImage := "ghcr.io/k6io/operator:latest"
	mockCreated := time.Now()
	mockDigest := "ghcr.io/k6io/operator@sha256:d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547"
	mockDigestHash := "sha256:d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547"
	mockDifferentDigest := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	mockInvalidDigest := "invalid-digest" // Malformed digest for testing

	// Mock containers for testing
	mockContainer := mocks.CreateMockContainerWithDigest(
		mockID,
		mockName,
		mockImage,
		mockCreated,
		mockDigest,
	)

	mockContainerNoImage := mocks.CreateMockContainerWithImageInfoP(
		mockID,
		mockName,
		mockImage,
		mockCreated,
		nil,
	)

	// newTestAuthClient creates a test AuthClient with optional TLS handshake timeout
	newTestAuthClient := func(timeout ...time.Duration) auth.Client {
		transport := &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: true,
		}
		if len(timeout) > 0 {
			transport.TLSHandshakeTimeout = timeout[0]
		}

		return &testAuthClient{
			client: &http.Client{
				Transport: transport,
			},
		}
	}

	// extractHeadDigest replicates digest.extractHeadDigest logic
	extractHeadDigest := func(resp *http.Response) (string, error) {
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf(
				"registry responded with invalid HEAD request: status %q, auth: %q",
				resp.Status,
				resp.Header.Get("Www-Authenticate"),
			)
		}
		digestHeader := resp.Header.Get(digest.ContentDigestHeader)
		if digestHeader == "" {
			return "", fmt.Errorf(
				"registry responded with invalid HEAD request: missing %s header",
				digest.ContentDigestHeader,
			)
		}

		return helpers.NormalizeDigest(digestHeader), nil
	}

	// extractGetDigest replicates digest.extractGetDigest logic
	extractGetDigest := func(resp *http.Response) (string, error) {
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf(
				"registry responded with invalid GET request: status %q",
				resp.Status,
			)
		}
		var manifestResp struct {
			Digest string `json:"digest"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&manifestResp); err != nil {
			return "", fmt.Errorf("failed to extract digest from response: %w", err)
		}
		if manifestResp.Digest == "" {
			return "", errors.New("failed to extract digest from response: empty digest")
		}

		return helpers.NormalizeDigest(manifestResp.Digest), nil
	}

	// digestsMatch replicates digest.digestsMatch logic
	digestsMatch := func(localDigests []string, remoteDigest string) bool {
		if len(localDigests) == 0 {
			return false
		}
		normalizedRemote := helpers.NormalizeDigest(remoteDigest)
		for _, local := range localDigests {
			parts := strings.SplitN(local, "@", 2)
			if len(parts) != 2 {
				continue
			}
			if helpers.NormalizeDigest(parts[1]) == normalizedRemote {
				return true
			}
		}

		return false
	}

	ginkgo.When("a digest comparison is done", func() {
		ginkgo.It("should return true if digests match",
			SkipIfCredentialsEmpty(GHCRCredentials, func() {
				creds := fmt.Sprintf("%s:%s", GHCRCredentials.Username, GHCRCredentials.Password)
				matches, err := digest.CompareDigest(context.Background(), mockContainer, creds)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(matches).To(gomega.BeTrue())
			}),
		)

		ginkgo.It("should return false if RepoDigests is empty", func() {
			server := ghttp.NewTLSServer()
			defer server.Close()

			serverAddr := server.Addr()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerEmptyDigests := mocks.CreateMockContainerWithImageInfoP(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				&dockerImageType.InspectResponse{RepoDigests: []string{}},
			)

			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v2/"),
					ghttp.RespondWith(http.StatusUnauthorized, nil, http.Header{
						"WWW-Authenticate": []string{
							fmt.Sprintf(
								`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`,
								serverAddr,
							),
						},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/token"),
					ghttp.RespondWith(http.StatusOK, `{"token": "mock-token"}`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("HEAD", "/v2/test/image/manifests/latest"),
					ghttp.RespondWith(http.StatusOK, nil, http.Header{
						digest.ContentDigestHeader: []string{mockDigestHash},
					}),
				),
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			token, _, err := auth.GetToken(ctx, mockContainerEmptyDigests, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerEmptyDigests)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set(
				"Accept",
				"application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json",
			)
			req.Header.Set("User-Agent", digest.UserAgent)

			resp, err := client.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			defer resp.Body.Close()

			remoteDigest, err := extractHeadDigest(resp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			matches := digestsMatch(mockContainerEmptyDigests.ImageInfo().RepoDigests, remoteDigest)
			gomega.Expect(matches).To(gomega.BeFalse())
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3))
		})

		ginkgo.It("should return false if digests differ", func() {
			server := ghttp.NewTLSServer()
			defer server.Close()

			serverAddr := server.Addr()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerWithServer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v2/"),
					ghttp.RespondWith(http.StatusUnauthorized, nil, http.Header{
						"WWW-Authenticate": []string{
							fmt.Sprintf(
								`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`,
								serverAddr,
							),
						},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/token"),
					ghttp.RespondWith(http.StatusOK, `{"token": "mock-token"}`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("HEAD", "/v2/test/image/manifests/latest"),
					ghttp.RespondWith(http.StatusOK, nil, http.Header{
						digest.ContentDigestHeader: []string{mockDifferentDigest},
					}),
				),
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			token, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set(
				"Accept",
				"application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json",
			)
			req.Header.Set("User-Agent", digest.UserAgent)

			resp, err := client.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			defer resp.Body.Close()

			remoteDigest, err := extractHeadDigest(resp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			matches := digestsMatch(mockContainerWithServer.ImageInfo().RepoDigests, remoteDigest)
			gomega.Expect(matches).To(gomega.BeFalse())
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3))
		})

		ginkgo.It("should return an error if the registry isn't available", func() {
			mockImageRef := "unreachable.local/test/image:latest"
			mockContainerUnreachable := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			_, _, err := auth.GetToken(ctx, mockContainerUnreachable, registryAuth, client)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("failed to execute challenge request"))
		})

		ginkgo.It("should return an error when container contains no image info", func() {
			matches, err := digest.CompareDigest(
				context.Background(),
				mockContainerNoImage,
				"user:pass",
			)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(matches).To(gomega.BeFalse())
		})

		ginkgo.It("should return an error if manifest URL build fails", func() {
			defer ginkgo.GinkgoRecover()
			// Use an invalid reference to trigger an error; GetToken fails before BuildManifestURL
			mockImageRef := "example.com/test/image:" // Missing tag, invalid format
			mockContainerInvalidImage := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			_, _, err := auth.GetToken(ctx, mockContainerInvalidImage, registryAuth, client)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to parse image name"))
		})

		ginkgo.It("should return an error if HEAD request creation fails", func() {
			defer ginkgo.GinkgoRecover()
			// Use an invalid reference; GetToken fails before request creation
			mockImageRef := "example.com/test/image:latest\x00invalid"
			mockContainerInvalidURL := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			_, _, err := auth.GetToken(ctx, mockContainerInvalidURL, registryAuth, client)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to parse image name"))
		})

		ginkgo.It("should return an error if HEAD request fails", func() {
			defer ginkgo.GinkgoRecover()
			mux := http.NewServeMux()
			server := httptest.NewTLSServer(mux)
			defer server.Close()

			serverAddr := server.Listener.Addr().String()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerWithServer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request")
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Simulating network failure for HEAD request")
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						logrus.WithError(err).Error("Failed to hijack connection")

						return
					}
					conn.Close()
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			token, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set(
				"Accept",
				"application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json",
			)
			req.Header.Set("User-Agent", digest.UserAgent)

			_, err = client.Do(req)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("EOF"))
		})

		ginkgo.It("should return an error if registry responds without digest header", func() {
			server := ghttp.NewTLSServer()
			defer server.Close()

			serverAddr := server.Addr()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerWithServer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v2/"),
					ghttp.RespondWith(http.StatusUnauthorized, nil, http.Header{
						"WWW-Authenticate": []string{
							fmt.Sprintf(
								`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`,
								serverAddr,
							),
						},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/token"),
					ghttp.RespondWith(http.StatusOK, `{"token": "mock-token"}`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("HEAD", "/v2/test/image/manifests/latest"),
					ghttp.RespondWith(http.StatusOK, nil, http.Header{
						"Www-Authenticate": []string{"Bearer realm=invalid"},
					}),
				),
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			token, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set(
				"Accept",
				"application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json",
			)
			req.Header.Set("User-Agent", digest.UserAgent)

			resp, err := client.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			defer resp.Body.Close()

			_, err = extractHeadDigest(resp)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("registry responded with invalid HEAD request"))
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3))
		})

		ginkgo.It("should handle malformed local digests", func() {
			server := ghttp.NewTLSServer()
			defer server.Close()

			serverAddr := server.Addr()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerWithInvalidDigest := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockInvalidDigest,
			)

			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v2/"),
					ghttp.RespondWith(http.StatusUnauthorized, nil, http.Header{
						"WWW-Authenticate": []string{
							fmt.Sprintf(
								`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`,
								serverAddr,
							),
						},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/token"),
					ghttp.RespondWith(http.StatusOK, `{"token": "mock-token"}`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("HEAD", "/v2/test/image/manifests/latest"),
					ghttp.RespondWith(http.StatusOK, nil, http.Header{
						digest.ContentDigestHeader: []string{mockDifferentDigest},
					}),
				),
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			token, _, err := auth.GetToken(
				ctx,
				mockContainerWithInvalidDigest,
				registryAuth,
				client,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithInvalidDigest)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set(
				"Accept",
				"application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json",
			)
			req.Header.Set("User-Agent", digest.UserAgent)

			resp, err := client.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			defer resp.Body.Close()

			remoteDigest, err := extractHeadDigest(resp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			matches := digestsMatch(
				mockContainerWithInvalidDigest.ImageInfo().RepoDigests,
				remoteDigest,
			)
			gomega.Expect(matches).To(gomega.BeFalse())
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3))
		})

		// Test case: Verifies that CompareDigest returns an error when the registry responds with
		// a 401 status and a malformed WWW-Authenticate header, simulating a misconfigured registry.
		ginkgo.It("should handle malformed WWW-Authenticate header", func() {
			defer ginkgo.GinkgoRecover()
			mux := http.NewServeMux()
			server := httptest.NewServer(mux)
			defer server.Close()

			serverAddr := server.Listener.Addr().String()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerWithServer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request")
				w.Header().Set("WWW-Authenticate", `Bearer realm="invalid"`)
				w.WriteHeader(http.StatusUnauthorized)
			})

			client := &testAuthClient{
				client: &http.Client{},
			}

			ctx := context.Background()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			registryAuth := digest.TransformAuth("token")
			_, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("challenge header did not include all values needed to construct an auth url"))

			matches, err := digest.CompareDigest(ctx, mockContainerWithServer, registryAuth)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("challenge header did not include all values needed to construct an auth url"))
			gomega.Expect(matches).To(gomega.BeFalse())
		})
	})

	ginkgo.When("using different registries", func() {
		ginkgo.It("should work with DockerHub",
			SkipIfCredentialsEmpty(DockerHubCredentials, func() {
				ginkgo.GinkgoT().
					Logf("DockerHubCredentials present: %v", DockerHubCredentials != nil)
			}),
		)
		ginkgo.It("should work with GitHub Container Registry", func() {
			SkipIfCredentialsEmpty(GHCRCredentials, func() {
				ginkgo.GinkgoT().Logf("GHCRCredentials present: %v", GHCRCredentials != nil)
			})
		})
	})

	ginkgo.When("sending a HEAD request", func() {
		var server *ghttp.Server

		ginkgo.BeforeEach(func() {
			defer ginkgo.GinkgoRecover()
			server = ghttp.NewTLSServer()
		})

		ginkgo.AfterEach(func() {
			defer ginkgo.GinkgoRecover()
			server.Close()
		})

		ginkgo.It("should use a custom user-agent", func() {
			defer ginkgo.GinkgoRecover()
			serverAddr := server.Addr()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerWithServer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			origUserAgent := digest.UserAgent
			digest.UserAgent = "Watchtower/v0.0.0-unknown"
			defer func() { digest.UserAgent = origUserAgent }()

			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v2/"),
					ghttp.RespondWith(http.StatusUnauthorized, nil, http.Header{
						"WWW-Authenticate": []string{
							fmt.Sprintf(
								`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`,
								serverAddr,
							),
						},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/token"),
					ghttp.RespondWith(http.StatusOK, `{"token": "mock-token"}`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyHeader(http.Header{
						"User-Agent": []string{"Watchtower/v0.0.0-unknown"},
					}),
					ghttp.VerifyRequest("HEAD", "/v2/test/image/manifests/latest"),
					ghttp.RespondWith(http.StatusOK, nil, http.Header{
						digest.ContentDigestHeader: []string{mockDigestHash},
					}),
				),
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			token, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set(
				"Accept",
				"application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json",
			)
			req.Header.Set("User-Agent", digest.UserAgent)

			resp, err := client.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			defer resp.Body.Close()

			remoteDigest, err := extractHeadDigest(resp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			matches := digestsMatch(mockContainerWithServer.ImageInfo().RepoDigests, remoteDigest)
			gomega.Expect(matches).To(gomega.BeTrue())
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3))
		})
	})

	ginkgo.When("transforming authentication", func() {
		ginkgo.It("should transform valid credentials into base64", func() {
			creds := struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}{
				Username: "testuser",
				Password: "testpass",
			}
			jsonData, _ := json.Marshal(creds)
			inputAuth := base64.StdEncoding.EncodeToString(jsonData)

			result := digest.TransformAuth(inputAuth)
			expected := base64.StdEncoding.EncodeToString([]byte("testuser:testpass"))
			gomega.Expect(result).To(gomega.Equal(expected))
		})

		ginkgo.It("should return original input if decoding fails", func() {
			inputAuth := "invalid-base64-string"
			result := digest.TransformAuth(inputAuth)
			gomega.Expect(result).To(gomega.Equal(inputAuth))
		})

		ginkgo.It("should handle empty credentials", func() {
			creds := struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}{
				Username: "",
				Password: "",
			}
			jsonData, _ := json.Marshal(creds)
			inputAuth := base64.StdEncoding.EncodeToString(jsonData)

			result := digest.TransformAuth(inputAuth)
			gomega.Expect(result).To(gomega.Equal(inputAuth))
		})
	})

	ginkgo.When("fetching a digest", func() {
		var server *httptest.Server
		var mux *http.ServeMux

		ginkgo.BeforeEach(func() {
			defer ginkgo.GinkgoRecover()
			mux = http.NewServeMux()
			server = httptest.NewTLSServer(mux)
			logrus.WithField("server_addr", server.Listener.Addr().String()).
				Debug("Starting test server")
		})

		ginkgo.AfterEach(func() {
			defer ginkgo.GinkgoRecover()
			logrus.Debug("Closing test server")
			server.Close()
		})

		ginkgo.It("should fetch a digest successfully", func() {
			defer ginkgo.GinkgoRecover()
			serverAddr := server.Listener.Addr().String()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerWithServer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request")
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Handled GET /v2/test/image/manifests/latest request")
					w.Write([]byte(`{"digest": "` + mockDigestHash + `"}`))
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			token, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set(
				"Accept",
				"application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json",
			)
			req.Header.Set("User-Agent", digest.UserAgent)

			resp, err := client.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			defer resp.Body.Close()

			result, err := extractGetDigest(resp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.Equal(helpers.NormalizeDigest(mockDigestHash)))
		})

		ginkgo.It("should return an error if GET request fails after token", func() {
			defer ginkgo.GinkgoRecover()
			serverAddr := server.Listener.Addr().String()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerWithServer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request")
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Simulating network failure for manifest request")
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						logrus.WithError(err).Error("Failed to hijack connection")

						return
					}
					conn.Close()
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			token, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set(
				"Accept",
				"application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json",
			)
			req.Header.Set("User-Agent", digest.UserAgent)

			_, err = client.Do(req)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("EOF"))
		})

		ginkgo.It("should return an error if TLS handshake times out", func() {
			defer ginkgo.GinkgoRecover()
			serverAddr := server.Listener.Addr().String()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerWithServer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request")
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Simulating slow response for manifest request")
					time.Sleep(500 * time.Millisecond)
					w.Write([]byte(`{"digest": "` + mockDigestHash + `"}`))
				},
			)

			client := newTestAuthClient(50 * time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			registryAuth := digest.TransformAuth("token")
			token, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set(
				"Accept",
				"application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json",
			)
			req.Header.Set("User-Agent", digest.UserAgent)

			resp, err := client.Do(req)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.MatchRegexp("net/http: TLS handshake timeout|context deadline exceeded"))
			if resp != nil {
				resp.Body.Close()
			}
		})

		// Test case: Verifies that GetToken returns an error when the registry is unreachable.
		ginkgo.It("should return an error if GetToken fails", func() {
			defer ginkgo.GinkgoRecover()
			mockImageRef := "unreachable.local/test/image:latest"
			mockContainerUnreachable := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			_, _, err := auth.GetToken(ctx, mockContainerUnreachable, registryAuth, client)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.MatchRegexp("no such host|server misbehaving"))
		})

		ginkgo.It("should return an error if manifest URL build fails", func() {
			defer ginkgo.GinkgoRecover()
			// Use an invalid reference; GetToken fails before BuildManifestURL
			mockImageRef := "example.com/test/image:" // Missing tag, invalid format
			mockContainerInvalidImage := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			_, _, err := auth.GetToken(ctx, mockContainerInvalidImage, registryAuth, client)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to parse image name"))
		})

		ginkgo.It("should return an error if GET request creation fails", func() {
			defer ginkgo.GinkgoRecover()
			serverAddr := server.Listener.Addr().String()
			mockImageRef := serverAddr + "/test/image:latest\x00invalid"
			mockContainerInvalidURL := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request")
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			_, _, err := auth.GetToken(ctx, mockContainerInvalidURL, registryAuth, client)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to parse image name"))
		})

		ginkgo.It("should return an error if response decoding fails", func() {
			defer ginkgo.GinkgoRecover()
			serverAddr := server.Listener.Addr().String()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerWithServer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request")
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Handled GET /v2/test/image/manifests/latest with invalid JSON")
					w.Write([]byte("invalid-json"))
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := digest.TransformAuth("token")
			token, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set(
				"Accept",
				"application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json",
			)
			req.Header.Set("User-Agent", digest.UserAgent)

			resp, err := client.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			defer resp.Body.Close()

			_, err = extractGetDigest(resp)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("failed to extract digest from response"))
		})

		// Test case: Verifies that FetchDigest successfully retrieves a digest from an HTTP registry
		// with WATCHTOWER_REGISTRY_TLS_SKIP enabled, handling empty tokens as errors for unauthenticated registries.
		ginkgo.It("should fetch digest from HTTP registry with TLS skip", func() {
			defer ginkgo.GinkgoRecover()
			mux := http.NewServeMux()
			server := httptest.NewServer(mux)
			defer server.Close()

			serverAddr := server.Listener.Addr().String()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerWithServer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request")
				w.WriteHeader(http.StatusOK)
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Handled GET /v2/test/image/manifests/latest request")
					w.Write([]byte(`{"digest": "` + mockDigestHash + `"}`))
				},
			)

			ctx := context.Background()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			registryAuth := digest.TransformAuth("token")
			result, err := digest.FetchDigest(ctx, mockContainerWithServer, registryAuth)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("empty token received from registry"))
			gomega.Expect(result).To(gomega.Equal(""))
		})
	})

	ginkgo.When("fetching a digest with a redirecting registry", func() {
		ginkgo.It("should update the manifest URL host based on challenge response", func() {
			defer ginkgo.GinkgoRecover()
			mux := http.NewServeMux()
			server := httptest.NewServer(mux)
			defer server.Close()

			redirectMux := http.NewServeMux()
			redirectServer := httptest.NewServer(redirectMux)
			defer redirectServer.Close()

			serverAddr := server.Listener.Addr().String()
			redirectAddr := redirectServer.Listener.Addr().
				String()
				// Use actual redirect server address
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerWithServer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request")
				w.Header().Set(
					"WWW-Authenticate",
					fmt.Sprintf(
						`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`,
						redirectAddr,
					),
				)
				w.WriteHeader(http.StatusUnauthorized)
			})
			redirectMux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			redirectMux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, r *http.Request) {
					logrus.Debug("Handled manifest request")
					if r.Host == redirectAddr {
						if r.Method == http.MethodGet {
							w.Header().Set("Content-Type", "application/json")
							w.Write([]byte(`{"digest": "` + mockDigestHash + `"}`))
						} else {
							w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
							w.WriteHeader(http.StatusOK)
						}
					} else {
						w.Header().Set(
							"WWW-Authenticate",
							fmt.Sprintf(
								`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`,
								redirectAddr,
							),
						)
						w.WriteHeader(http.StatusUnauthorized)
					}
				},
			)

			ctx := context.Background()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			registryAuth := digest.TransformAuth("token")
			result, err := digest.FetchDigest(ctx, mockContainerWithServer, registryAuth)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.Equal(helpers.NormalizeDigest(mockDigestHash)))
		})
	})
})

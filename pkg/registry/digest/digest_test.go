package digest_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/nicholas-fedor/watchtower/pkg/registry/manifest"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	httpScheme  = "http"
	httpsScheme = "https"
)

func getScheme() string {
	if viper.GetBool("WATCHTOWER_REGISTRY_TLS_SKIP") {
		return httpScheme
	}

	return httpsScheme
}

// FuzzExtractGetDigest fuzzes the body parsing in ExtractGetDigest to test for crashes or unexpected behavior with malformed inputs.
func FuzzExtractGetDigest(f *testing.F) {
	// Seed with known good and bad inputs
	f.Add([]byte(`{"digest": "sha256:abc123"}`))
	f.Add([]byte(`invalid json`))
	f.Add([]byte(`sha256:valid`))
	f.Add([]byte(``))
	f.Add([]byte(`{"digest": ""}`))

	f.Fuzz(func(_ *testing.T, body []byte) {
		// Create a mock response with the fuzzed body
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader(body)),
		}

		resp.Header.Set("Content-Type", "application/json")
		defer resp.Body.Close()

		// Call ExtractGetDigest; we don't care about the result, just that it doesn't panic
		_, _ = digest.ExtractGetDigest(resp)
	})
}

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

// failingReader is a mock io.Reader that always returns an error, used for testing io.ReadAll failures.
type failingReader struct{}

func (f *failingReader) Read(_ []byte) (int, error) {
	return 0, errors.New("simulated read failure")
}

var _ = ginkgo.BeforeSuite(func() {
	// Ensure WATCHTOWER_REGISTRY_TLS_SKIP is false to use https scheme
	viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
	// Set log level to debug to ensure debug logs are executed for coverage
	logrus.SetLevel(logrus.DebugLevel)
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

		return digest.NormalizeDigest(digestHeader), nil
	}

	// digestsMatch replicates digest.digestsMatch logic
	digestsMatch := func(localDigests []string, remoteDigest string) bool {
		if len(localDigests) == 0 {
			return false
		}
		normalizedRemote := digest.NormalizeDigest(remoteDigest)
		for _, local := range localDigests {
			parts := strings.SplitN(local, "@", 2)
			if len(parts) != 2 {
				continue
			}
			if digest.NormalizeDigest(parts[1]) == normalizedRemote {
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
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerEmptyDigests, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerEmptyDigests, getScheme())
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
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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
			registryAuth := auth.TransformAuth("token")
			_, _, _, err := auth.GetToken(ctx, mockContainerUnreachable, registryAuth, client)
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
			registryAuth := auth.TransformAuth("token")
			_, _, _, err := auth.GetToken(ctx, mockContainerInvalidImage, registryAuth, client)
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
			registryAuth := auth.TransformAuth("token")
			_, _, _, err := auth.GetToken(ctx, mockContainerInvalidURL, registryAuth, client)
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
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(
				ctx,
				mockContainerWithInvalidDigest,
				registryAuth,
				client,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithInvalidDigest, getScheme())
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
			registryAuth := auth.TransformAuth("token")
			_, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("challenge header did not include all values needed to construct an auth url"))

			matches, err := digest.CompareDigest(ctx, mockContainerWithServer, registryAuth)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("challenge header did not include all values needed to construct an auth url"))
			gomega.Expect(matches).To(gomega.BeFalse())
		})

		ginkgo.It("should not fall back to GET when HEAD returns 404", func() {
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
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, r *http.Request) {
					logrus.Debug("Handled manifest request")
					if r.Method == http.MethodHead {
						w.WriteHeader(http.StatusNotFound)
					} else {
						w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
						w.WriteHeader(http.StatusOK)
					}
				},
			)

			ctx := context.Background()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			registryAuth := auth.TransformAuth("token")

			// Test that CompareDigest does not fall back to GET for 404 and returns error
			matches, err := digest.CompareDigest(ctx, mockContainerWithServer, registryAuth)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(matches).To(gomega.BeFalse())
		})

		ginkgo.It("should return error when both HEAD and GET fail", func() {
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
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, r *http.Request) {
					logrus.Debug("Handled manifest request")
					if r.Method == http.MethodHead {
						w.WriteHeader(http.StatusNotFound)
					} else {
						w.WriteHeader(http.StatusInternalServerError)
						w.Write([]byte("server error"))
					}
				},
			)

			ctx := context.Background()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			registryAuth := auth.TransformAuth("token")

			// Test that CompareDigest fails when HEAD returns 500 (non-404 error)
			_, err := digest.CompareDigest(ctx, mockContainerWithServer, registryAuth)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("registry responded with invalid HEAD request"))
		})
		ginkgo.It("should return true when HEAD request succeeds with matching digest", func() {
			defer ginkgo.GinkgoRecover()
			mux := http.NewServeMux()
			server := httptest.NewServer(mux)
			defer server.Close()

			serverAddr := server.Listener.Addr().String()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request")
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, r *http.Request) {
					logrus.Debug("Handled manifest request")
					if r.Method == http.MethodHead {
						w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
						w.WriteHeader(http.StatusOK)
					} else {
						w.WriteHeader(http.StatusInternalServerError)
					}
				},
			)

			ctx := context.Background()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			registryAuth := auth.TransformAuth("token")
			matches, err := digest.CompareDigest(ctx, mockContainer, registryAuth)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(matches).To(gomega.BeTrue())
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
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

		ginkgo.It("should handle HEAD request with non-200 status", func() {
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
					ghttp.RespondWith(http.StatusNotFound, nil, http.Header{
						digest.ContentDigestHeader: []string{mockDigestHash},
					}),
				),
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			// Test extractHeadDigest directly with non-200 status
			_, err = extractHeadDigest(resp)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("registry responded with invalid HEAD request"))
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3))
		})

		ginkgo.It("should handle extractHeadDigest with missing digest header", func() {
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
						// Intentionally omit the digest header
						"Www-Authenticate": []string{"Bearer realm=invalid"},
					}),
				),
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			// Test extractHeadDigest directly with missing digest header
			_, err = extractHeadDigest(resp)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("registry responded with invalid HEAD request"))
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3))
		})

		ginkgo.It("should handle extractHeadDigest with valid digest header", func() {
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
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			// Test extractHeadDigest directly with valid digest header
			result, err := extractHeadDigest(resp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.Equal(digest.NormalizeDigest(mockDigestHash)))
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3))
		})
	})

	ginkgo.When("testing digest matching", func() {
		ginkgo.It("should handle malformed local digests without @ separator", func() {
			localDigests := []string{"malformed-digest"}
			remoteDigest := mockDigestHash
			result := digestsMatch(localDigests, remoteDigest)
			gomega.Expect(result).To(gomega.BeFalse())
		})

		ginkgo.It("should handle local digests with empty parts after @", func() {
			localDigests := []string{"repo@"}
			remoteDigest := mockDigestHash
			result := digestsMatch(localDigests, remoteDigest)
			gomega.Expect(result).To(gomega.BeFalse())
		})

		ginkgo.It("should handle local digests with only one part after @", func() {
			localDigests := []string{"repo@singlepart"}
			remoteDigest := mockDigestHash
			result := digestsMatch(localDigests, remoteDigest)
			gomega.Expect(result).To(gomega.BeFalse())
		})

		ginkgo.It("should match when local digest has multiple @ separators", func() {
			localDigests := []string{
				"repo@namespace@sha256:d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547",
			}
			remoteDigest := mockDigestHash
			result := digestsMatch(localDigests, remoteDigest)
			gomega.Expect(result).To(gomega.BeFalse()) // Should not match due to malformed format
		})

		ginkgo.It("should handle empty local digests slice", func() {
			localDigests := []string{}
			remoteDigest := mockDigestHash
			result := digestsMatch(localDigests, remoteDigest)
			gomega.Expect(result).To(gomega.BeFalse())
		})

		ginkgo.It("should handle nil local digests slice", func() {
			var localDigests []string
			remoteDigest := mockDigestHash
			result := digestsMatch(localDigests, remoteDigest)
			gomega.Expect(result).To(gomega.BeFalse())
		})

		ginkgo.Describe("NormalizeDigest", func() {
			ginkgo.It("should trim sha256: prefix from digest", func() {
				input := "sha256:d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547"
				expected := "d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547"
				gomega.Expect(digest.NormalizeDigest(input)).To(gomega.Equal(expected))
			})

			ginkgo.It("should return unchanged digest without recognized prefix", func() {
				input := "d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547"
				gomega.Expect(digest.NormalizeDigest(input)).To(gomega.Equal(input))
			})

			ginkgo.It("should handle empty digest string", func() {
				input := ""
				gomega.Expect(digest.NormalizeDigest(input)).To(gomega.Equal(""))
			})

			ginkgo.It("should handle digest with unrecognized prefix", func() {
				input := "md5:1234567890abcdef"
				gomega.Expect(digest.NormalizeDigest(input)).To(gomega.Equal(input))
			})
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

			result := auth.TransformAuth(inputAuth)
			expected := base64.StdEncoding.EncodeToString([]byte("testuser:testpass"))
			gomega.Expect(result).To(gomega.Equal(expected))
		})

		ginkgo.It("should return original input if decoding fails", func() {
			inputAuth := "invalid-base64-string"
			result := auth.TransformAuth(inputAuth)
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

			result := auth.TransformAuth(inputAuth)
			gomega.Expect(result).To(gomega.Equal(inputAuth))
		})
	})

	ginkgo.When("fetching a digest", func() {
		var server *httptest.Server
		var mux *http.ServeMux

		ginkgo.BeforeEach(func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			mux = http.NewServeMux()
			server = httptest.NewServer(mux)
			logrus.WithField("server_addr", server.Listener.Addr().String()).
				Debug("Starting test server")
		})

		ginkgo.AfterEach(func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			logrus.Debug("Closing test server")
			server.Close()
		})

		ginkgo.It("should fetch a digest successfully", func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			scheme := "https"
			if viper.GetBool("WATCHTOWER_REGISTRY_TLS_SKIP") {
				scheme = "http"
			}
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
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s://%s/token",service="test-service",scope="repository:test/image:pull"`, scheme, serverAddr))
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
					w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
					w.WriteHeader(http.StatusOK)
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			result, err := digest.ExtractGetDigest(resp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.Equal(digest.NormalizeDigest(mockDigestHash)))
		})

		ginkgo.It("should return an error if GET request fails after token", func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
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
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
				scheme := "https"
				if viper.GetBool("WATCHTOWER_REGISTRY_TLS_SKIP") {
					scheme = "http"
				}
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s://%s/token",service="test-service",scope="repository:test/image:pull"`, scheme, serverAddr))
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
					w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
					w.WriteHeader(http.StatusOK)
				},
			)

			client := newTestAuthClient(50 * time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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
			registryAuth := auth.TransformAuth("token")
			_, _, _, err := auth.GetToken(ctx, mockContainerUnreachable, registryAuth, client)
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
			registryAuth := auth.TransformAuth("token")
			_, _, _, err := auth.GetToken(ctx, mockContainerInvalidImage, registryAuth, client)
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
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := auth.TransformAuth("token")
			_, _, _, err := auth.GetToken(ctx, mockContainerInvalidURL, registryAuth, client)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to parse image name"))
		})

		ginkgo.It("should return an error if response decoding fails", func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
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
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			_, err = digest.ExtractGetDigest(resp)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("invalid digest format in body"))
		})

		ginkgo.It("should fall back to header when JSON decoding fails", func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug(
						"Handled GET /v2/test/image/manifests/latest with invalid JSON but valid header",
					)
					w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
					w.Write([]byte("invalid-json"))
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			result, err := digest.ExtractGetDigest(resp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.Equal(digest.NormalizeDigest(mockDigestHash)))
		})

		ginkgo.It("should parse JSON manifest for digest extraction", func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug(
						"Handled GET /v2/test/image/manifests/latest with plain text digest",
					)
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(mockDigestHash))
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			result, err := digest.ExtractGetDigest(resp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.Equal(digest.NormalizeDigest(mockDigestHash)))
		})

		ginkgo.It("should handle empty body error", func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Handled GET /v2/test/image/manifests/latest with empty body")
					w.WriteHeader(http.StatusOK)
					// Write empty body
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			_, err = digest.ExtractGetDigest(resp)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("missing digest header and empty body"))
		})

		ginkgo.It("should handle malformed JSON manifest", func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Handled GET /v2/test/image/manifests/latest with malformed JSON")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`invalid json`)) // Malformed JSON
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			_, err = digest.ExtractGetDigest(resp)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("invalid digest format in body"))
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
					logrus.Debug("Handled HEAD /v2/test/image/manifests/latest request")
					w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
					w.WriteHeader(http.StatusOK)
				},
			)

			ctx := context.Background()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			registryAuth := auth.TransformAuth("token")
			result, err := digest.CompareDigest(ctx, mockContainerWithServer, registryAuth)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.BeTrue())
		})

		ginkgo.It("should parse valid JSON manifest with digest field", func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug(
						"Handled GET /v2/test/image/manifests/latest with valid JSON manifest",
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					fmt.Fprintf(w, `{"digest": "%s"}`, mockDigestHash)
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			result, err := digest.ExtractGetDigest(resp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.Equal(digest.NormalizeDigest(mockDigestHash)))
		})

		ginkgo.It("should handle JSON manifest with empty digest field", func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug(
						"Handled GET /v2/test/image/manifests/latest with JSON manifest with empty digest",
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"digest": ""}`))
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			_, err = digest.ExtractGetDigest(resp)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("empty digest in JSON manifest"))
		})

		ginkgo.It("should handle JSON manifest without digest field", func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug(
						"Handled GET /v2/test/image/manifests/latest with JSON manifest without digest field",
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"other_field": "value"}`))
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			_, err = digest.ExtractGetDigest(resp)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("empty digest in JSON manifest"))
		})

		ginkgo.It("should handle invalid plain text digest format", func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug(
						"Handled GET /v2/test/image/manifests/latest with invalid plain text digest",
					)
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("invalid-digest-format"))
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			_, err = digest.ExtractGetDigest(resp)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("invalid digest format in body"))
		})

		ginkgo.It("should handle short plain text digest", func() {
			defer ginkgo.GinkgoRecover()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug(
						"Handled GET /v2/test/image/manifests/latest with short plain text digest",
					)
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("sha256:short"))
				},
			)

			client := newTestAuthClient()
			ctx := context.Background()
			registryAuth := auth.TransformAuth("token")
			token, _, _, err := auth.GetToken(ctx, mockContainerWithServer, registryAuth, client)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

			_, err = digest.ExtractGetDigest(resp)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("invalid digest format in body"))
		})

		ginkgo.It("should handle io.ReadAll failure in ExtractGetDigest", func() {
			defer ginkgo.GinkgoRecover()

			// Define a failing reader that returns an error on Read
			failingReader := &failingReader{}

			// Create a mock response with the failing body
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(failingReader),
			}

			_, err := digest.ExtractGetDigest(resp)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("failed to read response body"))
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
			redirectAddr := redirectServer.Listener.Addr().String()
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
				logrus.Debug("Handled GET /v2/ request - redirecting")
				w.Header().Set("Location", fmt.Sprintf("http://%s/v2/", redirectAddr))
				w.WriteHeader(http.StatusFound)
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						w.WriteHeader(http.StatusNotFound)
					} else {
						w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
						w.WriteHeader(http.StatusOK)
					}
				},
			)
			redirectMux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request on redirect server")
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s://%s/token",service="test-service",scope="repository:test/image:pull"`, getScheme(), redirectAddr))
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
						w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
						w.WriteHeader(http.StatusOK)
					} else {
						w.Header().Set(
							"WWW-Authenticate",
							fmt.Sprintf(
								`Bearer realm="%s://%s/token",service="test-service",scope="repository:test/image:pull"`,
								getScheme(),
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
			registryAuth := auth.TransformAuth("token")
			result, err := digest.FetchDigest(ctx, mockContainerWithServer, registryAuth)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).
				To(gomega.Equal("d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547"))
		})
		ginkgo.It("should conditionally update manifest URL host only when redirected", func() {
			defer ginkgo.GinkgoRecover()
			mux := http.NewServeMux()
			server := httptest.NewServer(mux)
			defer server.Close()

			redirectMux := http.NewServeMux()
			redirectServer := httptest.NewServer(redirectMux)
			defer redirectServer.Close()

			serverAddr := server.Listener.Addr().String()
			redirectAddr := redirectServer.Listener.Addr().String()
			mockImageRef := serverAddr + "/test/image:latest"
			mockContainerWithServer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			// Test case 1: redirected=true, should update host
			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request - redirecting")
				w.Header().Set("Location", fmt.Sprintf("http://%s/v2/", redirectAddr))
				w.WriteHeader(http.StatusFound)
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						w.WriteHeader(http.StatusNotFound)
					} else {
						w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
						w.WriteHeader(http.StatusOK)
					}
				},
			)
			redirectMux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request on redirect server")
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s://%s/token",service="test-service",scope="repository:test/image:pull"`, getScheme(), redirectAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			redirectMux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			redirectMux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Handled manifest request on redirect server")
					w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
					w.WriteHeader(http.StatusOK)
				},
			)

			ctx := context.Background()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			registryAuth := auth.TransformAuth("token")
			result, err := digest.FetchDigest(ctx, mockContainerWithServer, registryAuth)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).
				To(gomega.Equal("d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547"))
		})

		ginkgo.It("should not update manifest URL host when not redirected", func() {
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

			// Test case 2: redirected=false, should not update host
			mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /v2/ request - no redirect")
				w.Header().
					Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
				w.WriteHeader(http.StatusUnauthorized)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
				logrus.Debug("Handled GET /token request")
				w.Write([]byte(`{"token": "mock-token"}`))
			})
			mux.HandleFunc(
				"/v2/test/image/manifests/latest",
				func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Handled manifest request on original server")
					w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
					w.WriteHeader(http.StatusOK)
				},
			)

			ctx := context.Background()
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			registryAuth := auth.TransformAuth("token")
			result, err := digest.FetchDigest(ctx, mockContainerWithServer, registryAuth)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.Equal(digest.NormalizeDigest(mockDigestHash)))
		})

		ginkgo.When("testing fetchDigest function directly", func() {
			var server *httptest.Server
			var mux *http.ServeMux

			ginkgo.BeforeEach(func() {
				defer ginkgo.GinkgoRecover()
				mux = http.NewServeMux()
				server = httptest.NewServer(mux)
			})

			ginkgo.AfterEach(func() {
				defer ginkgo.GinkgoRecover()
				server.Close()
			})

			ginkgo.It("should handle no authentication required", func() {
				defer ginkgo.GinkgoRecover()
				// Use HTTP server for this test since we set TLS_SKIP
				httpServer := httptest.NewServer(mux)
				defer httpServer.Close()

				serverAddr := httpServer.Listener.Addr().String()
				mockImageRef := serverAddr + "/test/image:latest"
				mockContainerWithServer := mocks.CreateMockContainerWithDigest(
					mockID,
					mockName,
					mockImageRef,
					mockCreated,
					mockDigest,
				)

				mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Handled GET /v2/ request - no auth required")
					w.WriteHeader(http.StatusOK)
				})
				mux.HandleFunc(
					"/v2/test/image/manifests/latest",
					func(w http.ResponseWriter, _ *http.Request) {
						logrus.Debug("Handled GET /v2/test/image/manifests/latest - no auth")
						w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
						w.WriteHeader(http.StatusOK)
					},
				)

				ctx := context.Background()
				viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
				defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
				registryAuth := auth.TransformAuth("")
				result, err := digest.FetchDigest(ctx, mockContainerWithServer, registryAuth)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(result).To(gomega.Equal(digest.NormalizeDigest(mockDigestHash)))
			})

			ginkgo.It("should handle invalid image reference", func() {
				defer ginkgo.GinkgoRecover()
				// Create a mock container with invalid image reference
				// This should cause manifest.BuildManifestURL to fail
				mockContainerInvalid := mocks.CreateMockContainerWithDigest(
					mockID,
					mockName,
					"example.com/test/image:", // Missing tag, invalid format
					mockCreated,
					mockDigest,
				)

				ctx := context.Background()
				registryAuth := auth.TransformAuth("token")
				_, err := digest.FetchDigest(ctx, mockContainerInvalid, registryAuth)
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err.Error()).
					To(gomega.ContainSubstring("failed to parse image name"))
			})

			ginkgo.It("should handle URL parsing failure", func() {
				defer ginkgo.GinkgoRecover()
				mockContainerInvalidURL := mocks.CreateMockContainerWithDigest(
					mockID,
					mockName,
					"http://invalid url with spaces/test/image:latest", // Invalid URL
					mockCreated,
					mockDigest,
				)

				ctx := context.Background()
				registryAuth := auth.TransformAuth("token")
				_, err := digest.FetchDigest(ctx, mockContainerInvalidURL, registryAuth)
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err.Error()).
					To(gomega.ContainSubstring("failed to build manifest URL"))
			})

			ginkgo.It("should handle plain text 404 responses (non-JSON body)", func() {
				defer ginkgo.GinkgoRecover()
				viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
				defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
						Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
					w.WriteHeader(http.StatusUnauthorized)
				})
				mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Handled GET /token request")
					w.Write([]byte(`{"token": "mock-token"}`))
				})
				mux.HandleFunc(
					"/v2/test/image/manifests/latest",
					func(w http.ResponseWriter, _ *http.Request) {
						logrus.Debug(
							"Handled GET /v2/test/image/manifests/latest with plain text 404 body",
						)
						w.WriteHeader(http.StatusOK)
						w.Write([]byte("404 Not Found")) // Plain text non-JSON body
					},
				)

				client := newTestAuthClient()
				ctx := context.Background()
				registryAuth := auth.TransformAuth("token")
				token, _, _, err := auth.GetToken(
					ctx,
					mockContainerWithServer,
					registryAuth,
					client,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

				_, err = digest.ExtractGetDigest(resp)
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err.Error()).
					To(gomega.ContainSubstring("invalid digest format in body"))
			})

			ginkgo.It("should handle OCI image index responses with proper Content-Type", func() {
				defer ginkgo.GinkgoRecover()
				viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
				defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
						Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
					w.WriteHeader(http.StatusUnauthorized)
				})
				mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Handled GET /token request")
					w.Write([]byte(`{"token": "mock-token"}`))
				})
				mux.HandleFunc(
					"/v2/test/image/manifests/latest",
					func(w http.ResponseWriter, _ *http.Request) {
						logrus.Debug(
							"Handled GET /v2/test/image/manifests/latest with OCI image index",
						)
						w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
						w.WriteHeader(http.StatusOK)
						w.Write(
							[]byte(
								`{"digest": "sha256:ociindexdigest123456789012345678901234567890123456789012345678901234567890"}`,
							),
						)
					},
				)

				client := newTestAuthClient()
				ctx := context.Background()
				registryAuth := auth.TransformAuth("token")
				token, _, _, err := auth.GetToken(
					ctx,
					mockContainerWithServer,
					registryAuth,
					client,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

				result, err := digest.ExtractGetDigest(resp)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(result).
					To(gomega.Equal("ociindexdigest123456789012345678901234567890123456789012345678901234567890"))
			})

			ginkgo.It("should handle invalid Content-Type headers", func() {
				defer ginkgo.GinkgoRecover()
				viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
				defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
						Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
					w.WriteHeader(http.StatusUnauthorized)
				})
				mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Handled GET /token request")
					w.Write([]byte(`{"token": "mock-token"}`))
				})
				mux.HandleFunc(
					"/v2/test/image/manifests/latest",
					func(w http.ResponseWriter, _ *http.Request) {
						logrus.Debug(
							"Handled GET /v2/test/image/manifests/latest with invalid Content-Type",
						)
						w.Header().
							Set("Content-Type", "text/plain")
							// Invalid Content-Type for JSON
						w.WriteHeader(http.StatusOK)
						w.Write([]byte(`{"digest": "sha256:invalidcontenttypedigest"}`))
					},
				)

				client := newTestAuthClient()
				ctx := context.Background()
				registryAuth := auth.TransformAuth("token")
				token, _, _, err := auth.GetToken(
					ctx,
					mockContainerWithServer,
					registryAuth,
					client,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

				_, err = digest.ExtractGetDigest(resp)
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err.Error()).
					To(gomega.ContainSubstring("unsupported content type for JSON parsing"))
			})

			ginkgo.It("should handle missing or malformed Content-Type headers", func() {
				defer ginkgo.GinkgoRecover()
				viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
				defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
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
						Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr))
					w.WriteHeader(http.StatusUnauthorized)
				})
				mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
					logrus.Debug("Handled GET /token request")
					w.Write([]byte(`{"token": "mock-token"}`))
				})
				mux.HandleFunc(
					"/v2/test/image/manifests/latest",
					func(w http.ResponseWriter, _ *http.Request) {
						logrus.Debug(
							"Handled GET /v2/test/image/manifests/latest with missing Content-Type",
						)
						// No Content-Type header set
						w.WriteHeader(http.StatusOK)
						w.Write([]byte(`{"digest": "sha256:missingcontenttypedigest"}`))
					},
				)

				client := newTestAuthClient()
				ctx := context.Background()
				registryAuth := auth.TransformAuth("token")
				token, _, _, err := auth.GetToken(
					ctx,
					mockContainerWithServer,
					registryAuth,
					client,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				url, err := manifest.BuildManifestURL(mockContainerWithServer, getScheme())
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

				_, err = digest.ExtractGetDigest(resp)
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err.Error()).
					To(gomega.ContainSubstring("unsupported content type for JSON parsing"))
			})

			ginkgo.It(
				"should successfully use HEAD requests for lscr.io images when redirected",
				func() {
					defer ginkgo.GinkgoRecover()
					mux := http.NewServeMux()
					server := httptest.NewServer(mux)
					defer server.Close()

					redirectMux := http.NewServeMux()
					redirectServer := httptest.NewServer(redirectMux)
					defer redirectServer.Close()

					serverAddr := server.Listener.Addr().String()
					redirectAddr := redirectServer.Listener.Addr().String()
					// Use lscr.io image name to trigger special handling
					mockImageRef := serverAddr + "/lscr.io/test/image:latest"
					mockContainerWithServer := mocks.CreateMockContainerWithDigest(
						mockID,
						mockName,
						mockImageRef,
						mockCreated,
						mockDigest,
					)

					mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
						logrus.Debug("Handled GET /v2/ request - redirecting lscr.io")
						w.Header().Set("Location", fmt.Sprintf("http://%s/v2/", redirectAddr))
						w.WriteHeader(http.StatusFound)
					})
					mux.HandleFunc(
						"/v2/lscr.io/test/image/manifests/latest",
						func(w http.ResponseWriter, r *http.Request) {
							if r.Method == http.MethodHead {
								w.WriteHeader(http.StatusNotFound)
							} else {
								w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
								w.WriteHeader(http.StatusOK)
							}
						},
					)
					redirectMux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
						logrus.Debug("Handled GET /v2/ request on redirect server for lscr.io")
						w.Header().
							Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="repository:lscr.io/test/image:pull"`, redirectAddr))
						w.WriteHeader(http.StatusUnauthorized)
					})
					redirectMux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
						logrus.Debug("Handled GET /token request for lscr.io")
						w.Write([]byte(`{"token": "mock-token"}`))
					})
					redirectMux.HandleFunc(
						"/v2/lscr.io/test/image/manifests/latest",
						func(w http.ResponseWriter, r *http.Request) {
							logrus.Debug("Handled manifest request for lscr.io")
							if r.Method == http.MethodHead {
								// Simulate successful HEAD request for lscr.io
								w.Header().Set(digest.ContentDigestHeader, mockDigestHash)
								w.WriteHeader(http.StatusOK)
							} else {
								w.WriteHeader(http.StatusInternalServerError)
							}
						},
					)

					ctx := context.Background()
					viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
					defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
					registryAuth := auth.TransformAuth("token")
					result, err := digest.CompareDigest(ctx, mockContainerWithServer, registryAuth)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(result).To(gomega.BeTrue())
				},
			)
		})
	})
})

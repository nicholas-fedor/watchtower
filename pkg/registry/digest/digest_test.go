// Package digest_test provides tests for the digest retrieval functionality in Watchtower.
// It verifies digest comparison and fetching behavior against mock and real registries.
package digest_test

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	dockerImageType "github.com/docker/docker/api/types/image"

	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"
	"github.com/nicholas-fedor/watchtower/pkg/registry/digest"
	"github.com/nicholas-fedor/watchtower/pkg/registry/helpers"
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
	origClient *http.Client
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

var _ = ginkgo.BeforeSuite(func() {
	// Save the original client and set an insecure one for all tests
	origClient = auth.Client
	auth.Client = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
})

var _ = ginkgo.AfterSuite(func() {
	// Restore the original client
	auth.Client = origClient
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

			matches, err := digest.CompareDigest(
				context.Background(),
				mockContainerEmptyDigests,
				"token",
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
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

			matches, err := digest.CompareDigest(
				context.Background(),
				mockContainerWithServer,
				"token",
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
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

			matches, err := digest.CompareDigest(
				context.Background(),
				mockContainerUnreachable,
				"token",
			)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("failed to execute challenge request"))
			gomega.Expect(matches).To(gomega.BeFalse())
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

			matches, err := digest.CompareDigest(
				context.Background(),
				mockContainerInvalidImage,
				"token",
			)
			gomega.Expect(err).To(gomega.HaveOccurred())
			// Expect GetToken failure as it catches the invalid reference first
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to get token"))
			gomega.Expect(matches).To(gomega.BeFalse())
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

			matches, err := digest.CompareDigest(
				context.Background(),
				mockContainerInvalidURL,
				"token",
			)
			gomega.Expect(err).To(gomega.HaveOccurred())
			// Expect GetToken failure as it catches the invalid reference first
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to get token"))
			gomega.Expect(matches).To(gomega.BeFalse())
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
					conn.Close() // Simulate network failure
				},
			)

			matches, err := digest.CompareDigest(
				context.Background(),
				mockContainerWithServer,
				"token",
			)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("HEAD request failed"))
			gomega.Expect(matches).To(gomega.BeFalse())
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

			matches, err := digest.CompareDigest(
				context.Background(),
				mockContainerWithServer,
				"token",
			)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("registry responded with invalid HEAD request"))
			gomega.Expect(matches).To(gomega.BeFalse())
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

			matches, err := digest.CompareDigest(
				context.Background(),
				mockContainerWithInvalidDigest,
				"token",
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(matches).To(gomega.BeFalse())
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3))
		})
	})

	ginkgo.When("using different registries", func() {
		ginkgo.It("should work with DockerHub",
			SkipIfCredentialsEmpty(DockerHubCredentials, func() {
				ginkgo.GinkgoT().
					Logf("DockerHubCredentials present: %v", DockerHubCredentials != nil)
			}),
		)

		ginkgo.It("should work with GitHub Container Registry",
			SkipIfCredentialsEmpty(GHCRCredentials, func() {
				ginkgo.GinkgoT().Logf("GHCRCredentials present: %v", GHCRCredentials != nil)
			}),
		)
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
					ghttp.VerifyRequest("HEAD", "/v2/test/image/manifests/latest"),
					ghttp.VerifyHeader(http.Header{
						"User-Agent": []string{"Watchtower/v0.0.0-unknown"},
					}),
					ghttp.RespondWith(http.StatusOK, nil, http.Header{
						digest.ContentDigestHeader: []string{mockDigestHash},
					}),
				),
			)

			mockContainerWithServer := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			matches, err := digest.CompareDigest(
				context.Background(),
				mockContainerWithServer,
				"token",
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
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

			result, err := digest.FetchDigest(
				context.Background(),
				mockContainerWithServer,
				"token",
			)
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
					conn.Close() // Simulate network failure
				},
			)

			result, err := digest.FetchDigest(
				context.Background(),
				mockContainerWithServer,
				"token",
			)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("GET request failed"))
			gomega.Expect(result).To(gomega.BeEmpty())
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
					time.Sleep(5 * time.Second) // Delay to exceed context timeout
					w.Write([]byte(`{"digest": "` + mockDigestHash + `"}`))
				},
			)

			origClient := auth.Client
			auth.Client = &http.Client{
				Transport: &http.Transport{
					TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
					DisableKeepAlives:     true,
					ResponseHeaderTimeout: 50 * time.Millisecond,
				},
			}
			defer func() {
				logrus.Debug("Restoring original client")
				auth.Client = origClient
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			result, err := digest.FetchDigest(ctx, mockContainerWithServer, "token")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("GET request failed"))
			gomega.Expect(result).To(gomega.BeEmpty())
		})

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

			result, err := digest.FetchDigest(
				context.Background(),
				mockContainerUnreachable,
				"token",
			)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to get token"))
			gomega.Expect(result).To(gomega.BeEmpty())
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

			result, err := digest.FetchDigest(
				context.Background(),
				mockContainerInvalidImage,
				"token",
			)
			gomega.Expect(err).To(gomega.HaveOccurred())
			// Expect GetToken failure as it catches the invalid reference first
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to get token"))
			gomega.Expect(result).To(gomega.BeEmpty())
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

			result, err := digest.FetchDigest(
				context.Background(),
				mockContainerInvalidURL,
				"token",
			)
			gomega.Expect(err).To(gomega.HaveOccurred())
			// Expect GetToken failure as it catches the invalid reference first
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to get token"))
			gomega.Expect(result).To(gomega.BeEmpty())
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
					w.Write([]byte("invalid-json")) // Invalid JSON to trigger decode failure
				},
			)

			result, err := digest.FetchDigest(
				context.Background(),
				mockContainerWithServer,
				"token",
			)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("failed to decode manifest response"))
			gomega.Expect(result).To(gomega.BeEmpty())
		})
	})
})

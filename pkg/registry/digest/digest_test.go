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
	"os"
	"testing"
	"time"

	"github.com/docker/docker/api/types/image"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"

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
	// invalidImageRef is used to trigger parsing errors in tests.
	invalidImageRef = "invalid:image:ref:!!"
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
			// Use CreateMockContainerWithImageInfoP with an empty RepoDigests slice
			mockContainerEmptyDigests := mocks.CreateMockContainerWithImageInfoP(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				&image.InspectResponse{RepoDigests: []string{}}, // Empty RepoDigests
			)

			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v2/"),
					ghttp.RespondWith(http.StatusUnauthorized, nil, http.Header{
						"WWW-Authenticate": []string{fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr)},
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

			origClient := auth.Client
			auth.Client = &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
				},
			}
			defer func() { auth.Client = origClient }()

			matches, err := digest.CompareDigest(context.Background(), mockContainerEmptyDigests, "token")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(matches).To(gomega.BeFalse()) // No digests to compare
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
						"WWW-Authenticate": []string{fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr)},
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

			origClient := auth.Client
			auth.Client = &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Skip verification for self-signed cert
				},
			}
			defer func() { auth.Client = origClient }()

			matches, err := digest.CompareDigest(context.Background(), mockContainerWithServer, "token")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(matches).To(gomega.BeFalse())
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3)) // GET /v2/, GET /token, HEAD
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

			matches, err := digest.CompareDigest(context.Background(), mockContainerUnreachable, "token")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to execute challenge request"))
			gomega.Expect(matches).To(gomega.BeFalse())
		})

		ginkgo.It("should return an error when container contains no image info", func() {
			matches, err := digest.CompareDigest(context.Background(), mockContainerNoImage, "user:pass")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(matches).To(gomega.BeFalse())
		})

		ginkgo.It("should return an error if manifest URL build fails", func() {
			// Use an invalid image name to trigger manifest URL error
			mockContainerInvalidImage := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				invalidImageRef,
				mockCreated,
				mockDigest,
			)

			matches, err := digest.CompareDigest(context.Background(), mockContainerInvalidImage, "token")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to parse image name"))
			gomega.Expect(matches).To(gomega.BeFalse())
		})

		ginkgo.It("should return an error if HEAD request creation fails", func() {
			// Use a malformed URL to trigger request creation error in GetToken first
			mockImageRef := "\x00://invalid-url/test/image:latest" // Invalid URL with null byte
			mockContainerInvalidURL := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			matches, err := digest.CompareDigest(context.Background(), mockContainerInvalidURL, "token")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to parse image name"))
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

			// Mock server responses: challenge, token, then HEAD without digest
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v2/"),
					ghttp.RespondWith(http.StatusUnauthorized, nil, http.Header{
						"WWW-Authenticate": []string{fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr)},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/token"),
					ghttp.RespondWith(http.StatusOK, `{"token": "mock-token"}`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("HEAD", "/v2/test/image/manifests/latest"),
					ghttp.RespondWith(http.StatusOK, nil, http.Header{
						"Www-Authenticate": []string{"Bearer realm=invalid"}, // Simulate auth header
						// No ContentDigestHeader
					}),
				),
			)

			origClient := auth.Client
			auth.Client = &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Skip verification for self-signed cert
				},
			}
			defer func() { auth.Client = origClient }()

			matches, err := digest.CompareDigest(context.Background(), mockContainerWithServer, "token")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("registry responded with invalid HEAD request"))
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
				mockInvalidDigest, // Malformed digest
			)

			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v2/"),
					ghttp.RespondWith(http.StatusUnauthorized, nil, http.Header{
						"WWW-Authenticate": []string{fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr)},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/token"),
					ghttp.RespondWith(http.StatusOK, `{"token": "mock-token"}`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("HEAD", "/v2/test/image/manifests/latest"),
					ghttp.RespondWith(http.StatusOK, nil, http.Header{
						digest.ContentDigestHeader: []string{mockDifferentDigest}, // Different digest
					}),
				),
			)

			origClient := auth.Client
			auth.Client = &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Skip verification for self-signed cert
				},
			}
			defer func() { auth.Client = origClient }()

			matches, err := digest.CompareDigest(context.Background(), mockContainerWithInvalidDigest, "token")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(matches).To(gomega.BeFalse()) // No match due to malformed digest
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3))
		})
	})

	ginkgo.When("using different registries", func() {
		ginkgo.It("should work with DockerHub",
			SkipIfCredentialsEmpty(DockerHubCredentials, func() {
				ginkgo.GinkgoT().Logf("DockerHubCredentials present: %v", DockerHubCredentials != nil)
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

			// Temporarily set UserAgent for test consistency
			origUserAgent := digest.UserAgent
			digest.UserAgent = "Watchtower/v0.0.0-unknown"
			defer func() { digest.UserAgent = origUserAgent }()

			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v2/"),
					ghttp.RespondWith(http.StatusUnauthorized, nil, http.Header{
						"WWW-Authenticate": []string{fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr)},
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

			origClient := auth.Client
			auth.Client = &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Skip verification for self-signed cert
				},
			}
			defer func() { auth.Client = origClient }()

			matches, err := digest.CompareDigest(context.Background(), mockContainerWithServer, "token")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(matches).To(gomega.BeTrue())
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3)) // GET /v2/, GET /token, HEAD
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
		var server *ghttp.Server

		ginkgo.BeforeEach(func() {
			defer ginkgo.GinkgoRecover()
			server = ghttp.NewTLSServer()
		})

		ginkgo.AfterEach(func() {
			defer ginkgo.GinkgoRecover()
			server.Close()
		})

		ginkgo.It("should fetch a digest successfully", func() {
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
						"WWW-Authenticate": []string{fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr)},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/token"),
					ghttp.RespondWith(http.StatusOK, `{"token": "mock-token"}`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v2/test/image/manifests/latest"),
					ghttp.RespondWith(http.StatusOK, `{"digest": "`+mockDigestHash+`"}`),
				),
			)

			origClient := auth.Client
			auth.Client = &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Skip verification for self-signed cert
				},
			}
			defer func() { auth.Client = origClient }()

			result, err := digest.FetchDigest(context.Background(), mockContainerWithServer, "token")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.Equal(helpers.NormalizeDigest(mockDigestHash)))
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3)) // GET /v2/, GET /token, GET manifest
		})

		ginkgo.It("should return an error if GetToken fails", func() {
			defer ginkgo.GinkgoRecover()
			// Use an invalid image name to trigger GetToken failure
			mockContainerInvalidImage := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				invalidImageRef,
				mockCreated,
				mockDigest,
			)

			result, err := digest.FetchDigest(context.Background(), mockContainerInvalidImage, "token")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to get token"))
			gomega.Expect(result).To(gomega.BeEmpty())
		})

		ginkgo.It("should return an error if GET request fails after token", func() {
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
						"WWW-Authenticate": []string{fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr)},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/token"),
					ghttp.RespondWith(http.StatusOK, `{"token": "mock-token"}`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v2/test/image/manifests/latest"),
					func(w http.ResponseWriter, _ *http.Request) {
						// Simulate a network failure by closing the connection
						conn, _, err := w.(http.Hijacker).Hijack() //nolint:forcetypeassert
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
						conn.Close()
					},
				),
			)

			origClient := auth.Client
			auth.Client = &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Skip verification for self-signed cert
				},
			}
			defer func() { auth.Client = origClient }()

			result, err := digest.FetchDigest(context.Background(), mockContainerWithServer, "token")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("GET request failed"))
			gomega.Expect(result).To(gomega.BeEmpty())
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3))
		})

		ginkgo.It("should return an error if manifest URL build fails", func() {
			defer ginkgo.GinkgoRecover()
			// Use an invalid image name to trigger manifest URL error
			mockContainerInvalidImage := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				invalidImageRef,
				mockCreated,
				mockDigest,
			)

			result, err := digest.FetchDigest(context.Background(), mockContainerInvalidImage, "token")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to parse image name"))
			gomega.Expect(result).To(gomega.BeEmpty())
		})

		ginkgo.It("should return an error if GET request creation fails", func() {
			defer ginkgo.GinkgoRecover()
			// Use a malformed URL to trigger request creation error
			mockImageRef := "\x00://invalid-url/test/image:latest"
			mockContainerInvalidURL := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			result, err := digest.FetchDigest(context.Background(), mockContainerInvalidURL, "token")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to parse image name"))
			gomega.Expect(result).To(gomega.BeEmpty())
		})

		ginkgo.It("should return an error if GET request fails", func() {
			defer ginkgo.GinkgoRecover()
			mockImageRef := "unreachable.local/test/image:latest"
			mockContainerUnreachable := mocks.CreateMockContainerWithDigest(
				mockID,
				mockName,
				mockImageRef,
				mockCreated,
				mockDigest,
			)

			result, err := digest.FetchDigest(context.Background(), mockContainerUnreachable, "token")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to execute challenge request"))
			gomega.Expect(result).To(gomega.BeEmpty())
		})

		ginkgo.It("should return an error if response decoding fails", func() {
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
						"WWW-Authenticate": []string{fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, serverAddr)},
					}),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/token"),
					ghttp.RespondWith(http.StatusOK, `{"token": "mock-token"}`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v2/test/image/manifests/latest"),
					ghttp.RespondWith(http.StatusOK, "invalid-json"), // Malformed JSON
				),
			)

			origClient := auth.Client
			auth.Client = &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Skip verification for self-signed cert
				},
			}
			defer func() { auth.Client = origClient }()

			result, err := digest.FetchDigest(context.Background(), mockContainerWithServer, "token")
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to decode manifest response"))
			gomega.Expect(result).To(gomega.BeEmpty())
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(3))
		})
	})
})

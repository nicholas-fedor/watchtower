package digest_test

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/registry/digest"
	wtTypes "github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

func TestDigest(t *testing.T) {

	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(ginkgo.GinkgoT(), "Digest Suite")
}

var (
	DockerHubCredentials = &wtTypes.RegistryCredentials{
		Username: os.Getenv("CI_INTEGRATION_TEST_REGISTRY_DH_USERNAME"),
		Password: os.Getenv("CI_INTEGRATION_TEST_REGISTRY_DH_PASSWORD"),
	}
	GHCRCredentials = &wtTypes.RegistryCredentials{
		Username: os.Getenv("CI_INTEGRATION_TEST_REGISTRY_GH_USERNAME"),
		Password: os.Getenv("CI_INTEGRATION_TEST_REGISTRY_GH_PASSWORD"),
	}
)

func SkipIfCredentialsEmpty(credentials *wtTypes.RegistryCredentials, fn func()) func() {
	if credentials.Username == "" {
		return func() {
			ginkgo.Skip("Username missing. Skipping integration test")
		}
	} else if credentials.Password == "" {
		return func() {
			ginkgo.Skip("Password missing. Skipping integration test")
		}
	} else {
		return fn
	}
}

var _ = ginkgo.Describe("Digests", func() {
	mockId := "mock-id"
	mockName := "mock-container"
	mockImage := "ghcr.io/k6io/operator:latest"
	mockCreated := time.Now()
	mockDigest := "ghcr.io/k6io/operator@sha256:d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547"

	mockContainer := mocks.CreateMockContainerWithDigest(
		mockId,
		mockName,
		mockImage,
		mockCreated,
		mockDigest)

	mockContainerNoImage := mocks.CreateMockContainerWithImageInfoP(mockId, mockName, mockImage, mockCreated, nil)

	ginkgo.When("a digest comparison is done", func() {
		ginkgo.It("should return true if digests match",
			SkipIfCredentialsEmpty(GHCRCredentials, func() {
				creds := fmt.Sprintf("%s:%s", GHCRCredentials.Username, GHCRCredentials.Password)
				matches, err := digest.CompareDigest(mockContainer, creds)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(matches).To(gomega.Equal(true))
			}),
		)

		ginkgo.It("should return false if digests differ", func() {

		})
		ginkgo.It("should return an error if the registry isn't available", func() {

		})
		ginkgo.It("should return an error when container contains no image info", func() {
			matches, err := digest.CompareDigest(mockContainerNoImage, `user:pass`)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(matches).To(gomega.Equal(false))
		})
	})
	ginkgo.When("using different registries", func() {
		ginkgo.It("should work with DockerHub",
			SkipIfCredentialsEmpty(DockerHubCredentials, func() {
				fmt.Println(DockerHubCredentials != nil) // to avoid crying linters
			}),
		)
		ginkgo.It("should work with GitHub Container Registry",
			SkipIfCredentialsEmpty(GHCRCredentials, func() {
				fmt.Println(GHCRCredentials != nil) // to avoid crying linters
			}),
		)
	})
	ginkgo.When("sending a HEAD request", func() {
		var server *ghttp.Server
		ginkgo.BeforeEach(func() {
			server = ghttp.NewServer()
		})
		ginkgo.AfterEach(func() {
			server.Close()
		})
		ginkgo.It("should use a custom user-agent", func() {
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyHeader(http.Header{
						"User-Agent": []string{"Watchtower/v0.0.0-unknown"},
					}),
					ghttp.RespondWith(http.StatusOK, "", http.Header{
						digest.ContentDigestHeader: []string{
							mockDigest,
						},
					}),
				),
			)
			dig, err := digest.GetDigest(server.URL(), "token")
			gomega.Expect(server.ReceivedRequests()).Should(gomega.HaveLen(1))
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(dig).To(gomega.Equal(mockDigest))
		})
	})
})

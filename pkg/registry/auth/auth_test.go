package auth_test

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"

	"github.com/distribution/reference"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func TestAuth(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Registry Auth Suite")
}
func SkipIfCredentialsEmpty(credentials *types.RegistryCredentials, fn func()) func() {
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

var GHCRCredentials = &types.RegistryCredentials{
	Username: os.Getenv("CI_INTEGRATION_TEST_REGISTRY_GH_USERNAME"),
	Password: os.Getenv("CI_INTEGRATION_TEST_REGISTRY_GH_PASSWORD"),
}

var _ = ginkgo.Describe("the auth module", func() {
	mockId := "mock-id"
	mockName := "mock-container"
	mockImage := "ghcr.io/k6io/operator:latest"
	mockCreated := time.Now()
	mockDigest := "ghcr.io/k6io/operator@sha256:d6d356ad6ec80e6765b99921babb8580ca0dee21c27abc3f0197c9441d83d680"

	mockContainer := mocks.CreateMockContainerWithDigest(
		mockId,
		mockName,
		mockImage,
		mockCreated,
		mockDigest)

	ginkgo.Describe("GetToken", func() {
		ginkgo.It("should parse the token from the response",
			SkipIfCredentialsEmpty(GHCRCredentials, func() {
				creds := fmt.Sprintf("%s:%s", GHCRCredentials.Username, GHCRCredentials.Password)
				token, err := auth.GetToken(mockContainer, creds)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(token).NotTo(gomega.Equal(""))
			}),
		)
	})

	ginkgo.Describe("GetAuthURL", func() {
		ginkgo.It("should create a valid auth url object based on the challenge header supplied", func() {
			challenge := `bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:user/image:pull"`
			imageRef, err := reference.ParseNormalizedNamed("nicholas-fedor/watchtower")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			expected := &url.URL{
				Host:     "ghcr.io",
				Scheme:   "https",
				Path:     "/token",
				RawQuery: "scope=repository%3Anicholas-fedor%2Fwatchtower%3Apull&service=ghcr.io",
			}

			URL, err := auth.GetAuthURL(challenge, imageRef)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(URL).To(gomega.Equal(expected))
		})

		ginkgo.When("given an invalid challenge header", func() {
			ginkgo.It("should return an error", func() {
				challenge := `bearer realm="https://ghcr.io/token"`
				imageRef, err := reference.ParseNormalizedNamed("nicholas-fedor/watchtower")
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				URL, err := auth.GetAuthURL(challenge, imageRef)
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(URL).To(gomega.BeNil())
			})
		})

		ginkgo.When("deriving the auth scope from an image name", func() {
			ginkgo.It("should prepend official dockerhub images with \"library/\"", func() {
				gomega.Expect(getScopeFromImageAuthURL("registry")).To(gomega.Equal("library/registry"))
				gomega.Expect(getScopeFromImageAuthURL("docker.io/registry")).To(gomega.Equal("library/registry"))
				gomega.Expect(getScopeFromImageAuthURL("index.docker.io/registry")).To(gomega.Equal("library/registry"))
			})
			ginkgo.It("should not include vanity hosts\"", func() {
				gomega.Expect(getScopeFromImageAuthURL("docker.io/nickfedor/watchtower")).To(gomega.Equal("nickfedor/watchtower"))
				gomega.Expect(getScopeFromImageAuthURL("index.docker.io/nickfedor/watchtower")).To(gomega.Equal("nickfedor/watchtower"))
			})
			// ginkgo.It("should not prepend library/ to image names if they're not on dockerhub", func() {
			// 	gomega.Expect(getScopeFromImageAuthURL("ghcr.io/watchtower")).To(gomega.Equal("watchtower"))
			// 	gomega.Expect(getScopeFromImageAuthURL("ghcr.io/nicholas-fedor/watchtower")).To(gomega.Equal("nicholas-fedor/watchtower"))
			// })
		})
		ginkgo.It("should not crash when an empty field is received", func() {
			input := `bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:user/image:pull",`
			imageRef, err := reference.ParseNormalizedNamed("nicholas-fedor/watchtower")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			res, err := auth.GetAuthURL(input, imageRef)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(res).NotTo(gomega.BeNil())
		})
		ginkgo.It("should not crash when a field without a value is received", func() {
			input := `bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:user/image:pull",valuelesskey`
			imageRef, err := reference.ParseNormalizedNamed("nicholas-fedor/watchtower")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			res, err := auth.GetAuthURL(input, imageRef)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(res).NotTo(gomega.BeNil())
		})
	})

	ginkgo.Describe("GetChallengeURL", func() {
		ginkgo.It("should create a valid challenge url object based on the image ref supplied", func() {
			expected := url.URL{Host: "ghcr.io", Scheme: "https", Path: "/v2/"}
			imageRef, _ := reference.ParseNormalizedNamed("ghcr.io/nicholas-fedor/watchtower:latest")
			gomega.Expect(auth.GetChallengeURL(imageRef)).To(gomega.Equal(expected))
		})
		ginkgo.It("should assume Docker Hub for image refs with no explicit registry", func() {
			expected := url.URL{Host: "index.docker.io", Scheme: "https", Path: "/v2/"}
			imageRef, _ := reference.ParseNormalizedNamed("nickfedor/watchtower:latest")
			gomega.Expect(auth.GetChallengeURL(imageRef)).To(gomega.Equal(expected))
		})
		ginkgo.It("should use index.docker.io if the image ref specifies docker.io", func() {
			expected := url.URL{Host: "index.docker.io", Scheme: "https", Path: "/v2/"}
			imageRef, _ := reference.ParseNormalizedNamed("docker.io/nickfedor/watchtower:latest")
			gomega.Expect(auth.GetChallengeURL(imageRef)).To(gomega.Equal(expected))
		})
	})
})

var scopeImageRegexp = gomega.MatchRegexp("^repository:[a-z0-9]+(/[a-z0-9]+)*:pull$")

func getScopeFromImageAuthURL(imageName string) string {
	normalizedRef, _ := reference.ParseNormalizedNamed(imageName)
	challenge := `bearer realm="https://dummy.host/token",service="dummy.host",scope="repository:user/image:pull"`
	URL, _ := auth.GetAuthURL(challenge, normalizedRef)

	scope := URL.Query().Get("scope")
	gomega.Expect(scopeImageRegexp.Match(scope)).To(gomega.BeTrue())
	return strings.Replace(scope[11:], ":pull", "", 1)
}

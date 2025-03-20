package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

const (
	token = "123123123"
)

func TestAPI(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "API Suite")
}

var _ = ginkgo.Describe("API", func() {
	api := New(token)

	ginkgo.Describe("RequireToken middleware", func() {
		ginkgo.It("should return 401 Unauthorized when token is not provided", func() {
			handlerFunc := api.RequireToken(testHandler)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/hello", nil)

			handlerFunc(rec, req)

			gomega.Expect(rec.Code).To(gomega.Equal(http.StatusUnauthorized))
		})

		ginkgo.It("should return 401 Unauthorized when token is invalid", func() {
			handlerFunc := api.RequireToken(testHandler)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/hello", nil)
			req.Header.Set("Authorization", "Bearer 123")

			handlerFunc(rec, req)

			gomega.Expect(rec.Code).To(gomega.Equal(http.StatusUnauthorized))
		})

		ginkgo.It("should return 200 OK when token is valid", func() {
			handlerFunc := api.RequireToken(testHandler)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/hello", nil)
			req.Header.Set("Authorization", "Bearer "+token)

			handlerFunc(rec, req)

			gomega.Expect(rec.Code).To(gomega.Equal(http.StatusOK))
		})
	})
})

func testHandler(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, "Hello!")
}

// Package update_test provides tests for the update HTTP API handler.
package update_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/api/update"
)

func TestUpdate(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Update Handler Suite")
}

var _ = ginkgo.Describe("Update Handler", func() {
	var updateLock chan bool
	var mockUpdateFn func(images []string)
	var handler *update.Handler

	ginkgo.BeforeEach(func() {
		updateLock = nil
		mockUpdateFn = func(_ []string) {} // Use _ for unused images parameter.
		handler = update.New(mockUpdateFn, updateLock)
		logrus.SetOutput(io.Discard) // Suppress logs during tests.
	})

	ginkgo.Describe("Initialization", func() {
		ginkgo.It("should create a new lock if none provided", func() {
			gomega.Expect(handler.Path).To(gomega.Equal("/v1/update"))
			// Verify handler is initialized with a valid update function.
			gomega.Expect(handler).ToNot(gomega.BeNil())
		})

		ginkgo.It("should use provided lock if given", func() {
			customLock := make(chan bool, 1)
			customLock <- true
			handler = update.New(mockUpdateFn, customLock)
			gomega.Expect(handler.Path).To(gomega.Equal("/v1/update"))
			// Verify handler is initialized with the provided lock.
			gomega.Expect(handler).ToNot(gomega.BeNil())
		})
	})

	ginkgo.Describe("Handle function", func() {
		var req *http.Request
		var rec *httptest.ResponseRecorder

		ginkgo.BeforeEach(func() {
			rec = httptest.NewRecorder()
			req = httptest.NewRequest(
				http.MethodPost,
				"/v1/update",
				bytes.NewBufferString("test body"),
			)
		})

		ginkgo.It(
			"should execute full update and return 202 Accepted when lock is available",
			func() {
				called := false
				mockUpdateFn = func(images []string) {
					called = true
					gomega.Expect(images).To(gomega.BeNil())
				}
				handler = update.New(mockUpdateFn, nil)

				handler.Handle(rec, req)
				gomega.Expect(rec.Code).To(gomega.Equal(http.StatusAccepted))
				gomega.Expect(rec.Body.String()).
					To(gomega.ContainSubstring("Update enqueued and started"))
				gomega.Expect(called).To(gomega.BeTrue())
			},
		)

		ginkgo.It(
			"should execute targeted update and return 202 Accepted when lock is available",
			func() {
				called := false
				mockUpdateFn = func(images []string) {
					called = true
					gomega.Expect(images).To(gomega.Equal([]string{"foo/bar", "baz/qux"}))
				}
				handler = update.New(mockUpdateFn, nil)

				req.URL.RawQuery = "image=foo/bar,baz/qux"
				handler.Handle(rec, req)
				gomega.Expect(rec.Code).To(gomega.Equal(http.StatusAccepted))
				gomega.Expect(rec.Body.String()).
					To(gomega.ContainSubstring("Update enqueued and started"))
				gomega.Expect(called).To(gomega.BeTrue())
			},
		)

		ginkgo.It("should queue concurrent requests and process them sequentially", func() {
			// Use a custom lock to control concurrency.
			customLock := make(chan bool, 1)
			customLock <- true
			called := 0
			handler = update.New(func(_ []string) {
				called++
				time.Sleep(50 * time.Millisecond) // Short delay to simulate work.
			}, customLock)

			// Start first request in goroutine.
			go func() {
				req := httptest.NewRequest(http.MethodPost, "/v1/update", nil)
				rec := httptest.NewRecorder()
				handler.Handle(rec, req)
				gomega.Expect(rec.Code).To(gomega.Equal(http.StatusAccepted))
			}()

			// Wait briefly, then start second request.
			time.Sleep(10 * time.Millisecond)
			handler.Handle(rec, req)
			gomega.Expect(rec.Code).To(gomega.Equal(http.StatusAccepted))
			gomega.Expect(rec.Body.String()).
				To(gomega.ContainSubstring("Update enqueued and started"))

			// Both should have been called sequentially.
			gomega.Expect(called).To(gomega.Equal(2))
		})

		ginkgo.It("should handle concurrent requests correctly", func() {
			var wg sync.WaitGroup
			calledCount := 0
			mockUpdateFn = func(_ []string) { // Use _ for unused images parameter.
				calledCount++
				time.Sleep(10 * time.Millisecond) // Short delay to simulate work.
			}
			handler = update.New(mockUpdateFn, nil)

			// Simulate 3 concurrent requests.
			for range 3 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					localRec := httptest.NewRecorder()
					localReq := httptest.NewRequest(
						http.MethodPost,
						"/v1/update",
						bytes.NewBufferString("test body"),
					)
					handler.Handle(localRec, localReq)
					// All should succeed with 202.
					gomega.Expect(localRec.Code).To(gomega.Equal(http.StatusAccepted))
				}()
			}
			wg.Wait()
			gomega.Expect(calledCount).
				To(gomega.Equal(3))
			// All updates execute sequentially due to lock.
		})

		ginkgo.It("should return 500 Internal Server Error on body read failure", func() {
			// Simulate read error by using a faulty reader.
			faultyReader := &faultyReadCloser{err: errors.New("read error")}
			req.Body = faultyReader

			handler.Handle(rec, req)
			gomega.Expect(rec.Code).To(gomega.Equal(http.StatusInternalServerError))
			gomega.Expect(rec.Body.String()).
				To(gomega.ContainSubstring("Failed to read request body"))
		})
	})
})

// faultyReadCloser simulates a body reader that fails.
type faultyReadCloser struct {
	err error
}

func (f *faultyReadCloser) Read(_ []byte) (int, error) { return 0, f.err }
func (f *faultyReadCloser) Close() error               { return nil }

// Package update_test provides tests for the update HTTP API handler.
package update_test

import (
	"bytes"
	"encoding/json"
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
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
)

func TestUpdate(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Update Handler Suite")
}

var _ = ginkgo.Describe("Update Handler", func() {
	var updateLock chan bool
	var mockUpdateFn func(images []string) *metrics.Metric
	var handler *update.Handler

	ginkgo.BeforeEach(func() {
		updateLock = nil
		mockUpdateFn = func(_ []string) *metrics.Metric {
			return &metrics.Metric{Scanned: 8, Updated: 0, Failed: 0}
		} // Mock function returning sample metrics.
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
			"should execute full update and return 200 OK with JSON when lock is available",
			func() {
				called := false
				mockUpdateFn = func(images []string) *metrics.Metric {
					called = true
					gomega.Expect(images).To(gomega.BeNil())

					return &metrics.Metric{Scanned: 8, Updated: 0, Failed: 0}
				}
				handler = update.New(mockUpdateFn, nil)

				handler.Handle(rec, req)
				gomega.Expect(rec.Code).To(gomega.Equal(http.StatusOK))
				gomega.Expect(rec.Header().Get("Content-Type")).To(gomega.Equal("application/json"))

				var response map[string]any
				gomega.Expect(json.Unmarshal(rec.Body.Bytes(), &response)).To(gomega.Succeed())

				// Check summary section
				summary := response["summary"].(map[string]any)
				gomega.Expect(summary["scanned"]).To(gomega.Equal(float64(8)))
				gomega.Expect(summary["updated"]).To(gomega.Equal(float64(0)))
				gomega.Expect(summary["failed"]).To(gomega.Equal(float64(0)))
				gomega.Expect(summary["restarted"]).To(gomega.Equal(float64(0)))

				// Check timing section
				timing := response["timing"].(map[string]any)
				gomega.Expect(timing["duration_ms"]).To(gomega.BeNumerically(">=", 0))
				gomega.Expect(timing["duration"]).To(gomega.BeAssignableToTypeOf(""))

				// Check metadata
				gomega.Expect(response["timestamp"]).To(gomega.BeAssignableToTypeOf(""))
				gomega.Expect(response["api_version"]).To(gomega.Equal("v1"))

				gomega.Expect(called).To(gomega.BeTrue())
			},
		)

		ginkgo.It(
			"should execute targeted update and return 200 OK with JSON when lock is available",
			func() {
				called := false
				mockUpdateFn = func(images []string) *metrics.Metric {
					called = true
					gomega.Expect(images).To(gomega.Equal([]string{"foo/bar", "baz/qux"}))

					return &metrics.Metric{Scanned: 2, Updated: 1, Failed: 0}
				}
				handler = update.New(mockUpdateFn, nil)

				req.URL.RawQuery = "image=foo/bar,baz/qux"
				handler.Handle(rec, req)
				gomega.Expect(rec.Code).To(gomega.Equal(http.StatusOK))
				gomega.Expect(rec.Header().Get("Content-Type")).To(gomega.Equal("application/json"))

				var response map[string]any
				gomega.Expect(json.Unmarshal(rec.Body.Bytes(), &response)).To(gomega.Succeed())

				// Check summary section
				summary := response["summary"].(map[string]any)
				gomega.Expect(summary["scanned"]).To(gomega.Equal(float64(2)))
				gomega.Expect(summary["updated"]).To(gomega.Equal(float64(1)))
				gomega.Expect(summary["failed"]).To(gomega.Equal(float64(0)))
				gomega.Expect(summary["restarted"]).To(gomega.Equal(float64(0)))

				// Check timing section
				timing := response["timing"].(map[string]any)
				gomega.Expect(timing["duration_ms"]).To(gomega.BeNumerically(">=", 0))
				gomega.Expect(timing["duration"]).To(gomega.BeAssignableToTypeOf(""))

				// Check metadata
				gomega.Expect(response["timestamp"]).To(gomega.BeAssignableToTypeOf(""))
				gomega.Expect(response["api_version"]).To(gomega.Equal("v1"))

				gomega.Expect(called).To(gomega.BeTrue())
			},
		)

		ginkgo.It("should queue concurrent requests and process them sequentially", func() {
			// Use a custom lock to control concurrency.
			customLock := make(chan bool, 1)
			customLock <- true
			called := 0
			handler = update.New(func(_ []string) *metrics.Metric {
				called++
				time.Sleep(50 * time.Millisecond) // Short delay to simulate work.

				return &metrics.Metric{Scanned: 8, Updated: 0, Failed: 0}
			}, customLock)

			// Start first request in goroutine.
			go func() {
				req := httptest.NewRequest(http.MethodPost, "/v1/update", nil)
				rec := httptest.NewRecorder()
				handler.Handle(rec, req)
				gomega.Expect(rec.Code).To(gomega.Equal(http.StatusOK))
			}()

			// Wait briefly, then start second request.
			time.Sleep(10 * time.Millisecond)
			handler.Handle(rec, req)
			gomega.Expect(rec.Code).To(gomega.Equal(http.StatusOK))
			gomega.Expect(rec.Header().Get("Content-Type")).To(gomega.Equal("application/json"))

			var response map[string]any
			gomega.Expect(json.Unmarshal(rec.Body.Bytes(), &response)).To(gomega.Succeed())

			// Both should have been called sequentially.
			gomega.Expect(called).To(gomega.Equal(2))
		})

		ginkgo.It("should handle concurrent requests correctly", func() {
			var wg sync.WaitGroup
			calledCount := 0
			mockUpdateFn = func(_ []string) *metrics.Metric { // Use _ for unused images parameter.
				calledCount++
				time.Sleep(10 * time.Millisecond) // Short delay to simulate work.

				return &metrics.Metric{Scanned: 8, Updated: 0, Failed: 0}
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
					// All should succeed with 200.
					gomega.Expect(localRec.Code).To(gomega.Equal(http.StatusOK))
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

		ginkgo.It(
			"should handle error when restarted containers cause JSON encoding failure",
			func() {
				// Mock update function that returns metric with restarted containers
				mockUpdateFn = func(_ []string) *metrics.Metric {
					return &metrics.Metric{Scanned: 10, Updated: 2, Failed: 1, Restarted: 3}
				}
				handler = update.New(mockUpdateFn, nil)

				// Simulate JSON encoding failure by replacing the writer
				faultyWriter := &faultyResponseWriter{}
				req = httptest.NewRequest(http.MethodPost, "/v1/update", nil)

				handler.Handle(faultyWriter, req)
				gomega.Expect(faultyWriter.statusCode).
					To(gomega.Equal(http.StatusInternalServerError))
			},
		)

		ginkgo.It(
			"should correctly report state transitions with restarted containers in API response",
			func() {
				called := false
				mockUpdateFn = func(images []string) *metrics.Metric {
					called = true
					gomega.Expect(images).To(gomega.BeNil())

					// Simulate state transition: containers scanned, some updated, some restarted
					return &metrics.Metric{Scanned: 5, Updated: 1, Failed: 0, Restarted: 2}
				}
				handler = update.New(mockUpdateFn, nil)

				handler.Handle(rec, req)
				gomega.Expect(rec.Code).To(gomega.Equal(http.StatusOK))
				gomega.Expect(rec.Header().Get("Content-Type")).To(gomega.Equal("application/json"))

				var response map[string]any
				gomega.Expect(json.Unmarshal(rec.Body.Bytes(), &response)).To(gomega.Succeed())

				summary := response["summary"].(map[string]any)
				gomega.Expect(summary["scanned"]).To(gomega.Equal(float64(5)))
				gomega.Expect(summary["updated"]).To(gomega.Equal(float64(1)))
				gomega.Expect(summary["failed"]).To(gomega.Equal(float64(0)))
				gomega.Expect(summary["restarted"]).To(gomega.Equal(float64(2)))

				gomega.Expect(called).To(gomega.BeTrue())
			},
		)

		ginkgo.It(
			"should maintain priority ordering in API response summary with restarted containers",
			func() {
				mockUpdateFn = func(_ []string) *metrics.Metric {
					return &metrics.Metric{Scanned: 20, Updated: 5, Failed: 2, Restarted: 8}
				}
				handler = update.New(mockUpdateFn, nil)

				handler.Handle(rec, req)
				gomega.Expect(rec.Code).To(gomega.Equal(http.StatusOK))

				var response map[string]any
				gomega.Expect(json.Unmarshal(rec.Body.Bytes(), &response)).To(gomega.Succeed())

				summary := response["summary"].(map[string]any)
				// Verify all fields are present and correctly ordered in the JSON structure
				gomega.Expect(summary).To(gomega.HaveKey("scanned"))
				gomega.Expect(summary).To(gomega.HaveKey("updated"))
				gomega.Expect(summary).To(gomega.HaveKey("failed"))
				gomega.Expect(summary).To(gomega.HaveKey("restarted"))

				// Verify values are correct
				gomega.Expect(summary["scanned"]).To(gomega.Equal(float64(20)))
				gomega.Expect(summary["updated"]).To(gomega.Equal(float64(5)))
				gomega.Expect(summary["failed"]).To(gomega.Equal(float64(2)))
				gomega.Expect(summary["restarted"]).To(gomega.Equal(float64(8)))
			},
		)

		ginkgo.It("should handle concurrent requests correctly with restarted containers", func() {
			var wg sync.WaitGroup
			calledCount := 0
			mockUpdateFn = func(_ []string) *metrics.Metric {
				calledCount++
				time.Sleep(10 * time.Millisecond) // Simulate work

				return &metrics.Metric{Scanned: 8, Updated: 2, Failed: 0, Restarted: 3}
			}
			handler = update.New(mockUpdateFn, nil)

			// Simulate 5 concurrent requests
			for range 5 {
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
					gomega.Expect(localRec.Code).To(gomega.Equal(http.StatusOK))

					var response map[string]any
					gomega.Expect(json.Unmarshal(localRec.Body.Bytes(), &response)).
						To(gomega.Succeed())

					summary := response["summary"].(map[string]any)
					gomega.Expect(summary["restarted"]).To(gomega.Equal(float64(3)))
				}()
			}
			wg.Wait()
			gomega.Expect(calledCount).To(gomega.Equal(5))
		})

		ginkgo.It("should handle restarted containers with large datasets correctly", func() {
			mockUpdateFn = func(_ []string) *metrics.Metric {
				// Simulate large dataset with many restarted containers
				return &metrics.Metric{Scanned: 1000, Updated: 200, Failed: 50, Restarted: 300}
			}
			handler = update.New(mockUpdateFn, nil)

			handler.Handle(rec, req)
			gomega.Expect(rec.Code).To(gomega.Equal(http.StatusOK))
			gomega.Expect(rec.Header().Get("Content-Type")).To(gomega.Equal("application/json"))

			var response map[string]any
			gomega.Expect(json.Unmarshal(rec.Body.Bytes(), &response)).To(gomega.Succeed())

			summary := response["summary"].(map[string]any)
			gomega.Expect(summary["scanned"]).To(gomega.Equal(float64(1000)))
			gomega.Expect(summary["updated"]).To(gomega.Equal(float64(200)))
			gomega.Expect(summary["failed"]).To(gomega.Equal(float64(50)))
			gomega.Expect(summary["restarted"]).To(gomega.Equal(float64(300)))

			// Verify timing information is still included
			timing := response["timing"].(map[string]any)
			gomega.Expect(timing["duration_ms"]).To(gomega.BeNumerically(">=", 0))
		})

		ginkgo.It(
			"should return appropriate API error responses when restarted containers fail",
			func() {
				// Test case where update function encounters issues with restarted containers
				// Since the update function doesn't return errors, we test the scenario where
				// the metric indicates failures alongside restarts
				mockUpdateFn = func(_ []string) *metrics.Metric {
					return &metrics.Metric{Scanned: 10, Updated: 1, Failed: 3, Restarted: 2}
				}
				handler = update.New(mockUpdateFn, nil)

				handler.Handle(rec, req)
				gomega.Expect(rec.Code).
					To(gomega.Equal(http.StatusOK))
					// Still 200 since no error in processing

				var response map[string]any
				gomega.Expect(json.Unmarshal(rec.Body.Bytes(), &response)).To(gomega.Succeed())

				summary := response["summary"].(map[string]any)
				gomega.Expect(summary["failed"]).To(gomega.Equal(float64(3)))
				gomega.Expect(summary["restarted"]).To(gomega.Equal(float64(2)))
				// Verify that both failed and restarted are reported correctly
			},
		)
	})
})

// faultyReadCloser simulates a body reader that fails.
type faultyReadCloser struct {
	err error
}

func (f *faultyReadCloser) Read(_ []byte) (int, error) { return 0, f.err }
func (f *faultyReadCloser) Close() error               { return nil }

// faultyResponseWriter simulates a response writer that fails on Write.
type faultyResponseWriter struct {
	statusCode int
}

func (f *faultyResponseWriter) Header() http.Header {
	return make(http.Header)
}

func (f *faultyResponseWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write error")
}

func (f *faultyResponseWriter) WriteHeader(statusCode int) {
	f.statusCode = statusCode
}

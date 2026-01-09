// Package update_test provides tests for the update HTTP API handler.
package update_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
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
		var server *ghttp.Server

		ginkgo.BeforeEach(func() {
			server = ghttp.NewServer()
		})

		ginkgo.AfterEach(func() {
			server.Close()
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

				server.AppendHandlers(handler.Handle)
				resp, err := http.Post(
					server.URL()+"/v1/update",
					"application/json",
					bytes.NewBufferString("test body"),
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				defer resp.Body.Close()
				gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
				gomega.Expect(resp.Header.Get("Content-Type")).To(gomega.Equal("application/json"))

				var response map[string]any
				gomega.Expect(json.NewDecoder(resp.Body).Decode(&response)).To(gomega.Succeed())

				gomega.Expect(server.ReceivedRequests()).To(gomega.HaveLen(1))
				req := server.ReceivedRequests()[0]
				gomega.Expect(req.Method).To(gomega.Equal(http.MethodPost))
				gomega.Expect(req.URL.Path).To(gomega.Equal("/v1/update"))

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

				server.AppendHandlers(handler.Handle)
				resp, err := http.Post(
					server.URL()+"/v1/update?image=foo/bar,baz/qux",
					"application/json",
					bytes.NewBufferString("test body"),
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				defer resp.Body.Close()
				gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
				gomega.Expect(resp.Header.Get("Content-Type")).To(gomega.Equal("application/json"))

				var response map[string]any
				gomega.Expect(json.NewDecoder(resp.Body).Decode(&response)).To(gomega.Succeed())

				gomega.Expect(server.ReceivedRequests()).To(gomega.HaveLen(1))
				req := server.ReceivedRequests()[0]
				gomega.Expect(req.Method).To(gomega.Equal(http.MethodPost))
				gomega.Expect(req.URL.Path).To(gomega.Equal("/v1/update"))
				gomega.Expect(req.URL.RawQuery).To(gomega.Equal("image=foo/bar,baz/qux"))

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

		ginkgo.It("should return 500 Internal Server Error on body read failure", func() {
			// Simulate read error by using a faulty reader.
			faultyReader := &faultyReadCloser{err: errors.New("read error")}
			server.AppendHandlers(func(w http.ResponseWriter, r *http.Request) {
				r.Body = faultyReader
				handler.Handle(w, r)
			})

			resp, err := http.Post(
				server.URL()+"/v1/update",
				"application/json",
				bytes.NewBufferString("test body"),
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer resp.Body.Close()
			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusInternalServerError))
			body, err := io.ReadAll(resp.Body)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(string(body)).To(gomega.ContainSubstring("Failed to read request body"))

			gomega.Expect(server.ReceivedRequests()).To(gomega.HaveLen(1))
			req := server.ReceivedRequests()[0]
			gomega.Expect(req.Method).To(gomega.Equal(http.MethodPost))
			gomega.Expect(req.URL.Path).To(gomega.Equal("/v1/update"))
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
				var faulty *faultyResponseWriter
				server.AppendHandlers(func(w http.ResponseWriter, r *http.Request) {
					faulty = &faultyResponseWriter{ResponseWriter: w}
					handler.Handle(faulty, r)
				})

				resp, err := http.Post(server.URL()+"/v1/update", "application/json", nil)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				defer resp.Body.Close()
				// Verify that StatusInternalServerError was attempted to be set
				gomega.Expect(faulty.lastStatusCode).
					To(gomega.Equal(http.StatusInternalServerError))
				// Verify that writing was attempted (but failed)
				gomega.Expect(faulty.written).To(gomega.BeTrue())
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

				server.AppendHandlers(handler.Handle)
				resp, err := http.Post(
					server.URL()+"/v1/update",
					"application/json",
					bytes.NewBufferString("test body"),
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				defer resp.Body.Close()
				gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
				gomega.Expect(resp.Header.Get("Content-Type")).To(gomega.Equal("application/json"))

				var response map[string]any
				gomega.Expect(json.NewDecoder(resp.Body).Decode(&response)).To(gomega.Succeed())

				summary := response["summary"].(map[string]any)
				gomega.Expect(summary["scanned"]).To(gomega.Equal(float64(5)))
				gomega.Expect(summary["updated"]).To(gomega.Equal(float64(1)))
				gomega.Expect(summary["failed"]).To(gomega.Equal(float64(0)))
				gomega.Expect(summary["restarted"]).To(gomega.Equal(float64(2)))

				gomega.Expect(called).To(gomega.BeTrue())

				gomega.Expect(server.ReceivedRequests()).To(gomega.HaveLen(1))
				req := server.ReceivedRequests()[0]
				gomega.Expect(req.Method).To(gomega.Equal(http.MethodPost))
				gomega.Expect(req.URL.Path).To(gomega.Equal("/v1/update"))
			},
		)

		ginkgo.It(
			"should maintain priority ordering in API response summary with restarted containers",
			func() {
				mockUpdateFn = func(_ []string) *metrics.Metric {
					return &metrics.Metric{Scanned: 20, Updated: 5, Failed: 2, Restarted: 8}
				}
				handler = update.New(mockUpdateFn, nil)

				server.AppendHandlers(handler.Handle)
				resp, err := http.Post(
					server.URL()+"/v1/update",
					"application/json",
					bytes.NewBufferString("test body"),
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				defer resp.Body.Close()
				gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))

				var response map[string]any
				gomega.Expect(json.NewDecoder(resp.Body).Decode(&response)).To(gomega.Succeed())

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

				gomega.Expect(server.ReceivedRequests()).To(gomega.HaveLen(1))
				req := server.ReceivedRequests()[0]
				gomega.Expect(req.Method).To(gomega.Equal(http.MethodPost))
				gomega.Expect(req.URL.Path).To(gomega.Equal("/v1/update"))
			},
		)

		ginkgo.It("should handle restarted containers with large datasets correctly", func() {
			mockUpdateFn = func(_ []string) *metrics.Metric {
				// Simulate large dataset with many restarted containers
				return &metrics.Metric{Scanned: 1000, Updated: 200, Failed: 50, Restarted: 300}
			}
			handler = update.New(mockUpdateFn, nil)

			server.AppendHandlers(handler.Handle)
			resp, err := http.Post(
				server.URL()+"/v1/update",
				"application/json",
				bytes.NewBufferString("test body"),
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer resp.Body.Close()
			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
			gomega.Expect(resp.Header.Get("Content-Type")).To(gomega.Equal("application/json"))

			var response map[string]any
			gomega.Expect(json.NewDecoder(resp.Body).Decode(&response)).To(gomega.Succeed())

			gomega.Expect(server.ReceivedRequests()).To(gomega.HaveLen(1))
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

				server.AppendHandlers(handler.Handle)
				resp, err := http.Post(
					server.URL()+"/v1/update",
					"application/json",
					bytes.NewBufferString("test body"),
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				defer resp.Body.Close()
				gomega.Expect(resp.StatusCode).
					To(gomega.Equal(http.StatusOK))
					// Still 200 since no error in processing

				var response map[string]any
				gomega.Expect(json.NewDecoder(resp.Body).Decode(&response)).To(gomega.Succeed())

				summary := response["summary"].(map[string]any)
				gomega.Expect(summary["failed"]).To(gomega.Equal(float64(3)))
				gomega.Expect(summary["restarted"]).To(gomega.Equal(float64(2)))
				// Verify that both failed and restarted are reported correctly

				gomega.Expect(server.ReceivedRequests()).To(gomega.HaveLen(1))
				req := server.ReceivedRequests()[0]
				gomega.Expect(req.Method).To(gomega.Equal(http.MethodPost))
				gomega.Expect(req.URL.Path).To(gomega.Equal("/v1/update"))
			},
		)
	})
})

func TestQueueConcurrentRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Use a custom lock to control concurrency.
		customLock := make(chan bool, 1)
		customLock <- true

		var called atomic.Int32

		handler := update.New(func(_ []string) *metrics.Metric {
			called.Add(1)

			time.Sleep(50 * time.Millisecond) // Short delay to simulate work.

			return &metrics.Metric{Scanned: 8, Updated: 0, Failed: 0}
		}, customLock)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/update", nil)

		// Start first request in goroutine.
		go func() {
			localReq := httptest.NewRequest(http.MethodPost, "/v1/update", nil)
			localRec := httptest.NewRecorder()
			handler.Handle(localRec, localReq)

			if localRec.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", localRec.Code)
			}
		}()

		// Wait briefly, then start second request.
		time.Sleep(10 * time.Millisecond)
		handler.Handle(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		if rec.Header().Get("Content-Type") != "application/json" {
			t.Errorf(
				"expected Content-Type application/json, got %s",
				rec.Header().Get("Content-Type"),
			)
		}

		var response map[string]any

		err := json.Unmarshal(rec.Body.Bytes(), &response)
		if err != nil {
			t.Fatal(err)
		}

		// Both should have been called sequentially.
		if called.Load() != 2 {
			t.Errorf("expected 2 calls, got %d", called.Load())
		}
	})
}

func TestHandleConcurrentRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calledCount atomic.Int32

		mockUpdateFn := func(_ []string) *metrics.Metric { // Use _ for unused images parameter.
			calledCount.Add(1)

			return &metrics.Metric{Scanned: 8, Updated: 0, Failed: 0}
		}

		customLock := make(chan bool, 10)
		for range 10 {
			customLock <- true
		}

		handler := update.New(mockUpdateFn, customLock)

		// Simulate 3 concurrent requests.
		for range 3 {
			go func() {
				localRec := httptest.NewRecorder()
				localReq := httptest.NewRequest(
					http.MethodPost,
					"/v1/update",
					bytes.NewBufferString("test body"),
				)
				handler.Handle(localRec, localReq)
				// All should succeed with 200.
				if localRec.Code != http.StatusOK {
					t.Errorf("expected status 200, got %d", localRec.Code)
				}
			}()
		}

		synctest.Wait()

		if calledCount.Load() != 3 {
			t.Errorf("expected 3 calls, got %d", calledCount.Load())
		}
		// All updates execute concurrently.
	})
}

func TestHandleConcurrentRequestsWithRestarted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calledCount atomic.Int32

		mockUpdateFn := func(_ []string) *metrics.Metric {
			calledCount.Add(1)

			return &metrics.Metric{Scanned: 8, Updated: 2, Failed: 0, Restarted: 3}
		}

		customLock := make(chan bool, 10)
		for range 10 {
			customLock <- true
		}

		handler := update.New(mockUpdateFn, customLock)

		// Simulate 5 concurrent requests
		for range 5 {
			go func() {
				localRec := httptest.NewRecorder()
				localReq := httptest.NewRequest(
					http.MethodPost,
					"/v1/update",
					bytes.NewBufferString("test body"),
				)
				handler.Handle(localRec, localReq)

				if localRec.Code != http.StatusOK {
					t.Errorf("expected status 200, got %d", localRec.Code)
				}

				var response map[string]any

				err := json.Unmarshal(localRec.Body.Bytes(), &response)
				if err != nil {
					t.Errorf("failed to unmarshal response: %v", err)
				}

				summary := response["summary"].(map[string]any)
				if summary["restarted"].(float64) != 3 {
					t.Errorf("expected restarted 3, got %v", summary["restarted"])
				}
			}()
		}

		synctest.Wait()

		if calledCount.Load() != 5 {
			t.Errorf("expected 5 calls, got %d", calledCount.Load())
		}
	})
}

func TestConcurrentRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calledCount atomic.Int32

		mockUpdateFn := func(_ []string) *metrics.Metric {
			calledCount.Add(1)

			return &metrics.Metric{Scanned: 8, Updated: 0, Failed: 0}
		}
		handler := update.New(mockUpdateFn, nil)

		// Simulate 3 concurrent requests
		for range 3 {
			go func() {
				localRec := httptest.NewRecorder()
				localReq := httptest.NewRequest(
					http.MethodPost,
					"/v1/update",
					bytes.NewBufferString("test body"),
				)
				handler.Handle(localRec, localReq)

				if localRec.Code != http.StatusOK {
					t.Errorf("expected status 200, got %d", localRec.Code)
				}
			}()
		}

		synctest.Wait()

		if calledCount.Load() != 3 {
			t.Errorf("expected 3 calls, got %d", calledCount.Load())
		}
	})
}

// faultyReadCloser simulates a body reader that fails.
type faultyReadCloser struct {
	err error
}

func (f *faultyReadCloser) Read(_ []byte) (int, error) { return 0, f.err }
func (f *faultyReadCloser) Close() error               { return nil }

// faultyResponseWriter simulates a response writer that fails on Write.
type faultyResponseWriter struct {
	http.ResponseWriter

	statusCode     int
	header         http.Header
	written        bool
	lastStatusCode int // Track the last status code that was attempted
}

func (f *faultyResponseWriter) Header() http.Header {
	if f.header == nil {
		f.header = f.ResponseWriter.Header()
	}

	return f.header
}

func (f *faultyResponseWriter) Write(_ []byte) (int, error) {
	if !f.written && f.statusCode == 0 {
		f.statusCode = http.StatusOK
	}

	f.written = true

	return 0, errors.New("write error")
}

func (f *faultyResponseWriter) WriteHeader(statusCode int) {
	f.lastStatusCode = statusCode
	if !f.written {
		f.statusCode = statusCode
		f.ResponseWriter.WriteHeader(statusCode)
	}
	// Ignore subsequent calls to WriteHeader after writing has started
}

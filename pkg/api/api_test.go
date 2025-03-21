// Package api_test provides external tests for the Watchtower HTTP API server.
package api_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nicholas-fedor/watchtower/pkg/api"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

// testToken is a constant token used for testing authentication.
const testToken = "123123123"

// TestAPI runs the Ginkgo test suite for the API package.
func TestAPI(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "API Suite")
}

var _ = ginkgo.Describe("API", func() {
	ginkgo.Describe("RequireToken middleware", func() {
		var apiInstance *api.API

		ginkgo.BeforeEach(func() {
			apiInstance = api.New(testToken)
		})

		ginkgo.It("should return 401 Unauthorized when token is not provided", func() {
			handlerFunc := apiInstance.RequireToken(testHandler)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/hello", nil)

			handlerFunc(rec, req)

			gomega.Expect(rec.Code).To(gomega.Equal(http.StatusUnauthorized))
		})

		ginkgo.It("should return 401 Unauthorized when token is invalid", func() {
			handlerFunc := apiInstance.RequireToken(testHandler)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/hello", nil)
			req.Header.Set("Authorization", "Bearer 123")

			handlerFunc(rec, req)

			gomega.Expect(rec.Code).To(gomega.Equal(http.StatusUnauthorized))
		})

		ginkgo.It("should return 200 OK when token is valid", func() {
			handlerFunc := apiInstance.RequireToken(testHandler)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/hello", nil)
			req.Header.Set("Authorization", "Bearer "+testToken)

			handlerFunc(rec, req)

			gomega.Expect(rec.Code).To(gomega.Equal(http.StatusOK))
			gomega.Expect(rec.Body.String()).To(gomega.Equal("Hello!"))
		})
	})

	ginkgo.Describe("API Start and Handler Registration", func() {
		var logBuffer *threadSafeBuffer

		ginkgo.BeforeEach(func() {
			logBuffer = &threadSafeBuffer{
				buf: &bytes.Buffer{},
				mu:  sync.Mutex{},
			}
			logrus.SetOutput(logBuffer)
			logrus.SetLevel(logrus.DebugLevel)
		})

		ginkgo.AfterEach(func() {
			logrus.SetOutput(nil)
			logrus.SetLevel(logrus.InfoLevel)
		})

		ginkgo.It("should skip starting the server when no handlers are registered", func() {
			apiInstance := api.New(testToken)
			err := apiInstance.Start(context.Background(), true)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Eventually(logBuffer.String, 100*time.Millisecond).Should(gomega.ContainSubstring("Watchtower HTTP API skipped."))
		})

		ginkgo.It("should fail with a fatal log when token is empty", func() {
			emptyTokenAPI := api.New("")
			emptyTokenAPI.RegisterFunc("/test", http.HandlerFunc(testHandler))

			var logOutput string
			logrus.SetOutput(&testLogWriter{
				buffer: []byte{},
				writeFunc: func(b []byte) (int, error) {
					logOutput = string(b)

					return len(b), nil
				},
			})
			defer logrus.SetOutput(nil)

			originalExit := logrus.StandardLogger().ExitFunc
			logrus.StandardLogger().ExitFunc = func(int) { panic("fatal exit") }
			defer func() { logrus.StandardLogger().ExitFunc = originalExit }()

			gomega.Expect(func() { _ = emptyTokenAPI.Start(context.Background(), true) }).To(gomega.Panic())
			gomega.Expect(logOutput).To(gomega.ContainSubstring("api token is empty or has not been set. exiting"))
		})

		testHandlerResponse := func(path, token string, expectedCode int) {
			apiInstance := api.New(testToken)
			mux := http.NewServeMux()
			if path[0] == '/' {
				mux.HandleFunc(path, apiInstance.RequireToken(http.HandlerFunc(testHandler)))
			} else {
				mux.Handle(path, apiInstance.RequireToken(http.HandlerFunc(testHandler)))
			}

			server := httptest.NewServer(mux)
			defer server.Close()

			req, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			gomega.Expect(resp.StatusCode).To(gomega.Equal(expectedCode))
			if expectedCode == http.StatusOK {
				gomega.Expect(string(body)).To(gomega.Equal("Hello!"))
			}
		}

		ginkgo.It("should register and serve requests via RegisterFunc synchronously", func() {
			testHandlerResponse("/test-sync", testToken, http.StatusOK)
		})

		ginkgo.It("should register and serve requests via RegisterFunc asynchronously", func() {
			testHandlerResponse("/test-async", testToken, http.StatusOK)
		})

		ginkgo.It("should register and serve requests via RegisterHandler with valid token", func() {
			testHandlerResponse("/test-handler", testToken, http.StatusOK)
		})

		ginkgo.It("should return 401 via RegisterHandler with invalid token", func() {
			testHandlerResponse("/test-handler-invalid", "wrongtoken", http.StatusUnauthorized)
		})

		ginkgo.It("should start server and serve requests via RegisterFunc", func() {
			// Select a random port and ensure it’s available
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), "Failed to create listener")
			defer listener.Close()

			tcpAddr, ok := listener.Addr().(*net.TCPAddr)
			gomega.Expect(ok).To(gomega.BeTrue(), "Listener address should be TCPAddr")
			port := tcpAddr.Port

			// Close listener immediately to free the port for the API server
			listener.Close()

			apiInstance := api.New(testToken)
			apiInstance.Addr = fmt.Sprintf("127.0.0.1:%d", port)
			apiInstance.RegisterFunc("/test-real", http.HandlerFunc(testHandler))

			// Use a very short timeout for server startup
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			errChan := make(chan error, 1)
			go func() {
				defer ginkgo.GinkgoRecover()
				logrus.Debugf("Starting API server on port %d", port)
				errChan <- apiInstance.Start(ctx, true)
				logrus.Debug("API server stopped")
			}()

			// Poll with an extremely tight timeout and interval
			gomega.Eventually(func() error {
				req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/test-real", port), nil)
				req.Header.Set("Authorization", "Bearer "+testToken)
				resp, requestErr := http.DefaultClient.Do(req) // Renamed err to requestErr to avoid shadowing
				if requestErr != nil {
					logrus.Debugf("Request failed: %v", requestErr)

					return requestErr
				}
				defer resp.Body.Close()
				logrus.Debug("Request succeeded")

				return nil
			}, 400*time.Millisecond, 5*time.Millisecond).Should(gomega.Succeed())

			// Perform the actual request
			req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/test-real", port), nil)
			req.Header.Set("Authorization", "Bearer "+testToken)
			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), "HTTP request should succeed")
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
			gomega.Expect(string(body)).To(gomega.Equal("Hello!"))

			// Explicitly cancel and wait for shutdown
			cancel()
			select {
			case err := <-errChan:
				gomega.Expect(err).ToNot(gomega.HaveOccurred(), "Server should stop cleanly")
			case <-time.After(500 * time.Millisecond):
				ginkgo.Fail("Timeout waiting for server to stop")
			}
		})

		ginkgo.It("should fail to start server on occupied port via runHTTPServer", func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintln(w, "Occupied")
			})
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), "Failed to create listener")
			defer listener.Close()

			tcpAddr, ok := listener.Addr().(*net.TCPAddr)
			gomega.Expect(ok).To(gomega.BeTrue(), "Listener address should be TCPAddr")
			port := tcpAddr.Port

			testServer := &http.Server{
				Addr:                         fmt.Sprintf("127.0.0.1:%d", port),
				Handler:                      mux,
				DisableGeneralOptionsHandler: false,
				ReadTimeout:                  10 * time.Second,
				WriteTimeout:                 10 * time.Second,
				IdleTimeout:                  30 * time.Second,
				ReadHeaderTimeout:            10 * time.Second,
				MaxHeaderBytes:               1 << 20,
				TLSConfig:                    nil,
				TLSNextProto:                 nil,
				ConnState:                    nil,
				ErrorLog:                     nil,
				BaseContext:                  func(_ net.Listener) context.Context { return context.Background() },
				ConnContext:                  nil,
				HTTP2:                        nil,
				Protocols:                    nil,
			}
			go func() {
				_ = testServer.Serve(listener)
			}()
			defer testServer.Close()

			apiInstance := api.New(testToken)
			apiInstance.Addr = fmt.Sprintf("127.0.0.1:%d", port)
			apiInstance.RegisterFunc("/test-error", http.HandlerFunc(testHandler))

			errChan := make(chan error, 1)
			go func() {
				defer ginkgo.GinkgoRecover()
				errChan <- apiInstance.Start(context.Background(), true)
			}()

			select {
			case err := <-errChan:
				gomega.Expect(err).To(gomega.HaveOccurred(), "Server should fail to start on occupied port")
				// Check for both Windows and POSIX error messages
				gomega.Expect(err.Error()).To(gomega.SatisfyAny(
					gomega.ContainSubstring("address already in use"),
					gomega.ContainSubstring("Only one usage of each socket address"),
				))
			case <-time.After(1 * time.Second):
				ginkgo.Fail("Timeout waiting for server start error")
			}
		})
	})
})

// testHandler is a simple handler for testing HTTP responses.
func testHandler(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, "Hello!")
}

// testLogWriter is a custom io.Writer for capturing log output in tests.
type testLogWriter struct {
	buffer    []byte
	writeFunc func([]byte) (int, error)
}

func (w *testLogWriter) Write(data []byte) (int, error) {
	if w.writeFunc != nil {
		return w.writeFunc(data)
	}

	w.buffer = append(w.buffer, data...)

	return len(data), nil
}

func (w *testLogWriter) String() string {
	return string(w.buffer)
}

// threadSafeBuffer is a thread-safe wrapper around bytes.Buffer for capturing logs.
type threadSafeBuffer struct {
	buf *bytes.Buffer
	mu  sync.Mutex
}

func (b *threadSafeBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	n, err := b.buf.Write(data)
	if err != nil {
		return n, fmt.Errorf("buffer write failed: %w", err)
	}

	return n, nil
}

func (b *threadSafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

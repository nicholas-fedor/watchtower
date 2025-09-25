// Package api_test provides external tests for the Watchtower HTTP API server.
package api_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/api"
)

// testToken is a constant token used for testing authentication.
const testToken = "123123123"

// errMockShutdownFailure is a mock error for shutdown failure.
var errMockShutdownFailure = errors.New("mock shutdown failure")

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
			apiInstance = api.New(testToken, ":8080")
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
			logBuffer = &threadSafeBuffer{buf: &bytes.Buffer{}, mu: sync.Mutex{}}
			logrus.SetOutput(logBuffer)
			logrus.SetLevel(logrus.DebugLevel)
		})

		ginkgo.AfterEach(func() {
			logrus.SetOutput(os.Stderr)
			logrus.SetLevel(logrus.InfoLevel)
		})

		ginkgo.It("should skip starting the server when no handlers are registered", func() {
			apiInstance := api.New(testToken, ":8080")
			err := apiInstance.Start(context.Background(), true)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Eventually(logBuffer.String, 100*time.Millisecond).
				Should(gomega.ContainSubstring("No handlers registered, skipping API start"))
		})

		ginkgo.It("should fail with a fatal log when token is empty", func() {
			emptyTokenAPI := api.New("", ":8080")
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
			gomega.Expect(func() { _ = emptyTokenAPI.Start(context.Background(), true) }).
				To(gomega.Panic())
			gomega.Expect(logOutput).
				To(gomega.ContainSubstring("API token is empty or unset"))
		})

		ginkgo.It("should start server synchronously and serve requests", func() {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer listener.Close()
			tcpAddr, ok := listener.Addr().(*net.TCPAddr)
			gomega.Expect(ok).To(gomega.BeTrue())
			port := tcpAddr.Port
			listener.Close()

			apiInstance := api.New(testToken, ":8080")
			apiInstance.Addr = fmt.Sprintf("127.0.0.1:%d", port)
			apiInstance.RegisterFunc("/test-sync", http.HandlerFunc(testHandler))

			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			errChan := make(chan error, 1)
			go func() {
				defer ginkgo.GinkgoRecover()
				errChan <- apiInstance.Start(ctx, true)
			}()

			gomega.Eventually(func() error {
				req, _ := http.NewRequest(
					http.MethodGet,
					fmt.Sprintf("http://127.0.0.1:%d/test-sync", port),
					nil,
				)
				req.Header.Set("Authorization", "Bearer "+testToken)
				resp, reqErr := http.DefaultClient.Do(req)
				if reqErr != nil {
					return reqErr
				}
				defer resp.Body.Close()

				return nil
			}, 400*time.Millisecond, 5*time.Millisecond).Should(gomega.Succeed())

			req, _ := http.NewRequest(
				http.MethodGet,
				fmt.Sprintf("http://127.0.0.1:%d/test-sync", port),
				nil,
			)
			req.Header.Set("Authorization", "Bearer "+testToken)
			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
			gomega.Expect(string(body)).To(gomega.Equal("Hello!"))

			cancel()
			select {
			case err := <-errChan:
				// Accept nil or context.Canceled as valid outcomes
				if err != nil && !errors.Is(err, context.Canceled) {
					gomega.Expect(err).
						ToNot(gomega.HaveOccurred(), "Expected no error or context.Canceled, got unexpected error")
				}
			case <-time.After(500 * time.Millisecond):
				ginkgo.Fail("Timeout waiting for server to stop")
			}
		})

		ginkgo.It("should start server asynchronously and serve requests", func() {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer listener.Close()
			tcpAddr, ok := listener.Addr().(*net.TCPAddr)
			gomega.Expect(ok).To(gomega.BeTrue())
			port := tcpAddr.Port
			listener.Close()

			apiInstance := api.New(testToken, ":8080")
			apiInstance.Addr = fmt.Sprintf("127.0.0.1:%d", port)
			apiInstance.RegisterFunc("/test-async", http.HandlerFunc(testHandler))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err = apiInstance.Start(ctx, false)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				req, _ := http.NewRequest(
					http.MethodGet,
					fmt.Sprintf("http://127.0.0.1:%d/test-async", port),
					nil,
				)
				req.Header.Set("Authorization", "Bearer "+testToken)
				resp, reqErr := http.DefaultClient.Do(req)
				if reqErr != nil {
					return reqErr
				}
				defer resp.Body.Close()

				return nil
			}, 500*time.Millisecond, 10*time.Millisecond).Should(gomega.Succeed())

			req, _ := http.NewRequest(
				http.MethodGet,
				fmt.Sprintf("http://127.0.0.1:%d/test-async", port),
				nil,
			)
			req.Header.Set("Authorization", "Bearer "+testToken)
			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
			gomega.Expect(string(body)).To(gomega.Equal("Hello!"))

			// Wait for server to stop after cancellation
			done := make(chan struct{})
			go func() {
				defer close(done)
				<-ctx.Done()                       // Wait for context cancellation
				time.Sleep(100 * time.Millisecond) // Give server time to shut down
			}()

			cancel()
			gomega.Eventually(func() bool {
				select {
				case <-done:
					return true
				default:
					return false
				}
			}, 500*time.Millisecond, 10*time.Millisecond).Should(gomega.BeTrue())

			gomega.Expect(logBuffer.String()).ToNot(gomega.ContainSubstring("HTTP server failed"))
		})

		ginkgo.It("should start server asynchronously and log error on failure", func() {
			apiInstance := api.New(testToken, ":8080")
			apiInstance.Addr = "127.0.0.1:invalid" // Invalid address to force failure
			apiInstance.RegisterFunc("/test-fail", http.HandlerFunc(testHandler))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := apiInstance.Start(ctx, false)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(logBuffer.String, 1*time.Second, 50*time.Millisecond).Should(
				gomega.ContainSubstring("HTTP server failed"),
				"Expected error log for invalid address",
			)
			gomega.Expect(logBuffer.String()).To(gomega.ContainSubstring("invalid"))
		})

		ginkgo.It("should fail to start server on occupied port", func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintln(w, "Occupied")
			})
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer listener.Close()
			tcpAddr, ok := listener.Addr().(*net.TCPAddr)
			gomega.Expect(ok).To(gomega.BeTrue())
			port := tcpAddr.Port

			testServer := &http.Server{
				Addr:              fmt.Sprintf("127.0.0.1:%d", port),
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
			}
			go func() { _ = testServer.Serve(listener) }()
			defer testServer.Close()

			apiInstance := api.New(testToken, ":8080")
			apiInstance.Addr = fmt.Sprintf("127.0.0.1:%d", port)
			apiInstance.RegisterFunc("/test-error", http.HandlerFunc(testHandler))

			errChan := make(chan error, 1)
			go func() {
				defer ginkgo.GinkgoRecover()
				errChan <- apiInstance.Start(context.Background(), true)
			}()

			select {
			case err := <-errChan:
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err.Error()).To(gomega.SatisfyAny(
					gomega.ContainSubstring("address already in use"),
					gomega.ContainSubstring("Only one usage of each socket address"),
				))
			case <-time.After(1 * time.Second):
				ginkgo.Fail("Timeout waiting for server start error")
			}
		})

		ginkgo.It("should handle server shutdown via context cancellation", func() {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer listener.Close()
			tcpAddr, ok := listener.Addr().(*net.TCPAddr)
			gomega.Expect(ok).To(gomega.BeTrue())
			port := tcpAddr.Port
			listener.Close()

			apiInstance := api.New(testToken, ":8080")
			apiInstance.Addr = fmt.Sprintf("127.0.0.1:%d", port)
			apiInstance.RegisterFunc("/test-shutdown", http.HandlerFunc(testHandler))

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			errChan := make(chan error, 1)
			go func() {
				defer ginkgo.GinkgoRecover()
				errChan <- apiInstance.Start(ctx, true)
			}()

			select {
			case err := <-errChan:
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			case <-time.After(500 * time.Millisecond):
				ginkgo.Fail("Timeout waiting for server to stop")
			}

			gomega.Eventually(func() error {
				req, _ := http.NewRequest(
					http.MethodGet,
					fmt.Sprintf("http://127.0.0.1:%d/test-shutdown", port),
					nil,
				)
				req.Header.Set("Authorization", "Bearer "+testToken)
				_, err := http.DefaultClient.Do(req)

				return err
			}, 200*time.Millisecond, 10*time.Millisecond).Should(gomega.HaveOccurred())
		})

		ginkgo.It("should return error on shutdown failure", func() {
			apiInstance := api.New(testToken, ":8080")
			apiInstance.Addr = "127.0.0.1:0"
			apiInstance.RegisterFunc("/test-shutdown-fail", http.HandlerFunc(testHandler))

			mockServer := &mockHTTPServer{
				listenErr:   nil,
				shutdownErr: errMockShutdownFailure,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			// Run server in a goroutine and capture error
			errChan := make(chan error, 1)
			go func() {
				defer ginkgo.GinkgoRecover()
				errChan <- api.RunHTTPServer(ctx, mockServer)
			}()

			// Wait for server to start, then cancel to trigger shutdown
			gomega.Eventually(func() bool {
				return mockServer.ListenAndServe() == nil // Mock doesn’t block, but simulate start
			}, 100*time.Millisecond, 10*time.Millisecond).Should(gomega.BeTrue())

			cancel() // Trigger shutdown

			select {
			case err := <-errChan:
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err.Error()).To(gomega.ContainSubstring("server shutdown failed"))
			case <-time.After(600 * time.Millisecond):
				ginkgo.Fail("Timeout waiting for shutdown error")
			}
		})

		ginkgo.It("should register and serve requests via RegisterHandler", func() {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer listener.Close()
			tcpAddr, ok := listener.Addr().(*net.TCPAddr)
			gomega.Expect(ok).To(gomega.BeTrue())
			port := tcpAddr.Port
			listener.Close()

			apiInstance := api.New(testToken, ":8080")
			apiInstance.Addr = fmt.Sprintf("127.0.0.1:%d", port)
			// Use RegisterHandler with a custom handler
			apiInstance.RegisterHandler("/test-handler", http.HandlerFunc(testHandler))

			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			errChan := make(chan error, 1)
			go func() {
				defer ginkgo.GinkgoRecover()
				errChan <- apiInstance.Start(ctx, true)
			}()

			gomega.Eventually(func() error {
				req, _ := http.NewRequest(
					http.MethodGet,
					fmt.Sprintf("http://127.0.0.1:%d/test-handler", port),
					nil,
				)
				req.Header.Set("Authorization", "Bearer "+testToken)
				resp, reqErr := http.DefaultClient.Do(req)
				if reqErr != nil {
					return reqErr
				}
				defer resp.Body.Close()

				return nil
			}, 400*time.Millisecond, 5*time.Millisecond).Should(gomega.Succeed())

			// Valid token request
			req, _ := http.NewRequest(
				http.MethodGet,
				fmt.Sprintf("http://127.0.0.1:%d/test-handler", port),
				nil,
			)
			req.Header.Set("Authorization", "Bearer "+testToken)
			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
			gomega.Expect(string(body)).To(gomega.Equal("Hello!"))

			// Invalid token request
			req, _ = http.NewRequest(
				http.MethodGet,
				fmt.Sprintf("http://127.0.0.1:%d/test-handler", port),
				nil,
			)
			req.Header.Set("Authorization", "Bearer wrongtoken")
			resp, err = http.DefaultClient.Do(req)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer resp.Body.Close()
			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusUnauthorized))

			cancel()
			select {
			case err := <-errChan:
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			case <-time.After(500 * time.Millisecond):
				ginkgo.Fail("Timeout waiting for server to stop")
			}
		})
	})
})

// testHandler is a simple handler for testing HTTP responses.
func testHandler(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, "Hello!")
}

// mockHTTPServer is a mock implementation of HTTPServer for testing.
type mockHTTPServer struct {
	listenErr   error
	shutdownErr error
}

func (m *mockHTTPServer) ListenAndServe() error {
	return m.listenErr
}

func (m *mockHTTPServer) Shutdown(_ context.Context) error {
	return m.shutdownErr
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

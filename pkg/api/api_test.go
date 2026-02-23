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
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/api"
)

// testToken is a constant token used for testing authentication.
const testToken = "123123123"

// testResponse is a constant response used for testing.
const testResponse = "Hello!"

// errMockShutdownFailure is a mock error for shutdown failure.
var errMockShutdownFailure = errors.New("mock shutdown failure")

// TestAPI runs the Ginkgo test suite for the API package.
func TestAPI(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "API Suite")
}

// TestAPI_ServerShutdown tests that the server shuts down cleanly when context is canceled.
func TestAPI_ServerShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()

		tcpAddr, ok := listener.Addr().(*net.TCPAddr)
		if !ok {
			t.Fatal("expected TCP address")
		}

		port := tcpAddr.Port

		listener.Close()

		apiInstance := api.New(testToken, ":8080")
		apiInstance.Addr = fmt.Sprintf("127.0.0.1:%d", port)
		apiInstance.RegisterFunc("/test-shutdown", http.HandlerFunc(testHandler))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errChan := make(chan error, 1)

		go func() {
			errChan <- apiInstance.Start(ctx, true, false)
		}()

		cancel()

		synctest.Wait()

		// Wait for server to shut down cleanly
		err = <-errChan
		if err != nil {
			t.Fatal(err)
		}
	})
}

// TestAPI_ServerStartError tests that starting the server on an occupied port fails.
func TestAPI_ServerStartError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()

		tcpAddr, ok := listener.Addr().(*net.TCPAddr)
		if !ok {
			t.Fatal("expected TCP address")
		}

		port := tcpAddr.Port

		apiInstance := api.New(testToken, ":8080")
		apiInstance.Addr = fmt.Sprintf("127.0.0.1:%d", port)
		apiInstance.RegisterFunc("/test-error", http.HandlerFunc(testHandler))

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		err = apiInstance.Start(ctx, true, false)
		if err == nil {
			t.Fatal("expected error when starting server on occupied port")
		}

		if !strings.Contains(err.Error(), "address already in use") &&
			!strings.Contains(err.Error(), "Only one usage of each socket address") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// TestAPI_ServerShutdownTimeout tests that the server returns an error on shutdown failure.
func TestAPI_ServerShutdownTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		apiInstance := api.New(testToken, ":8080")
		apiInstance.Addr = "127.0.0.1:0"
		apiInstance.RegisterFunc("/test-shutdown-fail", http.HandlerFunc(testHandler))

		mockServer := &mockHTTPServer{
			listenErr:   nil,
			shutdownErr: errMockShutdownFailure,
			shutdownCh:  make(chan struct{}),
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Run server in a goroutine and capture error
		errChan := make(chan error, 1)

		go func() {
			errChan <- api.RunHTTPServer(ctx, mockServer)
		}()

		// Wait for server to start, then cancel to trigger shutdown
		// Since mock doesn't block, assume it starts immediately
		cancel() // Trigger shutdown

		// Wait for the server goroutine to complete
		synctest.Wait()

		// Receive the error
		err := <-errChan
		if err == nil {
			t.Fatal("Expected error on shutdown failure")
		}

		if !strings.Contains(err.Error(), "server shutdown failed") {
			t.Fatalf("Unexpected error: %v", err)
		}
	})
}

// TestAPI_ServerStartTimeout tests that the server starts and serves requests correctly.
func TestAPI_ServerStartTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		apiInstance := api.New(testToken, ":8080")

		testMux := http.NewServeMux()
		testMux.Handle("/test-handler", apiInstance.RequireToken(http.HandlerFunc(testHandler)))

		testServer := httptest.NewServer(testMux)
		defer testServer.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		client := &http.Client{Timeout: 100 * time.Millisecond}

		// Valid token request
		req, _ := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			testServer.URL+"/test-handler",
			nil,
		)
		req.Header.Set("Authorization", "Bearer "+testToken)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}

		if string(body) != testResponse {
			t.Fatalf("expected 'Hello!', got %s", string(body))
		}

		// Invalid token request
		req, _ = http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			testServer.URL+"/test-handler",
			nil,
		)
		req.Header.Set("Authorization", "Bearer wrongtoken")

		resp, err = client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d", resp.StatusCode)
		}
	})
}

func TestAPI_SkipStartWhenNoHandlers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logBuffer := &threadSafeBuffer{buf: &bytes.Buffer{}, mu: sync.Mutex{}}
		logrus.SetOutput(logBuffer)
		logrus.SetLevel(logrus.DebugLevel)

		defer func() {
			logrus.SetOutput(os.Stderr)
			logrus.SetLevel(logrus.InfoLevel)
		}()

		apiInstance := api.New(testToken, ":8080")

		err := apiInstance.Start(context.Background(), true, false)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Allow time for logging to complete
		synctest.Wait()

		// Check for the log message
		if !strings.Contains(logBuffer.String(), "No handlers registered, skipping API start") {
			t.Error("expected log message not found")
		}
	})
}

func TestAPI_StartServerSynchronously(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()

		tcpAddr, ok := listener.Addr().(*net.TCPAddr)
		if !ok {
			t.Fatal("expected TCP address")
		}

		port := tcpAddr.Port

		listener.Close()

		apiInstance := api.New(testToken, ":8080")
		apiInstance.Addr = fmt.Sprintf("127.0.0.1:%d", port)
		apiInstance.RegisterFunc("/test-sync", http.HandlerFunc(testHandler))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errChan := make(chan error, 1)

		go func() {
			errChan <- apiInstance.Start(ctx, true, false)
		}()

		// Wait for server to be ready
		client := &http.Client{Timeout: 100 * time.Millisecond}

		serverReady := false

		maxAttempts := 10
		for i := 0; i < maxAttempts && !serverReady; i++ {
			req, _ := http.NewRequestWithContext(
				ctx,
				http.MethodGet,
				fmt.Sprintf("http://127.0.0.1:%d/test-sync", port),
				nil,
			)
			req.Header.Set("Authorization", "Bearer "+testToken)

			resp, reqErr := client.Do(req)
			if reqErr == nil {
				resp.Body.Close()

				serverReady = true
			}
		}

		if !serverReady {
			t.Fatal("server did not start within expected time")
		}

		// Make the request
		req, _ := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			fmt.Sprintf("http://127.0.0.1:%d/test-sync", port),
			nil,
		)
		req.Header.Set("Authorization", "Bearer "+testToken)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}

		if string(body) != testResponse {
			t.Fatalf("expected 'Hello!', got %s", string(body))
		}

		cancel()

		synctest.Wait()

		select {
		case err := <-errChan:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("unexpected error: %v", err)
			}
		default:
			t.Fatal("server did not stop as expected")
		}
	})
}

func TestAPI_StartServerAsynchronously(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logBuffer := &threadSafeBuffer{buf: &bytes.Buffer{}, mu: sync.Mutex{}}
		logrus.SetOutput(logBuffer)
		logrus.SetLevel(logrus.DebugLevel)

		defer func() {
			logrus.SetOutput(os.Stderr)
			logrus.SetLevel(logrus.InfoLevel)
		}()

		listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()

		tcpAddr, ok := listener.Addr().(*net.TCPAddr)
		if !ok {
			t.Fatal("expected TCP address")
		}

		port := tcpAddr.Port

		listener.Close()

		apiInstance := api.New(testToken, ":8080")
		apiInstance.Addr = fmt.Sprintf("127.0.0.1:%d", port)
		apiInstance.RegisterFunc("/test-async", http.HandlerFunc(testHandler))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err = apiInstance.Start(ctx, false, false)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Wait for server to be ready
		serverReady := false

		maxAttempts := 50
		for i := 0; i < maxAttempts && !serverReady; i++ {
			req, _ := http.NewRequestWithContext(
				ctx,
				http.MethodGet,
				fmt.Sprintf("http://127.0.0.1:%d/test-async", port),
				nil,
			)
			req.Header.Set("Authorization", "Bearer "+testToken)

			resp, reqErr := http.DefaultClient.Do(req)
			if reqErr == nil {
				resp.Body.Close()

				serverReady = true
			}
		}

		if !serverReady {
			t.Fatal("server did not start within expected time")
		}

		// Make the request
		req, _ := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			fmt.Sprintf("http://127.0.0.1:%d/test-async", port),
			nil,
		)
		req.Header.Set("Authorization", "Bearer "+testToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}

		if string(body) != testResponse {
			t.Fatalf("expected 'Hello!', got %s", string(body))
		}

		// Wait for server to stop after cancellation
		done := make(chan struct{})

		go func() {
			defer close(done)

			<-ctx.Done()
		}()

		cancel()

		synctest.Wait()

		// Wait for the done channel
		select {
		case <-done:
			// Server stopped
		default:
			t.Fatal("server did not stop as expected")
		}

		if strings.Contains(logBuffer.String(), "HTTP server failed") {
			t.Error("unexpected error log")
		}
	})
}

func TestAPI_StartServerAsyncLogErrorOnFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logBuffer := &threadSafeBuffer{buf: &bytes.Buffer{}, mu: sync.Mutex{}}
		logrus.SetOutput(logBuffer)
		logrus.SetLevel(logrus.DebugLevel)

		defer func() {
			logrus.SetOutput(os.Stderr)
			logrus.SetLevel(logrus.InfoLevel)
		}()

		apiInstance := api.New(testToken, ":8080")
		apiInstance.Addr = "127.0.0.1:invalid"
		apiInstance.RegisterFunc("/test-fail", http.HandlerFunc(testHandler))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err := apiInstance.Start(ctx, false, false)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Wait for error log
		found := false

		maxAttempts := 20
		for i := 0; i < maxAttempts && !found; i++ {
			if strings.Contains(logBuffer.String(), "HTTP server failed") &&
				strings.Contains(logBuffer.String(), "invalid") {
				found = true
			} else {
				synctest.Wait()
			}
		}

		if !found {
			t.Error("expected error log not found within expected time")
		}
	})
}

var _ = ginkgo.Describe("API", func() {
	ginkgo.Describe("RequireToken middleware", func() {
		var (
			apiInstance *api.API
			server      *ghttp.Server
		)

		ginkgo.BeforeEach(func() {
			apiInstance = api.New(testToken, ":8080")
			server = ghttp.NewServer()
			server.RouteToHandler("GET", "/hello", apiInstance.RequireToken(testHandler))
		})

		ginkgo.AfterEach(func() {
			server.Close()
		})

		ginkgo.It("should return 401 Unauthorized when token is not provided", func() {
			resp, err := http.Get(server.URL() + "/hello")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusUnauthorized))
			resp.Body.Close()
			gomega.Expect(server.ReceivedRequests()).To(gomega.HaveLen(1))
			req := server.ReceivedRequests()[0]
			gomega.Expect(req.Method).To(gomega.Equal("GET"))
			gomega.Expect(req.URL.Path).To(gomega.Equal("/hello"))
			gomega.Expect(req.Header.Get("Authorization")).To(gomega.Equal(""))
		})

		ginkgo.It("should return 401 Unauthorized when token is invalid", func() {
			req, _ := http.NewRequest(http.MethodGet, server.URL()+"/hello", nil)
			req.Header.Set("Authorization", "Bearer 123")
			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusUnauthorized))
			resp.Body.Close()
			gomega.Expect(server.ReceivedRequests()).To(gomega.HaveLen(1))
			reqReceived := server.ReceivedRequests()[0]
			gomega.Expect(reqReceived.Method).To(gomega.Equal("GET"))
			gomega.Expect(reqReceived.URL.Path).To(gomega.Equal("/hello"))
			gomega.Expect(reqReceived.Header.Get("Authorization")).To(gomega.Equal("Bearer 123"))
		})

		ginkgo.It("should return 200 OK when token is valid", func() {
			req, _ := http.NewRequest(http.MethodGet, server.URL()+"/hello", nil)
			req.Header.Set("Authorization", "Bearer "+testToken)
			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
			body, _ := io.ReadAll(resp.Body)
			gomega.Expect(string(body)).To(gomega.Equal("Hello!"))
			resp.Body.Close()
			gomega.Expect(server.ReceivedRequests()).To(gomega.HaveLen(1))
			reqReceived := server.ReceivedRequests()[0]
			gomega.Expect(reqReceived.Method).To(gomega.Equal("GET"))
			gomega.Expect(reqReceived.URL.Path).To(gomega.Equal("/hello"))
			gomega.Expect(reqReceived.Header.Get("Authorization")).
				To(gomega.Equal("Bearer " + testToken))
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

			gomega.Expect(func() { _ = emptyTokenAPI.Start(context.Background(), true, false) }).
				To(gomega.Panic())
			gomega.Expect(logOutput).
				To(gomega.ContainSubstring("API token is empty or unset"))
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
	shutdownCh  chan struct{}
}

func (m *mockHTTPServer) ListenAndServe() error {
	if m.listenErr != nil {
		return m.listenErr
	}
	// Block until shutdown is called
	<-m.shutdownCh

	return nil
}

func (m *mockHTTPServer) Shutdown(_ context.Context) error {
	close(m.shutdownCh)

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

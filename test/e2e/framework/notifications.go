// Package framework provides notification testing utilities for Watchtower e2e tests.
package framework

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

const (
	notificationDelay = 100 * time.Millisecond
)

var (
	errNotificationTimeout = errors.New("notification containing text not received within timeout")
	errUnsupportedService  = errors.New("unsupported notification service type")
)

// MockNotificationServer manages a mock notification service for testing.
type MockNotificationServer struct {
	server   *httptest.Server
	mu       sync.RWMutex
	requests []NotificationRequest
}

// NotificationRequest represents a captured notification request.
type NotificationRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string
	Time    time.Time
}

// NewMockNotificationServer creates a new mock notification server.
func NewMockNotificationServer() *MockNotificationServer {
	mock := &MockNotificationServer{
		requests: make([]NotificationRequest, 0),
	}

	mock.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.handleRequest(r)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))

	return mock
}

// URL returns the mock server URL.
func (m *MockNotificationServer) URL() string {
	return m.server.URL
}

// handleRequest captures notification requests.
func (m *MockNotificationServer) handleRequest(r *http.Request) {
	body := ""

	if r.Body != nil {
		buf := make([]byte, bufferSize)
		n, _ := r.Body.Read(buf)
		body = string(buf[:n])
	}

	headers := make(map[string]string)
	for key, values := range r.Header {
		headers[key] = strings.Join(values, ", ")
	}

	req := NotificationRequest{
		Method:  r.Method,
		URL:     r.URL.String(),
		Headers: headers,
		Body:    body,
		Time:    time.Now(),
	}

	m.mu.Lock()
	m.requests = append(m.requests, req)
	m.mu.Unlock()

	log.Printf("Mock notification server received: %s %s", r.Method, r.URL.Path)
}

// GetRequests returns all captured notification requests.
func (m *MockNotificationServer) GetRequests() []NotificationRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]NotificationRequest, len(m.requests))
	copy(result, m.requests)

	return result
}

// ClearRequests clears all captured requests.
func (m *MockNotificationServer) ClearRequests() {
	m.mu.Lock()
	m.requests = nil
	m.mu.Unlock()
}

// WaitForNotification waits for a notification containing the specified text.
func (m *MockNotificationServer) WaitForNotification(text string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		requests := m.GetRequests()
		for _, req := range requests {
			if strings.Contains(req.Body, text) {
				return nil
			}
		}

		time.Sleep(notificationDelay)
	}

	return fmt.Errorf("%w: '%s' within %v", errNotificationTimeout, text, timeout)
}

// Close shuts down the mock server.
func (m *MockNotificationServer) Close() {
	m.server.Close()
}

// SlackMockServer provides a mock Slack webhook server.
type SlackMockServer struct {
	*MockNotificationServer
}

// NewSlackMockServer creates a mock Slack webhook server.
func NewSlackMockServer() *SlackMockServer {
	return &SlackMockServer{
		MockNotificationServer: NewMockNotificationServer(),
	}
}

// GetSlackMessages returns Slack message payloads.
func (s *SlackMockServer) GetSlackMessages() []string {
	requests := s.GetRequests()
	messages := make([]string, 0, len(requests))

	for _, req := range requests {
		if strings.Contains(req.URL, "/slack") {
			messages = append(messages, req.Body)
		}
	}

	return messages
}

// EmailMockServer provides a mock SMTP server for email notifications.
type EmailMockServer struct {
	*MockNotificationServer
	emails []EmailMessage
}

// EmailMessage represents a captured email.
type EmailMessage struct {
	From    string
	To      string
	Subject string
	Body    string
	Time    time.Time
}

// NewEmailMockServer creates a mock SMTP server.
func NewEmailMockServer() *EmailMockServer {
	mock := &EmailMockServer{
		MockNotificationServer: NewMockNotificationServer(),
		emails:                 make([]EmailMessage, 0),
	}

	// Override the handler to parse email data
	mock.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.handleEmailRequest(r)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "sent"}`))
	})

	return mock
}

// handleEmailRequest parses email notification requests.
func (e *EmailMockServer) handleEmailRequest(r *http.Request) {
	// Parse email from form data or JSON
	// This is a simplified implementation
	email := EmailMessage{
		Time: time.Now(),
	}

	// Extract email details from request
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		email.To = r.FormValue("to")
		email.Subject = r.FormValue("subject")
		email.Body = r.FormValue("body")
		email.From = r.FormValue("from")
	}

	e.mu.Lock()
	e.emails = append(e.emails, email)
	e.mu.Unlock()

	log.Printf("Mock email server received email to: %s", email.To)
}

// GetEmails returns all captured emails.
func (e *EmailMockServer) GetEmails() []EmailMessage {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]EmailMessage, len(e.emails))
	copy(result, e.emails)

	return result
}

// GotifyMockServer provides a mock Gotify server.
type GotifyMockServer struct {
	*MockNotificationServer
	messages []GotifyMessage
}

// GotifyMessage represents a Gotify notification.
type GotifyMessage struct {
	Title    string
	Message  string
	Priority int
	Time     time.Time
}

// NewGotifyMockServer creates a mock Gotify server.
func NewGotifyMockServer() *GotifyMockServer {
	mock := &GotifyMockServer{
		MockNotificationServer: NewMockNotificationServer(),
		messages:               make([]GotifyMessage, 0),
	}

	// Override handler for Gotify-specific parsing
	mock.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.handleGotifyRequest(r)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 1}`))
	})

	return mock
}

// handleGotifyRequest parses Gotify notification requests.
func (g *GotifyMockServer) handleGotifyRequest(r *http.Request) {
	message := GotifyMessage{
		Time: time.Now(),
	}

	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		message.Title = r.FormValue("title")

		message.Message = r.FormValue("message")
		if priority := r.FormValue("priority"); priority != "" {
			_, _ = fmt.Sscanf(priority, "%d", &message.Priority)
		}
	}

	g.mu.Lock()
	g.messages = append(g.messages, message)
	g.mu.Unlock()

	log.Printf("Mock Gotify server received message: %s", message.Title)
}

// GetMessages returns all captured Gotify messages.
func (g *GotifyMockServer) GetMessages() []GotifyMessage {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make([]GotifyMessage, len(g.messages))
	copy(result, g.messages)

	return result
}

// Framework methods for notification testing

// StartMockNotificationService starts a mock notification service of the specified type.
func (f *E2EFramework) StartMockNotificationService(serviceType string) (any, error) {
	switch strings.ToLower(serviceType) {
	case "slack":
		mock := NewSlackMockServer()

		f.addCleanupFunc(func() error {
			mock.Close()

			return nil
		})

		return mock, nil

	case "email", "smtp":
		mock := NewEmailMockServer()

		f.addCleanupFunc(func() error {
			mock.Close()

			return nil
		})

		return mock, nil

	case "gotify":
		mock := NewGotifyMockServer()

		f.addCleanupFunc(func() error {
			mock.Close()

			return nil
		})

		return mock, nil

	default:
		return nil, fmt.Errorf("%w: %s", errUnsupportedService, serviceType)
	}
}

// WaitForNotification waits for a notification on the specified mock service.
func (f *E2EFramework) WaitForNotification(
	mockService any,
	text string,
	timeout time.Duration,
) error {
	switch service := mockService.(type) {
	case *SlackMockServer:
		return service.WaitForNotification(text, timeout)
	case *EmailMockServer:
		return service.WaitForNotification(text, timeout)
	case *GotifyMockServer:
		return service.WaitForNotification(text, timeout)
	default:
		return errUnsupportedService
	}
}

// BuildNotificationArgs builds Watchtower command arguments for the specified notification service.
func (f *E2EFramework) BuildNotificationArgs(
	serviceType string,
	config map[string]string,
) []string {
	args := []string{"--run-once", "--no-self-update"}

	switch strings.ToLower(serviceType) {
	case "slack":
		if url, ok := config["SLACK_HOOK_URL"]; ok {
			args = append(args, "--notification-slack", "--notification-slack-hook-url", url)
		}

	case "email":
		args = append(args, "--notification-email")
		if from := config["EMAIL_FROM"]; from != "" {
			args = append(args, "--notification-email-from", from)
		}

		if to := config["EMAIL_TO"]; to != "" {
			args = append(args, "--notification-email-to", to)
		}

		if server := config["EMAIL_SERVER"]; server != "" {
			args = append(args, "--notification-email-server", server)
		}

	case "gotify":
		if url, ok := config["GOTIFY_URL"]; ok {
			args = append(args, "--notification-gotify", "--notification-gotify-url", url)
		}

	case "shoutrrr":
		if urls, ok := config["SHOUTRRR_URLS"]; ok {
			args = append(args, "--notification-shoutrrr", "--notification-shoutrrr-urls", urls)
		}
	}

	return args
}

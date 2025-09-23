// Package framework provides HTTP API testing utilities for Watchtower e2e tests.
package framework

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// APIClient provides utilities for testing Watchtower's HTTP API.
type APIClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewAPIClient creates a new API client for testing.
func NewAPIClient(baseURL, token string) *APIClient {
	return &APIClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// TriggerUpdate sends a POST request to trigger container updates.
func (c *APIClient) TriggerUpdate(images []string) (*http.Response, error) {
	url := c.baseURL + "/v1/update"

	body := map[string]any{
		"images": images,
	}

	return c.sendRequest("POST", url, body)
}

// GetMetrics retrieves update metrics from the API.
func (c *APIClient) GetMetrics() (*http.Response, error) {
	url := c.baseURL + "/v1/metrics"

	return c.sendRequest("GET", url, nil)
}

// GetHealth checks the health endpoint.
func (c *APIClient) GetHealth() (*http.Response, error) {
	url := c.baseURL + "/v1/health"

	return c.sendRequest("GET", url, nil)
}

// sendRequest sends an HTTP request with proper authentication.
func (c *APIClient) sendRequest(method, url string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication header
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	// Set content type for POST requests
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	return resp, nil
}

// APIResponse represents a generic API response.
type APIResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// MetricsResponse represents the metrics API response.
type MetricsResponse struct {
	Scanned int `json:"scanned"`
	Updated int `json:"updated"`
	Failed  int `json:"failed"`
}

// ParseAPIResponse parses a generic API response.
func ParseAPIResponse(resp *http.Response) (*APIResponse, error) {
	defer resp.Body.Close()

	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	return &apiResp, nil
}

// ParseMetricsResponse parses a metrics API response.
func ParseMetricsResponse(resp *http.Response) (*MetricsResponse, error) {
	defer resp.Body.Close()

	var metrics MetricsResponse
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return nil, fmt.Errorf("failed to decode metrics response: %w", err)
	}

	return &metrics, nil
}

// Framework methods for API testing

// CreateAPIClient creates an API client for testing Watchtower's HTTP API.
func (f *E2EFramework) CreateAPIClient(token string) (*APIClient, error) {
	// Find the Watchtower container to get its API port
	// This is a simplified implementation - in practice you'd need to
	// inspect running containers or pass the port explicitly
	return NewAPIClient("http://localhost:8080", token), nil
}

// TriggerAPIUpdate triggers an update via the HTTP API.
func (f *E2EFramework) TriggerAPIUpdate(token string, images []string) error {
	client := NewAPIClient("http://localhost:8080", token)

	resp, err := client.TriggerUpdate(images)
	if err != nil {
		return fmt.Errorf("failed to trigger API update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("API update failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetAPIMetrics retrieves metrics from the HTTP API.
func (f *E2EFramework) GetAPIMetrics(token string) (map[string]any, error) {
	client := NewAPIClient("http://localhost:8080", token)

	resp, err := client.GetMetrics()
	if err != nil {
		return nil, fmt.Errorf("failed to get API metrics: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API metrics request failed with status %d", resp.StatusCode)
	}

	var metrics map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return nil, fmt.Errorf("failed to decode metrics response: %w", err)
	}

	return metrics, nil
}

// WaitForAPIReady waits for the HTTP API to be ready.
func (f *E2EFramework) WaitForAPIReady(url, token string, timeout time.Duration) error {
	client := NewAPIClient(url, token)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.GetHealth()
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("API not ready within %v", timeout)
}

// MockAPIServer provides a mock HTTP API server for testing API clients.
type MockAPIServer struct {
	server    *httptest.Server
	mu        sync.RWMutex
	requests  []APIRequest
	responses map[string]any
}

// APIRequest represents a captured API request.
type APIRequest struct {
	Method   string
	Path     string
	Headers  map[string]string
	Body     string
	Response any
	Time     time.Time
}

// NewMockAPIServer creates a new mock API server.
func NewMockAPIServer() *MockAPIServer {
	mock := &MockAPIServer{
		requests:  make([]APIRequest, 0),
		responses: make(map[string]any),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/update", mock.handleUpdate)
	mux.HandleFunc("/v1/metrics", mock.handleMetrics)
	mux.HandleFunc("/v1/health", mock.handleHealth)

	// Use httptest.Server which automatically assigns a port and provides the URL
	mock.server = httptest.NewServer(mux)

	return mock
}

// URL returns the mock server URL.
func (m *MockAPIServer) URL() string {
	return m.server.URL
}

// SetResponse sets a mock response for a specific endpoint.
func (m *MockAPIServer) SetResponse(path string, response any) {
	m.mu.Lock()
	m.responses[path] = response
	m.mu.Unlock()
}

// GetRequests returns all captured API requests.
func (m *MockAPIServer) GetRequests() []APIRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]APIRequest, len(m.requests))
	copy(result, m.requests)

	return result
}

// handleUpdate handles mock update requests.
func (m *MockAPIServer) handleUpdate(w http.ResponseWriter, r *http.Request) {
	m.captureRequest(r, "/v1/update")

	response := map[string]any{
		"status":  "success",
		"message": "Update triggered",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleMetrics handles mock metrics requests.
func (m *MockAPIServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	m.captureRequest(r, "/v1/metrics")

	response := map[string]any{
		"scanned": 5,
		"updated": 3,
		"failed":  0,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleHealth handles mock health requests.
func (m *MockAPIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	m.captureRequest(r, "/v1/health")

	response := map[string]any{
		"status": "healthy",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// captureRequest captures an API request.
func (m *MockAPIServer) captureRequest(r *http.Request, path string) {
	body := ""
	if r.Body != nil {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		body = string(buf[:n])
	}

	headers := make(map[string]string)
	for key, values := range r.Header {
		headers[key] = strings.Join(values, ", ")
	}

	req := APIRequest{
		Method:  r.Method,
		Path:    path,
		Headers: headers,
		Body:    body,
		Time:    time.Now(),
	}

	m.mu.Lock()
	m.requests = append(m.requests, req)
	m.mu.Unlock()
}

// Close shuts down the mock server.
func (m *MockAPIServer) Close(ctx context.Context) error {
	m.server.Close()

	return nil
}

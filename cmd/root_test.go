package cmd

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	containerMock "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	typeMock "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

func TestDeriveScopeFromContainer(t *testing.T) {
	// Save original scope value to restore later
	originalScope := scope

	defer func() { scope = originalScope }()

	tests := []struct {
		name          string
		initialScope  string
		hostname      string
		mockSetup     func(*containerMock.MockClient, *typeMock.MockContainer)
		expectedScope string
		expectedError bool
	}{
		{
			name:          "scope already set - should return nil without derivation",
			initialScope:  "preset",
			hostname:      "test-container",
			mockSetup:     func(*containerMock.MockClient, *typeMock.MockContainer) {},
			expectedScope: "preset",
			expectedError: false,
		},
		{
			name:          "no hostname - should return nil",
			initialScope:  "",
			hostname:      "",
			mockSetup:     func(*containerMock.MockClient, *typeMock.MockContainer) {},
			expectedScope: "",
			expectedError: false,
		},
		{
			name:         "container lookup fails - should return error",
			initialScope: "",
			hostname:     "test-container",
			mockSetup: func(client *containerMock.MockClient, _ *typeMock.MockContainer) {
				client.EXPECT().GetContainer(types.ContainerID("test-container")).
					Return(nil, errors.New("container not found"))
			},
			expectedScope: "",
			expectedError: true,
		},
		{
			name:         "container has no scope label - should return nil",
			initialScope: "",
			hostname:     "test-container",
			mockSetup: func(client *containerMock.MockClient, container *typeMock.MockContainer) {
				client.EXPECT().GetContainer(types.ContainerID("test-container")).
					Return(container, nil)
				container.EXPECT().Scope().Return("", false)
			},
			expectedScope: "",
			expectedError: false,
		},
		{
			name:         "container has empty scope label - should return nil",
			initialScope: "",
			hostname:     "test-container",
			mockSetup: func(client *containerMock.MockClient, container *typeMock.MockContainer) {
				client.EXPECT().GetContainer(types.ContainerID("test-container")).
					Return(container, nil)
				container.EXPECT().Scope().Return("", true)
			},
			expectedScope: "",
			expectedError: false,
		},
		{
			name:         "container has valid scope label - should set scope and return nil",
			initialScope: "",
			hostname:     "test-container",
			mockSetup: func(client *containerMock.MockClient, container *typeMock.MockContainer) {
				client.EXPECT().GetContainer(types.ContainerID("test-container")).
					Return(container, nil)
				container.EXPECT().Scope().Return("production", true)
			},
			expectedScope: "production",
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset scope to initial value
			scope = tt.initialScope

			// Set up environment
			t.Setenv("HOSTNAME", tt.hostname)

			// Create mocks
			mockClient := containerMock.NewMockClient(t)
			mockContainer := typeMock.NewMockContainer(t)

			// Set up mock expectations
			tt.mockSetup(mockClient, mockContainer)

			// Execute function under test
			err := deriveScopeFromContainer(mockClient)

			// Assert results
			if tt.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expectedScope, scope)

			// Verify mock expectations
			mockClient.AssertExpectations(t)
			mockContainer.AssertExpectations(t)
		})
	}
}

func TestDeriveScopeFromContainer_Logging(t *testing.T) {
	// Save original scope value to restore later
	originalScope := scope

	defer func() { scope = originalScope }()

	// Save original log level
	originalLevel := logrus.GetLevel()
	defer logrus.SetLevel(originalLevel)

	// Set log level to debug to capture debug logs
	logrus.SetLevel(logrus.DebugLevel)

	// Set up environment
	t.Setenv("HOSTNAME", "test-container")

	// Reset scope
	scope = ""

	// Create mocks
	mockClient := containerMock.NewMockClient(t)
	mockContainer := typeMock.NewMockContainer(t)

	// Set up successful derivation
	mockClient.On("GetContainer", types.ContainerID("test-container")).Return(mockContainer, nil)
	mockContainer.On("Scope").Return("derived-scope", true)

	// Capture log output
	var logOutput []byte

	hook := &testLogHook{logs: &logOutput}

	logrus.AddHook(hook)
	defer logrus.StandardLogger().ReplaceHooks(make(map[logrus.Level][]logrus.Hook))

	// Execute function
	err := deriveScopeFromContainer(mockClient)

	// Assert no error and scope was set
	require.NoError(t, err)
	assert.Equal(t, "derived-scope", scope)

	// Verify log contains expected message
	logStr := string(logOutput)
	assert.Contains(t, logStr, "Derived operational scope from current container's scope label")
	assert.Contains(t, logStr, "container_id=test-container")
	assert.Contains(t, logStr, "derived_scope=derived-scope")

	// Verify mock expectations
	mockClient.AssertExpectations(t)
	mockContainer.AssertExpectations(t)
}

// testLogHook captures log output for testing.
type testLogHook struct {
	logs *[]byte
}

func (h *testLogHook) Fire(entry *logrus.Entry) error {
	// Format the log entry similar to how logrus does it
	formatted := fmt.Sprintf("time=\"%s\" level=%s msg=\"%s\"",
		entry.Time.Format("2006-01-02T15:04:05-07:00"),
		entry.Level.String(),
		entry.Message)

	// Add fields
	for k, v := range entry.Data {
		formatted += fmt.Sprintf(" %s=%v", k, v)
	}

	formatted += "\n"

	*h.logs = append(*h.logs, []byte(formatted)...)

	return nil
}

func (h *testLogHook) Levels() []logrus.Level {
	return []logrus.Level{logrus.DebugLevel}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "zero duration",
			duration: 0,
			expected: "0 seconds",
		},
		{
			name:     "only seconds",
			duration: 45 * time.Second,
			expected: "45 seconds",
		},
		{
			name:     "minutes and seconds",
			duration: 2*time.Minute + 30*time.Second,
			expected: "2 minutes, 30 seconds",
		},
		{
			name:     "hours, minutes, seconds",
			duration: 1*time.Hour + 30*time.Minute + 45*time.Second,
			expected: "1 hour, 30 minutes, 45 seconds",
		},
		{
			name:     "single hour",
			duration: 1 * time.Hour,
			expected: "1 hour",
		},
		{
			name:     "single minute",
			duration: 1 * time.Minute,
			expected: "1 minute",
		},
		{
			name:     "single second",
			duration: 1 * time.Second,
			expected: "1 second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatTimeUnit(t *testing.T) {
	tests := []struct {
		name         string
		value        int64
		singular     string
		plural       string
		forceInclude bool
		expected     string
	}{
		{
			name:         "zero value not forced",
			value:        0,
			singular:     "second",
			plural:       "seconds",
			forceInclude: false,
			expected:     "",
		},
		{
			name:         "zero value forced",
			value:        0,
			singular:     "second",
			plural:       "seconds",
			forceInclude: true,
			expected:     "0 seconds",
		},
		{
			name:         "singular value",
			value:        1,
			singular:     "hour",
			plural:       "hours",
			forceInclude: false,
			expected:     "1 hour",
		},
		{
			name:         "plural value",
			value:        5,
			singular:     "minute",
			plural:       "minutes",
			forceInclude: false,
			expected:     "5 minutes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTimeUnit(struct {
				value    int64
				singular string
				plural   string
			}{tt.value, tt.singular, tt.plural}, tt.forceInclude)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFilterEmpty(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "all empty",
			input:    []string{"", "", ""},
			expected: nil,
		},
		{
			name:     "all non-empty",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "mixed empty and non-empty",
			input:    []string{"", "a", "", "b", ""},
			expected: []string{"a", "b"},
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterEmpty(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAwaitDockerClient(t *testing.T) {
	// This function just sleeps for 1 second, so we test that it doesn't panic
	// and completes within a reasonable time
	start := time.Now()

	awaitDockerClient()

	elapsed := time.Since(start)

	// Should take at least 1 second but not more than 2 (to account for timing variations)
	assert.GreaterOrEqual(t, elapsed, time.Second, "Should sleep for at least 1 second")
	assert.Less(t, elapsed, 2*time.Second, "Should not sleep for more than 2 seconds")
}

// ./cmd/png-to-text-service/main_test.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nnikolov3/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nnikolov3/png-to-text-service/internal/config"
)

// Static errors for mocks, adhering to err113.
var (
	errNatsPublish         = errors.New("nats publish error")
	errPipelineProcessing  = errors.New("pipeline processing error")
	errPipelineCtxExceeded = errors.New("pipeline context deadline exceeded")
)

// --- Mocks and Test Helpers ---

// mockNatsConnection is a mock implementation of the NatsConnection interface for
// testing.
type mockNatsConnection struct {
	published map[string][]byte
	sync.Mutex
	shouldErr bool
}

func newMockNatsConnection() *mockNatsConnection {
	return &mockNatsConnection{
		Mutex:     sync.Mutex{},
		published: make(map[string][]byte),
		shouldErr: false,
	}
}

func (m *mockNatsConnection) Publish(subj string, data []byte) error {
	m.Lock()
	defer m.Unlock()

	if m.shouldErr {
		return errNatsPublish
	}

	m.published[subj] = data

	return nil
}

func (m *mockNatsConnection) getPublished(subj string) ([]byte, bool) {
	m.Lock()
	defer m.Unlock()

	data, ok := m.published[subj]

	return data, ok
}

// mockPipeline is a mock implementation of the PipelineProcessor interface.
type mockPipeline struct {
	shouldErr bool
}

func (m *mockPipeline) ProcessSingle(_ context.Context, _, outputPath string) error {
	if m.shouldErr {
		return errPipelineProcessing
	}
	// Simulate work by creating a dummy output file.
	writeErr := os.WriteFile(outputPath, []byte("processed text"), 0o600)
	if writeErr != nil {
		return fmt.Errorf("mock failed to write file: %w", writeErr)
	}

	return nil
}

// newTestLogger creates a logger instance that writes to a temporary directory.
func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()

	logDir := t.TempDir()
	testLogger, err := logger.New(logDir, "test.log")
	require.NoError(t, err)

	return testLogger
}

// newTestConfig creates a clean, fully-populated config object for tests.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()

	return &config.Config{
		Project: config.Project{
			Name:        "",
			Version:     "",
			Description: "",
		},
		Paths: config.Paths{
			InputDir:  "",
			OutputDir: t.TempDir(),
		},
		Prompts: config.Prompts{
			Augmentation: "",
		},
		Augmentation: config.Augmentation{
			Type:             "",
			CustomPrompt:     "",
			UsePromptBuilder: false,
		},
		Logging: config.Logging{
			Level:                "",
			Dir:                  "",
			EnableFileLogging:    false,
			EnableConsoleLogging: false,
		},
		Tesseract: config.Tesseract{
			Language:       "",
			OEM:            0,
			PSM:            0,
			DPI:            0,
			TimeoutSeconds: 0,
		},
		Gemini: config.Gemini{
			APIKeyVariable:    "",
			Models:            nil,
			MaxRetries:        0,
			RetryDelaySeconds: 0,
			TimeoutSeconds:    0,
			Temperature:       0,
			TopK:              0,
			TopP:              0,
			MaxTokens:         0,
		},
		Settings: config.Settings{
			Workers:            0,
			TimeoutSeconds:     1, // Default timeout for tests
			EnableAugmentation: false,
			SkipExisting:       false,
		},
	}
}

// --- Unit Tests ---

func TestParseJobMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expectedJob PngCreatedJob
		name        string
		inputJSON   string
		errContains string
		expectErr   bool
	}{
		{
			name:      "Valid Job",
			inputJSON: `{"pngPathInStorage": "/path/to/image.png", "jobId": "job-123"}`,
			expectErr: false,
			expectedJob: PngCreatedJob{
				PngPathInStorage: "/path/to/image.png",
				JobID:            "job-123",
			},
			errContains: "",
		},
		{
			name:        "Invalid JSON",
			inputJSON:   `{"invalid json`,
			expectErr:   true,
			expectedJob: PngCreatedJob{PngPathInStorage: "", JobID: ""},
			errContains: "json.Unmarshal",
		},
		{
			name:        "Missing PngPathInStorage",
			inputJSON:   `{"jobId": "job-123"}`,
			expectErr:   true,
			expectedJob: PngCreatedJob{PngPathInStorage: "", JobID: ""},
			errContains: "pngPathInStorage cannot be empty",
		},
		{
			name:        "Missing JobID",
			inputJSON:   `{"pngPathInStorage": "/path/to/image.png"}`,
			expectErr:   true,
			expectedJob: PngCreatedJob{PngPathInStorage: "", JobID: ""},
			errContains: "jobId cannot be empty",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			job, err := parseJobMessage([]byte(testCase.inputJSON))
			if testCase.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errContains)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.expectedJob, job)
			}
		})
	}
}

func TestGenerateOutputPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		outputDir    string
		pngPath      string
		expectedPath string
		errContains  string
		expectErr    bool
	}{
		{
			name:         "Valid Path",
			outputDir:    "/tmp/output",
			pngPath:      "/tmp/input/image.png",
			expectedPath: "/tmp/output/image.txt",
			expectErr:    false,
			errContains:  "",
		},
		{
			name:         "Path with spaces",
			outputDir:    "/tmp/output",
			pngPath:      "/tmp/input/my image.png",
			expectedPath: "/tmp/output/my image.txt",
			expectErr:    false,
			errContains:  "",
		},
		{
			name:         "Empty PNG Path",
			outputDir:    "/tmp/output",
			pngPath:      "",
			expectedPath: "",
			expectErr:    true,
			errContains:  "input png path cannot be empty",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path, err := generateOutputPath(
				testCase.outputDir,
				testCase.pngPath,
			)
			if testCase.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errContains)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.expectedPath, path)
			}
		})
	}
}

// --- JobHandler Tests ---

func TestJobHandler_Handle_Success(t *testing.T) {
	t.Parallel()
	// Setup
	mockConn := newMockNatsConnection()
	mockProc := &mockPipeline{shouldErr: false}
	testCfg := newTestConfig(t)
	testLog := newTestLogger(t)

	handler := NewJobHandler(mockConn, mockProc, testLog, testCfg)

	job := PngCreatedJob{PngPathInStorage: "/test/image.png", JobID: "success-job"}
	jobJSON, marshalErr := json.Marshal(job)
	require.NoError(t, marshalErr)

	natsMsg := &nats.Msg{Data: jobJSON, Subject: "", Reply: "", Header: nil, Sub: nil}

	// Execute
	handler.Handle(natsMsg)

	// Verify
	// 1. Check if the output file was created by the mock pipeline.
	expectedOutputPath := filepath.Join(testCfg.Paths.OutputDir, "image.txt")
	_, statErr := os.Stat(expectedOutputPath)
	require.NoError(t, statErr, "Expected output file to be created")

	// 2. Check if the completion event was published to NATS.
	publishedData, ok := mockConn.getPublished("ocr.completed")
	require.True(t, ok, "Expected a message to be published to 'ocr.completed'")

	var completionJob OcrCompletedJob

	unmarshalErr := json.Unmarshal(publishedData, &completionJob)
	require.NoError(t, unmarshalErr)

	assert.Equal(t, job.JobID, completionJob.JobID)
	assert.Equal(t, job.PngPathInStorage, completionJob.OriginalPngPath)
	assert.Equal(t, expectedOutputPath, completionJob.TextPathInStorage)
}

func TestJobHandler_Handle_UnmarshalFailure(t *testing.T) {
	t.Parallel()
	// Setup
	mockConn := newMockNatsConnection()
	mockProc := &mockPipeline{shouldErr: false}
	testCfg := newTestConfig(t)
	testLog := newTestLogger(t)
	handler := NewJobHandler(mockConn, mockProc, testLog, testCfg)
	natsMsg := &nats.Msg{
		Data:    []byte("invalid json"),
		Subject: "",
		Reply:   "",
		Header:  nil,
		Sub:     nil,
	}

	// Execute
	handler.Handle(natsMsg)

	// Verify - No message should be published
	_, ok := mockConn.getPublished("ocr.completed")
	assert.False(t, ok, "Should not publish on unmarshal failure")
}

func TestJobHandler_Handle_PipelineFailure(t *testing.T) {
	t.Parallel()
	// Setup
	mockConn := newMockNatsConnection()
	mockProc := &mockPipeline{shouldErr: true} // Force pipeline to fail
	testCfg := newTestConfig(t)
	testLog := newTestLogger(t)
	handler := NewJobHandler(mockConn, mockProc, testLog, testCfg)
	job := PngCreatedJob{
		PngPathInStorage: "/test/image.png",
		JobID:            "pipeline-fail-job",
	}
	jobJSON, marshalErr := json.Marshal(job)
	require.NoError(t, marshalErr)

	natsMsg := &nats.Msg{Data: jobJSON, Subject: "", Reply: "", Header: nil, Sub: nil}

	// Execute
	handler.Handle(natsMsg)

	// Verify
	_, ok := mockConn.getPublished("ocr.completed")
	assert.False(t, ok, "Should not publish on pipeline processing failure")
}

func TestJobHandler_Handle_PublishFailure(t *testing.T) {
	t.Parallel()
	// Setup
	mockConn := newMockNatsConnection()

	mockConn.shouldErr = true // Force NATS publish to fail

	mockProc := &mockPipeline{shouldErr: false}
	testCfg := newTestConfig(t)
	testLog := newTestLogger(t)
	handler := NewJobHandler(mockConn, mockProc, testLog, testCfg)
	job := PngCreatedJob{
		PngPathInStorage: "/test/image.png",
		JobID:            "publish-fail-job",
	}
	jobJSON, marshalErr := json.Marshal(job)
	require.NoError(t, marshalErr)

	natsMsg := &nats.Msg{Data: jobJSON, Subject: "", Reply: "", Header: nil, Sub: nil}

	// Execute - We don't check for panics, but we expect an error log.
	// In a real scenario, you'd check the logs or metrics.
	handler.Handle(natsMsg)

	// The meaningful test is that the service doesn't crash.
	// We confirmed this by running the handler. No further assertion is needed.
}

// stallingMockPipeline is a mock that blocks to test context cancellation.
type stallingMockPipeline struct {
	stallDuration time.Duration
}

// ProcessSingle waits for a stall duration or until the context is canceled.
// It uses time.NewTimer for a leak-proof implementation.
func (m *stallingMockPipeline) ProcessSingle(ctx context.Context, _, _ string) error {
	timer := time.NewTimer(m.stallDuration)
	defer timer.Stop() // Ensures the timer doesn't leak if context cancels first.

	select {
	case <-timer.C:
		return nil // Stall completed without context cancellation.
	case <-ctx.Done():
		return errPipelineCtxExceeded // Context was canceled, as expected.
	}
}

func TestJobHandler_RunPipelineProcessing_Timeout(t *testing.T) {
	t.Parallel()
	// Setup
	// The stall duration MUST be longer than the timeout to trigger the timeout.
	stallingPipeline := &stallingMockPipeline{stallDuration: 2 * time.Second}
	testCfg := newTestConfig(t)

	testCfg.Settings.TimeoutSeconds = 1 // 1 second timeout

	handler := &JobHandler{
		conn:      nil,
		pipeline:  stallingPipeline,
		logger:    newTestLogger(t),
		appConfig: testCfg,
	}

	// Execute: Run processing and expect a context deadline exceeded error.
	err := handler.runPipelineProcessing("input.png", "output.txt")

	// Verify
	require.Error(t, err, "An error is expected due to timeout")
	assert.ErrorIs(
		t,
		err,
		errPipelineCtxExceeded,
		"The error should be the specific context exceeded error",
	)
}

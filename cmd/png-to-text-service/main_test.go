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
	errJetStreamNoMessage  = errors.New("no messages available")
	errConsumerFetch       = errors.New("consumer fetch error")
	errStreamCreation      = errors.New("stream creation failed")
	errConsumerCreation    = errors.New("consumer creation failed")
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

func (m *mockNatsConnection) getOcrCompletedMessage() ([]byte, bool) {
	m.Lock()
	defer m.Unlock()

	data, ok := m.published["ocr.completed"]

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

// mockJetStreamMessage represents a JetStream message for testing.
type mockJetStreamMessage struct {
	ackFunc func() error
	nakFunc func() error
	data    []byte
}

func (m *mockJetStreamMessage) Data() []byte {
	return m.data
}

func (m *mockJetStreamMessage) Ack() error {
	if m.ackFunc != nil {
		return m.ackFunc()
	}

	return nil
}

func (m *mockJetStreamMessage) Nak() error {
	if m.nakFunc != nil {
		return m.nakFunc()
	}

	return nil
}

// mockJetStreamConsumer is a mock implementation of JetStream consumer operations.
type mockJetStreamConsumer struct {
	consumeErr error
	messages   []*mockJetStreamMessage
	messageIdx int
	mutex      sync.Mutex
	shouldErr  bool
}

func newMockJetStreamConsumer() *mockJetStreamConsumer {
	return &mockJetStreamConsumer{
		consumeErr: nil,
		messages:   make([]*mockJetStreamMessage, 0),
		messageIdx: 0,
		mutex:      sync.Mutex{},
		shouldErr:  false,
	}
}

func (m *mockJetStreamConsumer) FetchMessage() (*jetStreamMessageWrapper, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.shouldErr {
		return nil, m.consumeErr
	}

	if m.messageIdx >= len(m.messages) {
		return nil, errJetStreamNoMessage
	}

	msg := m.messages[m.messageIdx]
	m.messageIdx++

	// Wrap the mock message in the expected wrapper type
	return &jetStreamMessageWrapper{
		msg: &nats.Msg{
			Data:    msg.data,
			Subject: "",
			Reply:   "",
			Header:  nil,
			Sub:     nil,
		},
	}, nil
}

func (m *mockJetStreamConsumer) addMessage(data []byte) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	msg := &mockJetStreamMessage{
		data:    data,
		ackFunc: func() error { return nil },
		nakFunc: func() error { return nil },
	}

	m.messages = append(m.messages, msg)
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
	publishedData, ok := mockConn.getOcrCompletedMessage()
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
	_, ok := mockConn.getOcrCompletedMessage()
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
	_, ok := mockConn.getOcrCompletedMessage()
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

// TestJetStreamMessage_Success tests successful JetStream message operations.
func TestJetStreamMessage_Success(t *testing.T) {
	t.Parallel()

	testData := []byte(`{"jobId": "test", "pngPathInStorage": "/test/file.png"}`)
	msg := &mockJetStreamMessage{
		ackFunc: func() error { return nil },
		nakFunc: func() error { return nil },
		data:    testData,
	}

	// Test Data method
	assert.Equal(t, testData, msg.Data())

	// Test successful Ack
	ackErr := msg.Ack()
	require.NoError(t, ackErr)

	// Test successful Nak
	nakErr := msg.Nak()
	require.NoError(t, nakErr)
}

// TestJetStreamConsumer_Success tests successful JetStream consumer operations.
func TestJetStreamConsumer_Success(t *testing.T) {
	t.Parallel()

	consumer := newMockJetStreamConsumer()
	testData := []byte(`{"jobId": "test", "pngPathInStorage": "/test/file.png"}`)

	// Add a test message
	consumer.addMessage(testData)

	// Fetch the message
	msg, fetchErr := consumer.FetchMessage()
	require.NoError(t, fetchErr)
	require.NotNil(t, msg)
	assert.Equal(t, testData, msg.Data())

	// Verify no more messages available
	_, noMsgErr := consumer.FetchMessage()
	assert.ErrorIs(t, noMsgErr, errJetStreamNoMessage)
}

// TestJetStreamConsumer_Error tests JetStream consumer error scenarios.
func TestJetStreamConsumer_Error(t *testing.T) {
	t.Parallel()

	consumer := newMockJetStreamConsumer()

	consumer.shouldErr = true
	consumer.consumeErr = errConsumerFetch

	// Should return error when shouldErr is true
	_, fetchErr := consumer.FetchMessage()
	require.Error(t, fetchErr)
	assert.ErrorIs(t, fetchErr, errConsumerFetch)
}

// TestJetStreamClientWrapper tests the wrapper around NATS JetStream client.
func TestJetStreamClientWrapper_Success(t *testing.T) {
	t.Parallel()

	// This test will need mock NATS connection or embedded server
	// For now, we'll test that the wrapper can be created successfully
	t.Skip("Integration test - requires NATS server")
}

// TestJobHandler_ProcessJetStreamMessage tests processing a single JetStream message.
func TestJobHandler_ProcessJetStreamMessage_Success(t *testing.T) {
	t.Parallel()

	mockConn := newMockNatsConnection()
	mockProc := &mockPipeline{shouldErr: false}
	testCfg := newTestConfig(t)
	testLog := newTestLogger(t)
	handler := NewJobHandler(mockConn, mockProc, testLog, testCfg)

	// Create test job message
	job := PngCreatedJob{
		PngPathInStorage: "/test/image.png",
		JobID:            "jetstream-test-job",
	}
	jobJSON, marshalErr := json.Marshal(job)
	require.NoError(t, marshalErr)

	// Create mock JetStream message
	msg := &mockJetStreamMessage{
		data:    jobJSON,
		ackFunc: func() error { return nil },
		nakFunc: func() error { return nil },
	}

	// Execute
	processErr := handler.ProcessJetStreamMessage(msg)

	// Verify
	require.NoError(t, processErr)

	// Verify completion message was published
	publishedData, wasPublished := mockConn.getOcrCompletedMessage()
	require.True(t, wasPublished, "Should publish completion event")

	var completionEvent OcrCompletedJob

	unmarshalErr := json.Unmarshal(publishedData, &completionEvent)
	require.NoError(t, unmarshalErr)
	assert.Equal(t, "jetstream-test-job", completionEvent.JobID)
	assert.Equal(t, "/test/image.png", completionEvent.OriginalPngPath)
}

// TestJobHandler_ProcessJetStreamMessage_ProcessingError tests error handling.
func TestJobHandler_ProcessJetStreamMessage_ProcessingError(t *testing.T) {
	t.Parallel()

	mockConn := newMockNatsConnection()
	mockProc := &mockPipeline{shouldErr: true} // Force processing error
	testCfg := newTestConfig(t)
	testLog := newTestLogger(t)
	handler := NewJobHandler(mockConn, mockProc, testLog, testCfg)

	// Create test job message
	job := PngCreatedJob{
		PngPathInStorage: "/test/image.png",
		JobID:            "error-test-job",
	}
	jobJSON, marshalErr := json.Marshal(job)
	require.NoError(t, marshalErr)

	// Create mock JetStream message
	msg := &mockJetStreamMessage{
		data:    jobJSON,
		ackFunc: func() error { return nil },
		nakFunc: func() error { return nil },
	}

	// Execute - should return error but still acknowledge message
	processErr := handler.ProcessJetStreamMessage(msg)

	// Verify error is returned for processing failure
	require.Error(t, processErr)

	// Verify no completion message was published on error
	_, wasPublished := mockConn.getOcrCompletedMessage()
	assert.False(t, wasPublished, "Should not publish on processing error")
}

// mockJetStreamContext is a mock implementation for testing JetStream operations.
type mockJetStreamContext struct {
	streamErr       error
	consumerErr     error
	streamName      string
	consumerName    string
	streamCreated   bool
	consumerCreated bool
}

func newMockJetStreamContext() *mockJetStreamContext {
	return &mockJetStreamContext{
		streamCreated:   false,
		consumerCreated: false,
		streamName:      "",
		consumerName:    "",
		streamErr:       nil,
		consumerErr:     nil,
	}
}

func (m *mockJetStreamContext) CreateStream(name string, _ []string) error {
	if m.streamErr != nil {
		return m.streamErr
	}

	m.streamCreated = true
	m.streamName = name

	return nil
}

func (m *mockJetStreamContext) CreateConsumer(_, consumerName string) error {
	if m.consumerErr != nil {
		return m.consumerErr
	}

	m.consumerCreated = true
	m.consumerName = consumerName

	return nil
}

// TestConfigureJetStreamResources tests JetStream stream and consumer configuration.
func TestConfigureJetStreamResources_Success(t *testing.T) {
	t.Parallel()

	mockJS := newMockJetStreamContext()

	configErr := configureJetStreamResources(mockJS)

	require.NoError(t, configErr)
	assert.True(t, mockJS.streamCreated, "Stream should be created")
	assert.True(t, mockJS.consumerCreated, "Consumer should be created")
	assert.Equal(t, "PNG_PROCESSING", mockJS.streamName)
	assert.Equal(t, "png-text-workers", mockJS.consumerName)
}

// TestConfigureJetStreamResources_StreamError tests stream creation failure.
func TestConfigureJetStreamResources_StreamError(t *testing.T) {
	t.Parallel()

	mockJS := newMockJetStreamContext()

	mockJS.streamErr = errStreamCreation

	configErr := configureJetStreamResources(mockJS)

	require.Error(t, configErr)
	assert.Contains(t, configErr.Error(), "failed to create stream")
	assert.False(t, mockJS.streamCreated, "Stream creation should fail")
}

// TestConfigureJetStreamResources_ConsumerError tests consumer creation failure.
func TestConfigureJetStreamResources_ConsumerError(t *testing.T) {
	t.Parallel()

	mockJS := newMockJetStreamContext()

	mockJS.consumerErr = errConsumerCreation

	configErr := configureJetStreamResources(mockJS)

	require.Error(t, configErr)
	assert.Contains(t, configErr.Error(), "failed to create consumer")
	assert.True(t, mockJS.streamCreated, "Stream should be created successfully")
	assert.False(t, mockJS.consumerCreated, "Consumer creation should fail")
}

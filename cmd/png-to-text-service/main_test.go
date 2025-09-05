// ./cmd/png-to-text-service/main_test.go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/nnikolov3/png-to-text-service/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Constants for test data.
const (
	testWorkers           = 8
	testCustomPrompt      = "Summarize this document."
	testAugmentationType  = "summary"
	testInvalidAugmenType = "invalid-type"
	testDirPermissions    = 0o750
)

// mockLogger captures log output for verification in tests.
// It correctly implements the logProvider interface.
type mockLogger struct {
	outputs []string
	mu      sync.Mutex
}

func (m *mockLogger) Info(
	format string,
	args ...any,
) {
	m.log("INFO: " + fmt.Sprintf(format, args...))
}

func (m *mockLogger) Error(format string, args ...any) {
	m.log("ERROR: " + fmt.Sprintf(format, args...))
}

func (m *mockLogger) Success(format string, args ...any) {
	m.log("SUCCESS: " + fmt.Sprintf(format, args...))
}
func (m *mockLogger) Close() error { return nil }
func (m *mockLogger) log(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.outputs = append(m.outputs, message)
}

func (m *mockLogger) getLastOutput() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.outputs) == 0 {
		return ""
	}

	return m.outputs[len(m.outputs)-1]
}

// mockPipeline simulates the processing pipeline for testing main logic.
// It correctly implements the pipelineProcessor interface.
type mockPipeline struct {
	processSingleErr       error
	processDirectoryErr    error
	processSingleCalled    bool
	processDirectoryCalled bool
}

// ProcessSingle mocks the single file processing method.
// The unused parameters are named _ to satisfy the linter.
func (p *mockPipeline) ProcessSingle(_ context.Context, _, _ string) error {
	p.processSingleCalled = true

	return p.processSingleErr
}

// ProcessDirectory mocks the directory processing method.
// The unused parameters are named _ to satisfy the linter.
func (p *mockPipeline) ProcessDirectory(_ context.Context, _, _ string) error {
	p.processDirectoryCalled = true

	return p.processDirectoryErr
}

// newTestConfig creates a clean, fully-populated config object for each test,
// satisfying the exhaustruct linter.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()

	return &config.Config{
		Project: config.Project{
			Name:        "test-project",
			Version:     "1.0.0",
			Description: "Test Description",
		},
		Paths: config.Paths{
			InputDir:  t.TempDir(),
			OutputDir: t.TempDir(),
		},
		Prompts: config.Prompts{
			Augmentation: "Test Augmentation Prompt",
		},
		Augmentation: config.Augmentation{
			Type:             "commentary",
			CustomPrompt:     "Test Custom Prompt",
			UsePromptBuilder: true,
		},
		Logging: config.Logging{
			Level:                "info",
			Dir:                  "/test/logs",
			EnableFileLogging:    true,
			EnableConsoleLogging: true,
		},
		Tesseract: config.Tesseract{
			Language:       "eng",
			OEM:            3,
			PSM:            3,
			DPI:            300,
			TimeoutSeconds: 60,
		},
		Gemini: config.Gemini{
			APIKeyVariable:    "TEST_API_KEY",
			Models:            []string{"gemini-pro"},
			MaxRetries:        3,
			RetryDelaySeconds: 5,
			TimeoutSeconds:    60,
			Temperature:       0.7,
			TopK:              40,
			TopP:              0.9,
			MaxTokens:         2048,
		},
		Settings: config.Settings{
			Workers:            4,
			TimeoutSeconds:     120,
			EnableAugmentation: true,
			SkipExisting:       false,
		},
	}
}

// TestApplyPathOverrides checks if input/output directory flags override the config.
func TestApplyPathOverrides(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	newInputDir := "/new/input/from/flag"
	newOutputDir := "/new/output/from/flag"

	applyPathOverrides(cfg, newInputDir, newOutputDir)

	assert.Equal(t, newInputDir, cfg.Paths.InputDir)
	assert.Equal(t, newOutputDir, cfg.Paths.OutputDir)
}

// TestApplySettingsOverrides checks if worker and no-augment flags override the config.
func TestApplySettingsOverrides(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	newWorkers := testWorkers
	disableAugmentation := true

	applySettingsOverrides(cfg, newWorkers, disableAugmentation)

	assert.Equal(t, newWorkers, cfg.Settings.Workers)
	assert.False(t, cfg.Settings.EnableAugmentation)
}

// TestApplyAugmentationOverrides checks if prompt and type flags override the config.
func TestApplyAugmentationOverrides(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	newPrompt := testCustomPrompt
	newType := testAugmentationType

	applyAugmentationOverrides(cfg, newPrompt, newType)

	assert.Equal(t, newPrompt, cfg.Augmentation.CustomPrompt)
	assert.Equal(t, newType, cfg.Augmentation.Type)
}

// TestProcessSingleFile_Success verifies the logic for processing one file.
func TestProcessSingleFile_Success(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pngFile := filepath.Join(tmpDir, "test.png")
	_, createErr := os.Create(pngFile)
	require.NoError(t, createErr)

	// All fields are explicitly initialized to satisfy the exhaustruct linter.
	mockPipe := &mockPipeline{
		processSingleCalled:    false,
		processDirectoryCalled: false,
		processSingleErr:       nil,
		processDirectoryErr:    nil,
	}
	mockLog := &mockLogger{mu: sync.Mutex{}, outputs: nil}

	err := processSingleFile(context.Background(), mockPipe, pngFile, tmpDir, mockLog)

	require.NoError(t, err)
	assert.True(t, mockPipe.processSingleCalled)
	assert.Contains(t, mockLog.getLastOutput(), "Processing single file")
}

// TestProcessDirectory_Success verifies the logic for processing a directory.
func TestProcessDirectory_Success(t *testing.T) {
	t.Parallel()
	// All fields are explicitly initialized to satisfy the exhaustruct linter.
	mockPipe := &mockPipeline{
		processSingleCalled:    false,
		processDirectoryCalled: false,
		processSingleErr:       nil,
		processDirectoryErr:    nil,
	}
	mockLog := &mockLogger{mu: sync.Mutex{}, outputs: nil}
	cfg := newTestConfig(t) // Uses temp dirs from config

	err := processDirectory(
		context.Background(),
		mockPipe,
		cfg.Paths.InputDir,
		cfg.Paths.OutputDir,
		mockLog,
	)

	require.NoError(t, err)
	assert.True(t, mockPipe.processDirectoryCalled)
	assert.Contains(t, mockLog.getLastOutput(), "Processing directory")
}

// TestProcessDirectory_MissingDirError checks for the specific error when a dir is
// missing.
func TestProcessDirectory_MissingDirError(t *testing.T) {
	t.Parallel()
	// All fields are explicitly initialized to satisfy the exhaustruct linter.
	mockPipe := &mockPipeline{
		processSingleCalled:    false,
		processDirectoryCalled: false,
		processSingleErr:       nil,
		processDirectoryErr:    nil,
	}
	mockLog := &mockLogger{mu: sync.Mutex{}, outputs: nil}

	err := processDirectory(
		context.Background(),
		mockPipe,
		"",
		"/some/output",
		mockLog,
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDirectoriesRequired)
}

// TestResolveConfigPath_FindsRoot confirms it finds project.toml automatically.
func TestResolveConfigPath_FindsRoot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "cmd")
	require.NoError(t, os.Mkdir(subDir, testDirPermissions))

	expectedConfigPath := filepath.Join(tmpDir, "project.toml")
	_, createErr := os.Create(expectedConfigPath)
	require.NoError(t, createErr)

	// Mock osGetwd by overwriting the package-level variable.
	originalGetwd := osGetwd

	osGetwd = func() (string, error) { return subDir, nil }

	defer func() { osGetwd = originalGetwd }() // Restore original function

	foundPath, err := resolveConfigPath("")

	require.NoError(t, err)
	assert.Equal(t, expectedConfigPath, foundPath)
}

// TestValidateAndSetAugmentationType tests the validation logic for augmentation types.
func TestValidateAndSetAugmentationType(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)

	// We can't directly test os.Exit(1), but we can test the fatalf call
	// by checking if it panics. This requires replacing the package-level fatalf.
	originalFatalf := fatalf

	defer func() { fatalf = originalFatalf }()

	fatalf = func(format string, args ...any) {
		panic(fmt.Sprintf(format, args...))
	}

	// Assert that calling with an invalid type panics with the expected message.
	expectedPanicMsg := fmt.Sprintf(
		"Invalid augmentation type: %s. Must be 'commentary' or 'summary'",
		testInvalidAugmenType,
	)
	assert.PanicsWithValue(t, expectedPanicMsg, func() {
		validateAndSetAugmentationType(cfg, testInvalidAugmenType)
	})
}

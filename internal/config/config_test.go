// Package config_test tests the configuration setting
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nnikolov3/png-to-text-service/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadTestCase defines the structure for table-driven tests for the Load function.
type loadTestCase struct {
	setupEnv      map[string]string
	validateFunc  func(*testing.T, *config.Config)
	name          string
	configContent string
	expectedError string
}

// runLoadTestCase executes a single test case for the Load function, reducing the
// complexity of the main test function.
func runLoadTestCase(t *testing.T, testCase loadTestCase) {
	t.Helper()

	if testCase.setupEnv != nil {
		for key, value := range testCase.setupEnv {
			t.Setenv(key, value)
		}
	}

	tmpFile, createFileErr := os.CreateTemp(t.TempDir(), "test-config-*.toml")
	require.NoError(t, createFileErr)

	_, writeErr := tmpFile.WriteString(testCase.configContent)
	require.NoError(t, writeErr)

	closeErr := tmpFile.Close()
	require.NoError(t, closeErr)

	cfg, loadErr := config.Load(tmpFile.Name())

	if testCase.expectedError != "" {
		require.Error(t, loadErr)
		assert.Contains(t, loadErr.Error(), testCase.expectedError)
		assert.Nil(t, cfg)

		return // End test here if an error is expected.
	}

	require.NoError(t, loadErr)
	require.NotNil(t, cfg)

	if testCase.validateFunc != nil {
		testCase.validateFunc(t, cfg)
	}
}

// TestLoad validates the configuration loading and validation logic.
func TestLoad(t *testing.T) {
	t.Parallel()

	testCases := []loadTestCase{
		{
			name: "valid configuration with all fields",
			configContent: `
[project]
name = "test-service"
[paths]
input_dir = "/tmp/input"
output_dir = "/tmp/output"
[settings]
enable_augmentation = true
[gemini]
api_key_variable = "GEMINI_API_KEY"`,
			expectedError: "",
			setupEnv: map[string]string{
				"GEMINI_API_KEY": "test-key-123",
			},
			validateFunc: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				assert.Equal(t, "test-service", cfg.Project.Name)
				assert.True(t, cfg.Settings.EnableAugmentation)
			},
		},
		{
			name: "missing input directory fails validation",
			configContent: `[paths]
output_dir = "/tmp/output"`,
			expectedError: "paths.input_dir is required",
			setupEnv:      nil,
			validateFunc:  nil,
		},
	}

	for _, currentTest := range testCases {
		t.Run(currentTest.name, func(t *testing.T) {
			t.Parallel()
			runLoadTestCase(t, currentTest)
		})
	}
}

// TestConfigGetAPIKey verifies API key retrieval from environment variables.
func TestConfigGetAPIKey(t *testing.T) {
	t.Parallel()

	// Initialize config with all fields and sub-fields to satisfy exhaustruct.
	cfg := &config.Config{
		Project: config.Project{
			Name:        "",
			Version:     "",
			Description: "",
		},
		Paths: config.Paths{
			InputDir:  "",
			OutputDir: "",
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
		Settings: config.Settings{
			Workers:            0,
			TimeoutSeconds:     0,
			EnableAugmentation: false,
			SkipExisting:       false,
		},
		Gemini: config.Gemini{
			APIKeyVariable:    "TEST_API_KEY",
			Models:            nil,
			MaxRetries:        0,
			RetryDelaySeconds: 0,
			TimeoutSeconds:    0,
			Temperature:       0,
			TopK:              0,
			TopP:              0,
			MaxTokens:         0,
		},
	}

	t.Setenv("TEST_API_KEY", "key-123")

	apiKey := cfg.GetAPIKey()
	assert.Equal(t, "key-123", apiKey)
}

// TestConfigEnsureDirectories checks that required directories are created.
func TestConfigEnsureDirectories(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Initialize config with all fields and sub-fields to satisfy exhaustruct.
	cfg := &config.Config{
		Project: config.Project{
			Name:        "",
			Version:     "",
			Description: "",
		},
		Paths: config.Paths{
			InputDir:  filepath.Join(tmpDir, "input"),
			OutputDir: filepath.Join(tmpDir, "output"),
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
			Dir:                  filepath.Join(tmpDir, "logs"),
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
			TimeoutSeconds:     0,
			EnableAugmentation: false,
			SkipExisting:       false,
		},
	}

	err := cfg.EnsureDirectories()
	require.NoError(t, err)

	dirs := []string{cfg.Paths.InputDir, cfg.Paths.OutputDir, cfg.Logging.Dir}
	for _, dir := range dirs {
		info, statErr := os.Stat(dir)
		require.NoError(t, statErr, "Directory should exist: %s", dir)
		assert.True(t, info.IsDir(), "Path should be a directory: %s", dir)
	}
}

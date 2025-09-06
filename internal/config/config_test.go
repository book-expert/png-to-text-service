package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nnikolov3/png-to-text-service/internal/config"
)

// Constants for test data and configuration content.
const (
	testAPIKeyEnvName     = "GEMINI_API_KEY"
	testAPIKeySecretValue = "test-key-12345"
	testProjectName       = "test-service"
	testInputDir          = "/tmp/test-input"
	testOutputDir         = "/tmp/test-output"
	testLogsDirName       = "logs"
	testLogFileName       = "app.log"
	nonExistentPath       = "non/existent/path/project.toml"
	defaultDirPermissions = 0o755
)

// newTestConfig is a helper that returns a valid, fully-populated config struct.
// This simplifies test setup and satisfies the exhaustruct linter.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()

	tmpDir := t.TempDir()

	return &config.Config{
		Project: config.Project{
			Name:        testProjectName,
			Version:     "1.0.0",
			Description: "Test Description",
		},
		Paths: config.Paths{
			InputDir:  filepath.Join(tmpDir, "input"),
			OutputDir: filepath.Join(tmpDir, "output"),
		},
		Logging: config.Logging{
			Level:                "info",
			Dir:                  filepath.Join(tmpDir, testLogsDirName),
			EnableFileLogging:    true,
			EnableConsoleLogging: true,
		},
		Gemini: config.Gemini{
			APIKeyVariable:    testAPIKeyEnvName,
			Models:            []string{"gemini-pro"},
			MaxRetries:        3,
			RetryDelaySeconds: 5,
			TimeoutSeconds:    60,
			Temperature:       0.5,
			TopK:              40,
			TopP:              0.9,
			MaxTokens:         2048,
		},
		Prompts: config.Prompts{
			Augmentation: "Test prompt",
		},
		Augmentation: config.Augmentation{
			Type:             "commentary",
			CustomPrompt:     "",
			UsePromptBuilder: true,
		},
		Tesseract: config.Tesseract{
			Language:       "eng",
			OEM:            3,
			PSM:            3,
			DPI:            300,
			TimeoutSeconds: 60,
		},
		Settings: config.Settings{
			Workers:            4,
			TimeoutSeconds:     120,
			EnableAugmentation: true,
			SkipExisting:       false,
		},
	}
}

// createTempConfigFile is a helper to create a temporary TOML file.
func createTempConfigFile(t *testing.T, content string) string {
	t.Helper()

	tmpfile, createErr := os.CreateTemp(t.TempDir(), "config.*.toml")
	require.NoError(t, createErr)

	_, writeErr := tmpfile.WriteString(content)
	require.NoError(t, writeErr)

	closeErr := tmpfile.Close()
	require.NoError(t, closeErr)

	return tmpfile.Name()
}

// TestLoad_Success tests loading a valid configuration file.
func TestLoad_Success(t *testing.T) {
	t.Parallel()

	validConfigContent := fmt.Sprintf(`
[project]
name = "%s"
[paths]
input_dir = "%s"
output_dir = "%s"
[settings]
enable_augmentation = true
[gemini]
api_key_variable = "%s"`, testProjectName, testInputDir, testOutputDir, testAPIKeyEnvName)
	configPath := createTempConfigFile(t, validConfigContent)

	cfg, loadErr := config.Load(configPath)

	require.NoError(t, loadErr)
	require.NotNil(t, cfg)
	assert.Equal(t, testProjectName, cfg.Project.Name)
	assert.True(t, cfg.Settings.EnableAugmentation)
	assert.Equal(t, testAPIKeyEnvName, cfg.Gemini.APIKeyVariable)
}

// TestLoad_MissingRequiredField tests that validation catches a missing input directory.
func TestLoad_MissingRequiredField(t *testing.T) {
	t.Parallel()

	missingInputDirConfigContent := fmt.Sprintf(`
[paths]
output_dir = "%s"`, testOutputDir)
	configPath := createTempConfigFile(t, missingInputDirConfigContent)

	cfg, loadErr := config.Load(configPath)

	require.Error(t, loadErr)
	require.ErrorIs(t, loadErr, config.ErrInputDirRequired)
	require.Nil(t, cfg)
}

// TestLoad_MissingAPIKeyVariable tests validation fails if augmentation is enabled
// without an API key variable.
func TestLoad_MissingAPIKeyVariable(t *testing.T) {
	t.Parallel()

	configContent := fmt.Sprintf(`
[paths]
input_dir = "%s"
output_dir = "%s"
[settings]
enable_augmentation = true
[gemini]
api_key_variable = ""`, testInputDir, testOutputDir)
	configPath := createTempConfigFile(t, configContent)

	cfg, loadErr := config.Load(configPath)

	require.Error(t, loadErr)
	require.ErrorIs(t, loadErr, config.ErrAPIKeyVarRequired)
	assert.Nil(t, cfg)
}

// TestLoad_DefaultsApplied tests that default values are set correctly.
func TestLoad_DefaultsApplied(t *testing.T) {
	t.Parallel()

	configContent := fmt.Sprintf(`
[paths]
input_dir = "%s"
output_dir = "%s"`, testInputDir, testOutputDir)
	configPath := createTempConfigFile(t, configContent)

	cfg, loadErr := config.Load(configPath)

	require.NoError(t, loadErr)
	require.NotNil(t, cfg)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, 3, cfg.Tesseract.OEM)
	assert.Equal(t, 4, cfg.Settings.Workers) // Default worker count
}

// TestLoad_FileNotExist tests that Load returns an error for a non-existent file.
func TestLoad_FileNotExist(t *testing.T) {
	t.Parallel()

	cfg, loadErr := config.Load(nonExistentPath)
	require.Error(t, loadErr)
	assert.Nil(t, cfg)
}

// TestGetAPIKey_Success verifies API key retrieval from the environment.
func TestGetAPIKey_Success(t *testing.T) {
	t.Setenv(testAPIKeyEnvName, testAPIKeySecretValue)

	cfg := newTestConfig(t)

	apiKey := cfg.GetAPIKey()

	assert.Equal(t, testAPIKeySecretValue, apiKey)
}

// TestGetAPIKey_NotSet verifies an empty string is returned if the env var is not set.
func TestGetAPIKey_NotSet(t *testing.T) {
	t.Setenv(testAPIKeyEnvName, "")

	cfg := newTestConfig(t)

	apiKey := cfg.GetAPIKey()

	assert.Empty(t, apiKey)
}

// TestEnsureDirectories_CreatesAll checks that required directories are created.
func TestEnsureDirectories_CreatesAll(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)

	cfg.Logging.Dir = filepath.Join(cfg.Paths.InputDir, "..", "logs-new")

	ensureErr := cfg.EnsureDirectories()
	require.NoError(t, ensureErr)

	dirs := []string{cfg.Paths.InputDir, cfg.Paths.OutputDir, cfg.Logging.Dir}
	for _, dir := range dirs {
		info, statErr := os.Stat(dir)
		require.NoError(t, statErr, "Directory should exist: %s", dir)
		assert.True(t, info.IsDir(), "Path should be a directory: %s", dir)
	}
}

// TestEnsureDirectories_SucceedsOnExisting verifies the function succeeds if paths exist.
func TestEnsureDirectories_SucceedsOnExisting(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	require.NoError(t, os.MkdirAll(cfg.Paths.InputDir, defaultDirPermissions))
	require.NoError(t, os.MkdirAll(cfg.Paths.OutputDir, defaultDirPermissions))

	ensureErr := cfg.EnsureDirectories()

	assert.NoError(
		t,
		ensureErr,
		"Function should succeed even if directories already exist.",
	)
}

// TestGetLogFilePath tests the construction of a log file path.
func TestGetLogFilePath(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	expectedPath := filepath.Join(cfg.Logging.Dir, testLogFileName)

	actualPath := cfg.GetLogFilePath(testLogFileName)

	assert.Equal(t, expectedPath, actualPath)
}

// TestFindProjectRoot_Success tests finding the project root successfully.
func TestFindProjectRoot_Success(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "some", "nested", "dir")
	require.NoError(t, os.MkdirAll(subDir, defaultDirPermissions))

	configPath := filepath.Join(tmpDir, "project.toml")
	_, createErr := os.Create(configPath)
	require.NoError(t, createErr)

	rootDir, foundPath, findErr := config.FindProjectRoot(subDir)

	require.NoError(t, findErr)
	assert.Equal(t, tmpDir, rootDir)
	assert.Equal(t, configPath, foundPath)
}

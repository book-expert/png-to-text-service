package config_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/book-expert/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/book-expert/png-to-text-service/internal/config"
)

// Constants for test data and configuration content.
const (
	testProjectName       = "test-service"
	testLogsDirName       = "logs"
	testLogFileName       = "app.log"
	defaultDirPermissions = 0o755
)

// newTestLogger creates a logger for testing purposes.
func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(t.TempDir(), "test.log")
	require.NoError(t, err)

	return log
}

// newTestConfig is a helper that returns a valid, fully-populated config struct.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()

	tmpDir := t.TempDir()
	apiKeyEnvName := "GEMINI_API_KEY"

	return &config.Config{
		Project: config.Project{
			Name:        testProjectName,
			Version:     "1.0.0",
			Description: "Test Description",
		},
		Paths: config.PathsConfig{
			BaseLogsDir: filepath.Join(tmpDir, "base_logs"),
		},
		PNGToTextService: config.PNGToTextServiceConfig{
			Logging: config.Logging{
				Level: "info",
				Dir: filepath.Join(
					tmpDir,
					testLogsDirName,
				),
				EnableFileLogging:    true,
				EnableConsoleLogging: true,
			},
			Gemini: config.Gemini{
				APIKeyVariable:    apiKeyEnvName,
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
		},
		NATS: config.NATSConfig{
			URL:                      "nats://127.0.0.1:4222",
			PDFStreamName:            "PDF_JOBS",
			PDFConsumerName:          "pdf-workers",
			PDFCreatedSubject:        "pdf.created",
			PDFObjectStoreBucket:     "PDF_FILES",
			PNGStreamName:            "PNG_PROCESSING",
			PNGConsumerName:          "png-text-workers",
			PNGCreatedSubject:        "png.created",
			PNGObjectStoreBucket:     "PNG_FILES",
			TextObjectStoreBucket:    "TEXT_FILES",
			TTSStreamName:            "TTS_JOBS",
			TTSConsumerName:          "tts-workers",
			TextProcessedSubject:     "text.processed",
			AudioChunkCreatedSubject: "audio.chunk.created",
			AudioObjectStoreBucket:   "AUDIO_FILES",
			TextStreamName:           "TTS_JOBS",
			DeadLetterSubject:        "",
		},
	}
}

// TestLoad_Success tests loading a valid configuration file.
func TestLoad_Success(t *testing.T) {
	log := newTestLogger(t)

	apiKeyEnvName := "GEMINI_API_KEY"
	validConfigContent := fmt.Sprintf(`
[project]
name = "%s"
[png_to_text_service.gemini]
api_key_variable = "%s"`, testProjectName, apiKeyEnvName)
	configPath := createTempConfigFile(t, validConfigContent)

	url, cleanup := startTestServer(t, configPath)
	t.Cleanup(cleanup)
	t.Setenv("PROJECT_TOML", url)

	cfg, loadErr := config.Load("", log)

	require.NoError(t, loadErr)
	require.NotNil(t, cfg)
	assert.Equal(t, testProjectName, cfg.Project.Name)
	assert.Equal(t, apiKeyEnvName, cfg.PNGToTextService.Gemini.APIKeyVariable)
}

// TestLoad_DefaultsApplied tests that default values are set correctly.
func TestLoad_DefaultsApplied(t *testing.T) {
	log := newTestLogger(t)

	configContent := `
[project]
name = "test"`
	configPath := createTempConfigFile(t, configContent)

	url, cleanup := startTestServer(t, configPath)
	t.Cleanup(cleanup)
	t.Setenv("PROJECT_TOML", url)

	cfg, loadErr := config.Load("", log)

	require.NoError(t, loadErr)
	require.NotNil(t, cfg)
	assert.Equal(t, "info", cfg.PNGToTextService.Logging.Level)
	assert.Equal(t, 3, cfg.PNGToTextService.Tesseract.OEM)
}

// TestLoad_URLNotSet tests that Load returns an error if the PROJECT_TOML env var is not
// set.
func TestLoad_URLNotSet(t *testing.T) {
	log := newTestLogger(t)

	// Unset the env var to ensure it's not present
	t.Setenv("PROJECT_TOML", "")

	cfg, loadErr := config.Load("", log)

	require.Error(t, loadErr)
	assert.Nil(t, cfg)
}

// TestLoad_BadURL tests that Load returns an error for a malformed URL.
func TestLoad_BadURL(t *testing.T) {
	log := newTestLogger(t)

	// Set the env var to a bad URL
	t.Setenv("PROJECT_TOML", "http://a b.com")

	cfg, loadErr := config.Load("", log)

	require.Error(t, loadErr)
	assert.Nil(t, cfg)
}

// TestLoad_Server404 tests that Load returns an error for a 404.
func TestLoad_Server404(t *testing.T) {
	log := newTestLogger(t)

	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	t.Setenv("PROJECT_TOML", server.URL)

	cfg, loadErr := config.Load("", log)

	require.Error(t, loadErr)
	assert.Nil(t, cfg)
}

// TestGetAPIKey_Success verifies API key retrieval from the environment.
func TestGetAPIKey_Success(t *testing.T) {
	apiKeyEnvName := "GEMINI_API_KEY"
	apiKeySecretValue := "test-key-12345"
	t.Setenv(apiKeyEnvName, apiKeySecretValue)

	cfg := newTestConfig(t)

	apiKey := cfg.GetAPIKey()

	assert.Equal(t, apiKeySecretValue, apiKey)
}

// TestGetAPIKey_NotSet verifies an empty string is returned if the env var is not set.
func TestGetAPIKey_NotSet(t *testing.T) {
	apiKeyEnvName := "GEMINI_API_KEY"
	t.Setenv(apiKeyEnvName, "")

	cfg := newTestConfig(t)

	apiKey := cfg.GetAPIKey()

	assert.Empty(t, apiKey)
}

// TestEnsureDirectories_CreatesAll checks that required directories are created.
func TestEnsureDirectories_CreatesAll(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)

	// Override log dir to test creation
	logDir := filepath.Join(t.TempDir(), "new-logs")
	cfg.PNGToTextService.Logging.Dir = logDir

	ensureErr := cfg.EnsureDirectories()
	require.NoError(t, ensureErr)

	info, statErr := os.Stat(logDir)
	require.NoError(t, statErr, "Log directory should exist: %s", logDir)
	assert.True(t, info.IsDir(), "Path should be a directory: %s", logDir)
}

// TestGetLogFilePath tests the construction of a log file path.
func TestGetLogFilePath(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	expectedPath := filepath.Join(cfg.PNGToTextService.Logging.Dir, testLogFileName)

	actualPath := cfg.GetLogFilePath(testLogFileName)

	assert.Equal(t, expectedPath, actualPath)
}

// createTempConfigFile creates a temporary TOML config file and returns its path.
func createTempConfigFile(t *testing.T, content string) string {
	t.Helper()

	tmpFile, err := os.CreateTemp(t.TempDir(), "config.*.toml")
	require.NoError(t, err)

	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)

	err = tmpFile.Close()
	require.NoError(t, err)

	return tmpFile.Name()
}

// startTestServer starts a local HTTP server to serve a given file path.
// It returns the server instance and the URL where the file is served.
func startTestServer(t *testing.T, filePath string) (string, func()) {
	t.Helper()

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filePath)
		}),
	)

	url := fmt.Sprintf("%s/%s", server.URL, filepath.Base(filePath))

	return url, server.Close
}

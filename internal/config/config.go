// Package config provides configuration management for the PNG-to-text service.
// It loads, validates, and provides access to configuration from project.toml files,
// ensuring that the application has valid settings before execution.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/book-expert/configurator"
	"github.com/book-expert/logger"
)

// Constants for default values and permissions.
const (
	// defaultDirPermission defines the default permissions for created directories.
	defaultDirPermission = 0o750

	// defaultTesseractOEM is the default OCR Engine Mode for Tesseract.
	defaultTesseractOEM = 3
	// defaultTesseractPSM is the default Page Segmentation Mode for Tesseract.
	defaultTesseractPSM = 3
	// defaultTesseractDPI is the default DPI for Tesseract processing.
	defaultTesseractDPI = 300
	// defaultTesseractTimeoutSeconds is the default timeout for Tesseract operations.
	defaultTesseractTimeoutSeconds = 120

	// defaultGeminiMaxRetries is the default number of retries for Gemini API calls.
	defaultGeminiMaxRetries = 3
	// defaultGeminiRetryDelaySeconds is the default delay between retries for Gemini
	// API calls.
	defaultGeminiRetryDelaySeconds = 30
	// defaultGeminiTimeoutSeconds is the default timeout for Gemini API calls.
	defaultGeminiTimeoutSeconds = 120
	// defaultGeminiMaxTokens is the default maximum number of tokens for Gemini
	// responses.
	defaultGeminiMaxTokens = 8192
)

// Pre-defined errors for configuration validation.
var (
	ErrAPIKeyVarRequired = errors.New(
		"gemini.api_key_variable is required when augmentation is enabled",
	)
)

// NATSConfig holds all NATS and JetStream settings.
type NATSConfig struct {
	URL                      string `toml:"url"`
	PDFStreamName            string `toml:"pdf_stream_name"`
	PDFConsumerName          string `toml:"pdf_consumer_name"`
	PDFCreatedSubject        string `toml:"pdf_created_subject"`
	PDFObjectStoreBucket     string `toml:"pdf_object_store_bucket"`
	PNGStreamName            string `toml:"png_stream_name"`
	PNGConsumerName          string `toml:"png_consumer_name"`
	PNGCreatedSubject        string `toml:"png_created_subject"`
	PNGObjectStoreBucket     string `toml:"png_object_store_bucket"`
	TextStreamName           string `toml:"text_stream_name"`
	TextObjectStoreBucket    string `toml:"text_object_store_bucket"`
	TTSStreamName            string `toml:"tts_stream_name"`
	TTSConsumerName          string `toml:"tts_consumer_name"`
	TextProcessedSubject     string `toml:"text_processed_subject"`
	AudioChunkCreatedSubject string `toml:"audio_chunk_created_subject"`
	AudioObjectStoreBucket   string `toml:"audio_object_store_bucket"`
	DeadLetterSubject        string `toml:"dead_letter_subject"`
}

// PathsConfig holds common path configurations.
type PathsConfig struct {
	BaseLogsDir string `toml:"base_logs_dir"`
}

// PNGToTextServiceConfig holds all settings specific to this service.
type PNGToTextServiceConfig struct {
	Tesseract    Tesseract    `toml:"tesseract"`
	Gemini       Gemini       `toml:"gemini"`
	Logging      Logging      `toml:"logging"`
	Augmentation Augmentation `toml:"augmentation"`
	Prompts      Prompts      `toml:"prompts"`
}

// Config represents the complete, validated configuration for the service.
type Config struct {
	Project          Project                `toml:"project"`
	NATS             NATSConfig             `toml:"nats"`
	Paths            PathsConfig            `toml:"paths"`
	PNGToTextService PNGToTextServiceConfig `toml:"png_to_text_service"`
}

// Project contains project metadata.
type Project struct {
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
}

// Prompts contains AI prompts used for augmentation.
type Prompts struct {
	Augmentation string `toml:"augmentation"`
}

// Augmentation defines settings for AI text augmentation.
type Augmentation struct {
	Type             string `toml:"type"`
	CustomPrompt     string `toml:"custom_prompt"`
	UsePromptBuilder bool   `toml:"use_prompt_builder"`
}

// Logging defines the configuration for logging.
type Logging struct {
	Level                string `toml:"level"`
	Dir                  string `toml:"dir"`
	EnableFileLogging    bool   `toml:"enable_file_logging"`
	EnableConsoleLogging bool   `toml:"enable_console_logging"`
}

// Tesseract holds configuration for the Tesseract OCR engine.
type Tesseract struct {
	Language       string `toml:"language"`
	OEM            int    `toml:"oem"`
	PSM            int    `toml:"psm"`
	DPI            int    `toml:"dpi"`
	TimeoutSeconds int    `toml:"timeout_seconds"`
}

// Gemini holds configuration for the Gemini AI model.
type Gemini struct {
	APIKeyVariable    string   `toml:"api_key_variable"`
	Models            []string `toml:"models"`
	MaxRetries        int      `toml:"max_retries"`
	RetryDelaySeconds int      `toml:"retry_delay_seconds"`
	TimeoutSeconds    int      `toml:"timeout_seconds"`
	Temperature       float64  `toml:"temperature"`
	TopK              int      `toml:"top_k"`
	TopP              float64  `toml:"top_p"`
	MaxTokens         int      `toml:"max_tokens"`
}

// Load reads, validates, and sets defaults for the configuration.
// If a configPath is provided, it sets the PROJECT_TOML env var for the configurator to
// use.
// Otherwise, it relies on the existing environment variable.
func Load(configPath string, log *logger.Logger) (*Config, error) {
	if configPath != "" {
		if err := os.Setenv("PROJECT_TOML", configPath); err != nil {
			return nil, fmt.Errorf(
				"failed to set PROJECT_TOML env var for test: %w",
				err,
			)
		}
	}

	var cfg Config
	err := configurator.Load(&cfg, log)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	err = cfg.validate()
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// GetAPIKey retrieves the Gemini API key from the environment variable specified
// in the configuration.
func (c *Config) GetAPIKey() string {
	if c.PNGToTextService.Gemini.APIKeyVariable == "" {
		return ""
	}
	return os.Getenv(c.PNGToTextService.Gemini.APIKeyVariable)
}

// EnsureDirectories creates the logging directory if it does not already exist.
func (c *Config) EnsureDirectories() error {
	logDir := c.PNGToTextService.Logging.Dir
	if logDir == "" {
		logDir = c.Paths.BaseLogsDir
	}
	err := os.MkdirAll(logDir, defaultDirPermission)
	if err != nil {
		return fmt.Errorf("failed to create log directory %s: %w", logDir, err)
	}
	return nil
}

// GetLogFilePath constructs the full path for a given log filename.
func (c *Config) GetLogFilePath(filename string) string {
	logDir := c.PNGToTextService.Logging.Dir
	if logDir == "" {
		logDir = c.Paths.BaseLogsDir
	}
	return filepath.Join(logDir, filename)
}

// validate runs all validation and default-setting routines for the configuration.
func (c *Config) validate() error {
	c.applyDefaults()
	return nil
}

// applyDefaults sets default values for various configuration sections.
func (c *Config) applyDefaults() {
	// Tesseract defaults
	setStringDefault(&c.PNGToTextService.Tesseract.Language, "eng")
	setIntDefault(&c.PNGToTextService.Tesseract.OEM, defaultTesseractOEM)
	setIntDefault(&c.PNGToTextService.Tesseract.PSM, defaultTesseractPSM)
	setIntDefault(&c.PNGToTextService.Tesseract.DPI, defaultTesseractDPI)
	setIntDefault(
		&c.PNGToTextService.Tesseract.TimeoutSeconds,
		defaultTesseractTimeoutSeconds,
	)

	// Augmentation defaults
	setStringDefault(&c.PNGToTextService.Augmentation.Type, "commentary")

	// Logging defaults
	setStringDefault(&c.PNGToTextService.Logging.Level, "info")
	if c.PNGToTextService.Logging.Dir == "" {
		c.PNGToTextService.Logging.Dir = c.Paths.BaseLogsDir
	}

	// Gemini defaults
	if len(c.PNGToTextService.Gemini.Models) == 0 {
		c.PNGToTextService.Gemini.Models = []string{"gemini-1.5-flash"}
	}
	setIntDefault(&c.PNGToTextService.Gemini.MaxRetries, defaultGeminiMaxRetries)
	setIntDefault(
		&c.PNGToTextService.Gemini.RetryDelaySeconds,
		defaultGeminiRetryDelaySeconds,
	)
	setIntDefault(
		&c.PNGToTextService.Gemini.TimeoutSeconds,
		defaultGeminiTimeoutSeconds,
	)
	setIntDefault(&c.PNGToTextService.Gemini.MaxTokens, defaultGeminiMaxTokens)
}

// setStringDefault sets a default string value if the target field is empty.
func setStringDefault(field *string, defaultValue string) {
	if *field == "" {
		*field = defaultValue
	}
}

// setIntDefault sets a default integer value if the target field is non-positive.
func setIntDefault(field *int, defaultValue int) {
	if *field <= 0 {
		*field = defaultValue
	}
}

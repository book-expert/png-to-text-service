// Package config provides configuration management for the PNG-to-text service.
// It loads, validates, and provides access to configuration from project.toml files,
// ensuring that the application has valid settings before execution.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/book-expert/configurator"
	"github.com/book-expert/events"
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
	ErrTTSVoiceRequired = errors.New("png_to_text_service.tts_defaults.voice is required")
)

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
    TTSDefaults  TTSDefaults  `toml:"tts_defaults"`
    // DeadLetterSubject is the subject to publish failed messages to (DLQ).
    DeadLetterSubject string `toml:"dead_letter_subject"`
}

// Config represents the complete, validated configuration for the service.
type Config struct {
	Project          Project                       `toml:"project"`
	ServiceNATS      configurator.ServiceNATSConfig `toml:"png-to-text-service"`
	Paths            PathsConfig                   `toml:"paths"`
	PNGToTextService PNGToTextServiceConfig        `toml:"png_to_text_service"`
}

// Project contains project metadata.
type Project struct {
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
}

// Prompts contains AI prompts used for augmentation.
type Prompts struct {
	CommentaryBase string `toml:"commentary_base"`
	SummaryBase    string `toml:"summary_base"`
}

// Augmentation defines settings for AI text augmentation.
type Augmentation struct {
	UsePromptBuilder bool                 `toml:"use_prompt_builder"`
	Parameters       map[string]any       `toml:"parameters"`
	Defaults         AugmentationDefaults `toml:"defaults"`
}

// AugmentationDefaults captures the default augmentation behaviour when a workflow
// does not override preferences.
type AugmentationDefaults struct {
	Commentary AugmentationCommentaryDefaults `toml:"commentary"`
	Summary    AugmentationSummaryDefaults    `toml:"summary"`
}

// AugmentationCommentaryDefaults defines commentary fallback behaviour.
type AugmentationCommentaryDefaults struct {
	Enabled         bool   `toml:"enabled"`
	CustomAdditions string `toml:"custom_additions"`
}

// AugmentationSummaryDefaults defines summary fallback behaviour.
type AugmentationSummaryDefaults struct {
	Enabled         bool                    `toml:"enabled"`
	Placement       events.SummaryPlacement `toml:"placement"`
	CustomAdditions string                  `toml:"custom_additions"`
}

// SummaryPlacement mirrors events.SummaryPlacement to expose meaningful constants to
// configuration consumers without requiring them to import the events package.
type SummaryPlacement = events.SummaryPlacement

const (
	// SummaryPlacementTop places summaries before the OCR text.
	SummaryPlacementTop SummaryPlacement = events.SummaryPlacementTop
	// SummaryPlacementBottom places summaries after the OCR text.
	SummaryPlacementBottom SummaryPlacement = events.SummaryPlacementBottom
)

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

// TTSDefaults holds default text-to-speech parameters that will be propagated to downstream services.
type TTSDefaults struct {
	Voice             string  `toml:"voice"`
	Seed              int     `toml:"seed"`
	NGL               int     `toml:"ngl"`
	TopP              float64 `toml:"top_p"`
	RepetitionPenalty float64 `toml:"repetition_penalty"`
	Temperature       float64 `toml:"temperature"`
}

// Load is the single entry point for loading, validating, and setting defaults for the
// service configuration. It ensures that the application starts with a valid and
// complete configuration.
func Load(configPath string, log *logger.Logger) (*Config, error) {
	if configPath != "" {
		err := os.Setenv("PROJECT_TOML", configPath)
		if err != nil {
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

// GetAPIKey is a helper function that abstracts away the access to the Gemini API key.
// It retrieves the key from the environment variable specified in the configuration,
// providing a single, consistent way to access the key throughout the application.
func (c *Config) GetAPIKey() string {
	if c.PNGToTextService.Gemini.APIKeyVariable == "" {
		return ""
	}

	return os.Getenv(c.PNGToTextService.Gemini.APIKeyVariable)
}

// EnsureDirectories is a helper function that ensures the log directory exists before
// the application tries to write to it. This prevents the application from crashing
// if the log directory is missing.
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

// GetLogFilePath is a helper function that constructs the full path for a given log
// filename. This provides a single, consistent way to get the log file path
// throughout the application.
func (c *Config) GetLogFilePath(filename string) string {
	logDir := c.PNGToTextService.Logging.Dir
	if logDir == "" {
		logDir = c.Paths.BaseLogsDir
	}

	return filepath.Join(logDir, filename)
}

// validate is the main validation entry point for the configuration. It calls
// applyDefaults to set default values and then runs all validation checks.
func (c *Config) validate() error {
	c.applyDefaults()

	if c.PNGToTextService.Augmentation.UsePromptBuilder &&
		c.PNGToTextService.Gemini.APIKeyVariable == "" {
		return ErrAPIKeyVarRequired
	}

	if strings.TrimSpace(c.PNGToTextService.TTSDefaults.Voice) == "" {
		return ErrTTSVoiceRequired
	}

	return nil
}

// applyDefaults is responsible for setting all the default values for the
// configuration. This ensures that the application has a complete and valid
// configuration even if some values are not specified in the project.toml file.
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
	// Ensure defaults exist for augmentation selections.
	if c.PNGToTextService.Augmentation.Defaults.Summary.Placement == "" {
		c.PNGToTextService.Augmentation.Defaults.Summary.Placement = SummaryPlacementBottom
	}

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

	setStringDefault(&c.PNGToTextService.TTSDefaults.Voice, "default")

	if c.PNGToTextService.TTSDefaults.TopP <= 0 {
		c.PNGToTextService.TTSDefaults.TopP = 0.95
	}

	if c.PNGToTextService.TTSDefaults.RepetitionPenalty < 1.0 {
		c.PNGToTextService.TTSDefaults.RepetitionPenalty = 1.0
	}

	if c.PNGToTextService.TTSDefaults.Temperature <= 0 {
		c.PNGToTextService.TTSDefaults.Temperature = 0.7
	}
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

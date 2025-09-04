// Package config provides configuration management for the PNG-to-text service.
// It loads, validates, and provides access to configuration from project.toml files,
// ensuring that the application has valid settings before execution.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nnikolov3/configurator"
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
	// ErrInputDirRequired is returned when the input directory path is missing.
	ErrInputDirRequired = errors.New("paths.input_dir is required")
	// ErrOutputDirRequired is returned when the output directory path is missing.
	ErrOutputDirRequired = errors.New("paths.output_dir is required")
	// ErrAPIKeyVarRequired is returned when the API key variable is required but not
	// set.
	ErrAPIKeyVarRequired = errors.New(
		"gemini.api_key_variable is required when augmentation is enabled",
	)
)

// Config represents the complete, validated configuration for the service.
type Config struct {
	Project      Project      `toml:"project"`
	Paths        Paths        `toml:"paths"`
	Prompts      Prompts      `toml:"prompts"`
	Augmentation Augmentation `toml:"augmentation"`
	Logging      Logging      `toml:"logging"`
	Tesseract    Tesseract    `toml:"tesseract"`
	Gemini       Gemini       `toml:"gemini"`
	Settings     Settings     `toml:"settings"`
}

// Project contains project metadata.
type Project struct {
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
}

// Paths contains input and output directory paths.
type Paths struct {
	InputDir  string `toml:"input_dir"`
	OutputDir string `toml:"output_dir"`
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

// Settings contains general processing settings for the service.
type Settings struct {
	Workers            int  `toml:"workers"`
	TimeoutSeconds     int  `toml:"timeout_seconds"`
	EnableAugmentation bool `toml:"enable_augmentation"`
	SkipExisting       bool `toml:"skip_existing"`
}

// Load reads, validates, and sets defaults for the configuration from a given path.
func Load(configPath string) (*Config, error) {
	var cfg Config

	err := configurator.LoadInto(configPath, &cfg)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to load configuration from %s: %w",
			configPath,
			err,
		)
	}

	err = cfg.validate()
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// FindProjectRoot searches upwards from a starting directory to find a "project.toml"
// file, returning the project's root directory and the full path to the config file.
func FindProjectRoot(startDir string) (rootDir, configPath string, err error) {
	rootDir, configPath, err = configurator.FindProjectRoot(startDir)
	if err != nil {
		return "", "", fmt.Errorf(
			"failed to find project root from %s: %w",
			startDir,
			err,
		)
	}

	return rootDir, configPath, nil
}

// GetAPIKey retrieves the Gemini API key from the environment variable specified
// in the configuration. It returns an empty string if the variable is not set.
func (c *Config) GetAPIKey() string {
	if c.Gemini.APIKeyVariable == "" {
		return ""
	}

	return os.Getenv(c.Gemini.APIKeyVariable)
}

// EnsureDirectories creates the input, output, and logging directories if they
// do not already exist, using the permissions defined by defaultDirPermission.
func (c *Config) EnsureDirectories() error {
	dirs := []string{c.Paths.InputDir, c.Paths.OutputDir, c.Logging.Dir}
	for _, dir := range dirs {
		err := os.MkdirAll(dir, defaultDirPermission)
		if err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// GetLogFilePath constructs the full path for a given log filename inside the
// configured logging directory.
func (c *Config) GetLogFilePath(filename string) string {
	return filepath.Join(c.Logging.Dir, filename)
}

// validate runs all validation and default-setting routines for the configuration.
func (c *Config) validate() error {
	err := c.validateRequiredFields()
	if err != nil {
		return err
	}

	c.applyDefaults()

	err = c.validateGeminiSettings()
	if err != nil {
		return err
	}

	return nil
}

// validateRequiredFields checks for the presence of essential configuration values.
func (c *Config) validateRequiredFields() error {
	if c.Paths.InputDir == "" {
		return ErrInputDirRequired
	}

	if c.Paths.OutputDir == "" {
		return ErrOutputDirRequired
	}

	return nil
}

// applyDefaults sets default values for various configuration sections.
func (c *Config) applyDefaults() {
	// Settings defaults
	if c.Settings.Workers <= 0 {
		c.Settings.Workers = 4
	}

	if c.Settings.TimeoutSeconds <= 0 {
		c.Settings.TimeoutSeconds = 300
	}

	// Tesseract defaults
	setStringDefault(&c.Tesseract.Language, "eng")
	setIntDefault(&c.Tesseract.OEM, defaultTesseractOEM)
	setIntDefault(&c.Tesseract.PSM, defaultTesseractPSM)
	setIntDefault(&c.Tesseract.DPI, defaultTesseractDPI)
	setIntDefault(&c.Tesseract.TimeoutSeconds, defaultTesseractTimeoutSeconds)

	// Augmentation defaults
	setStringDefault(&c.Augmentation.Type, "commentary")

	// Logging defaults
	setStringDefault(&c.Logging.Level, "info")
	setStringDefault(&c.Logging.Dir, "./logs")
}

// validateGeminiSettings checks Gemini settings only if augmentation is enabled
// and applies defaults where necessary.
func (c *Config) validateGeminiSettings() error {
	if !c.Settings.EnableAugmentation {
		return nil
	}

	if c.Gemini.APIKeyVariable == "" {
		return ErrAPIKeyVarRequired
	}

	// Gemini defaults (only applied if augmentation is enabled)
	if len(c.Gemini.Models) == 0 {
		c.Gemini.Models = []string{"gemini-1.5-flash"}
	}

	setIntDefault(&c.Gemini.MaxRetries, defaultGeminiMaxRetries)
	setIntDefault(&c.Gemini.RetryDelaySeconds, defaultGeminiRetryDelaySeconds)
	setIntDefault(&c.Gemini.TimeoutSeconds, defaultGeminiTimeoutSeconds)
	setIntDefault(&c.Gemini.MaxTokens, defaultGeminiMaxTokens)

	return nil
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

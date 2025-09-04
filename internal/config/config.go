// ./internal/config/config.go
// Package config provides configuration management for the PNG-to-text service.
// It loads and validates configuration from project.toml files.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nnikolov3/configurator"
)

const (
	// Directory permissions.
	defaultDirPermission = 0o750
)

var (
	ErrInputDirRequired  = errors.New("paths.input_dir is required")
	ErrOutputDirRequired = errors.New("paths.output_dir is required")
	ErrAPIKeyRequired    = errors.New(
		"gemini.api_key_variable is required when augmentation is enabled",
	)
)

// Config represents the complete configuration for the PNG-to-text service.
type Config struct {
	Project struct {
		Name        string `toml:"name"`
		Version     string `toml:"version"`
		Description string `toml:"description"`
	} `toml:"project"`
	Paths struct {
		InputDir  string `toml:"input_dir"`
		OutputDir string `toml:"output_dir"`
	} `toml:"paths"`
	Prompts struct {
		Augmentation string `toml:"augmentation"`
	} `toml:"prompts"`
	Augmentation struct {
		Type             string `toml:"type"`               // "commentary" or "summary"
		CustomPrompt     string `toml:"custom_prompt"`      // Optional custom prompt override
		UsePromptBuilder bool   `toml:"use_prompt_builder"` // Enable prompt-builder integration
	} `toml:"augmentation"`
	Logging struct {
		Level                string `toml:"level"`
		Dir                  string `toml:"dir"`
		EnableFileLogging    bool   `toml:"enable_file_logging"`
		EnableConsoleLogging bool   `toml:"enable_console_logging"`
	} `toml:"logging"`
	Tesseract struct {
		Language       string `toml:"language"`
		OEM            int    `toml:"oem"`
		PSM            int    `toml:"psm"`
		DPI            int    `toml:"dpi"`
		TimeoutSeconds int    `toml:"timeout_seconds"`
	} `toml:"tesseract"`
	Gemini struct {
		APIKeyVariable    string   `toml:"api_key_variable"`
		Models            []string `toml:"models"`
		MaxRetries        int      `toml:"max_retries"`
		RetryDelaySeconds int      `toml:"retry_delay_seconds"`
		TimeoutSeconds    int      `toml:"timeout_seconds"`
		Temperature       float64  `toml:"temperature"`
		TopK              int      `toml:"top_k"`
		TopP              float64  `toml:"top_p"`
		MaxTokens         int      `toml:"max_tokens"`
	} `toml:"gemini"`
	Settings struct {
		Workers            int  `toml:"workers"`
		TimeoutSeconds     int  `toml:"timeout_seconds"`
		EnableAugmentation bool `toml:"enable_augmentation"`
		SkipExisting       bool `toml:"skip_existing"`
	} `toml:"settings"`
}

// Load reads configuration from the specified path using the configurator library.
func Load(configPath string) (*Config, error) {
	var config Config

	err := configurator.LoadInto(configPath, &config)
	if err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("validate configuration: %w", err)
	}

	return &config, nil
}

// FindProjectRoot searches for project.toml starting from the given directory.
func FindProjectRoot(startDir string) (rootDir, configPath string, err error) {
	return configurator.FindProjectRoot(startDir)
}

// GetAPIKey retrieves the API key from the environment variable specified in the
// configuration.
func (c *Config) GetAPIKey() string {
	if c.Gemini.APIKeyVariable == "" {
		return ""
	}

	return os.Getenv(c.Gemini.APIKeyVariable)
}

// EnsureDirectories creates necessary directories if they don't exist.
func (c *Config) EnsureDirectories() error {
	dirs := []string{
		c.Paths.InputDir,
		c.Paths.OutputDir,
		c.Logging.Dir,
	}

	for _, dir := range dirs {
		err := os.MkdirAll(dir, defaultDirPermission)
		if err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	return nil
}

// GetLogFilePath returns the path for log files.
func (c *Config) GetLogFilePath(filename string) string {
	return filepath.Join(c.Logging.Dir, filename)
}

// validate checks that the configuration is valid and sets defaults where appropriate.
func (c *Config) validate() error {
	err := c.validateRequiredFields()
	if err != nil {
		return err
	}

	c.setDefaultSettings()
	c.setTesseractDefaults()
	c.setAugmentationDefaults()

	if err := c.validateGeminiSettings(); err != nil {
		return err
	}

	c.setLoggingDefaults()

	return nil
}

func (c *Config) validateRequiredFields() error {
	if c.Paths.InputDir == "" {
		return ErrInputDirRequired
	}

	if c.Paths.OutputDir == "" {
		return ErrOutputDirRequired
	}

	return nil
}

func (c *Config) setDefaultSettings() {
	if c.Settings.Workers <= 0 {
		c.Settings.Workers = 4
	}

	if c.Settings.TimeoutSeconds <= 0 {
		c.Settings.TimeoutSeconds = 300
	}
}

func (c *Config) setTesseractDefaults() {
	if c.Tesseract.Language == "" {
		c.Tesseract.Language = "eng"
	}

	if c.Tesseract.OEM <= 0 {
		c.Tesseract.OEM = 3
	}

	if c.Tesseract.PSM <= 0 {
		c.Tesseract.PSM = 3
	}

	if c.Tesseract.DPI <= 0 {
		c.Tesseract.DPI = 300
	}

	if c.Tesseract.TimeoutSeconds <= 0 {
		c.Tesseract.TimeoutSeconds = 120
	}
}

func (c *Config) setAugmentationDefaults() {
	if c.Augmentation.Type == "" {
		c.Augmentation.Type = "commentary"
	}
}

func (c *Config) validateGeminiSettings() error {
	if !c.Settings.EnableAugmentation {
		return nil
	}

	if c.Gemini.APIKeyVariable == "" {
		return ErrAPIKeyRequired
	}

	c.setGeminiDefaults()

	return nil
}

func (c *Config) setGeminiDefaults() {
	if len(c.Gemini.Models) == 0 {
		c.Gemini.Models = []string{"gemini-1.5-flash"}
	}

	if c.Gemini.MaxRetries <= 0 {
		c.Gemini.MaxRetries = 3
	}

	if c.Gemini.RetryDelaySeconds <= 0 {
		c.Gemini.RetryDelaySeconds = 30
	}

	if c.Gemini.TimeoutSeconds <= 0 {
		c.Gemini.TimeoutSeconds = 120
	}

	if c.Gemini.MaxTokens <= 0 {
		c.Gemini.MaxTokens = 8192
	}
}

func (c *Config) setLoggingDefaults() {
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}

	if c.Logging.Dir == "" {
		c.Logging.Dir = "./logs"
	}
}

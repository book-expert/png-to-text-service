package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/book-expert/logger"
	"github.com/pelletier/go-toml/v2"
)

const DefaultConfigFilename = "project.toml"

type Config struct {
	Service ServiceSettings `toml:"service"`
	LLM     LLMSettings     `toml:"llm"`
	NATS    NATSSettings    `toml:"nats"`
}

type ServiceSettings struct {
	LogDir  string `toml:"log_dir"`
	Workers int    `toml:"workers"`
}

type LLMSettings struct {
	APIKeyEnvironmentVariable string  `toml:"api_key_variable"`
	BaseURL                   string  `toml:"base_url"`
	Model                     string  `toml:"model"`
	MaxRetries                int     `toml:"max_retries"`
	TimeoutSeconds            int     `toml:"timeout_seconds"`
	Temperature               float64 `toml:"temperature"`
	SystemInstruction         string  `toml:"system_instruction"`
	ExtractionPrompt          string  `toml:"extraction_prompt"`
}


type NATSSettings struct {
	URL         string              `toml:"url"`
	DLQSubject  string              `toml:"dlq_subject"`
	Consumer    ConsumerSettings    `toml:"consumer"`
	Producer    ProducerSettings    `toml:"producer"`
	ObjectStore ObjectStoreSettings `toml:"object_store"`
}

type ConsumerSettings struct {
	Stream  string `toml:"stream"`
	Subject string `toml:"subject"`
	Durable string `toml:"durable"`
}

type ProducerSettings struct {
	Stream  string `toml:"stream"`
	Subject string `toml:"subject"`
}

type ObjectStoreSettings struct {
	PNGBucket  string `toml:"png_bucket"`
	TextBucket string `toml:"text_bucket"`
}

func Load(filePath string, loggerInstance *logger.Logger) (*Config, error) {
	if filePath == "" {
		filePath = DefaultConfigFilename
	}

	configFile, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file '%s': %w", filePath, err)
	}
	defer func() {
		if closeErr := configFile.Close(); closeErr != nil && loggerInstance != nil {
			// Correct: Using Warnf
			loggerInstance.Warnf("Failed to close config file: %v", closeErr)
		}
	}()

	var configuration Config
	decoder := toml.NewDecoder(configFile)
	if err := decoder.Decode(&configuration); err != nil {
		return nil, fmt.Errorf("failed to decode TOML configuration: %w", err)
	}

	return &configuration, nil
}

func (c *Config) GetAPIKey() string {
	return os.Getenv(c.LLM.APIKeyEnvironmentVariable)
}

func (c *Config) GetLogFilePath(filename string) string {
	return filepath.Join(c.Service.LogDir, filename)
}

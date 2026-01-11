/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */

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
	Stream         string `toml:"stream"`
	Subject        string `toml:"subject"`
	StartedSubject string `toml:"started_subject"`
}

type ObjectStoreSettings struct {
	PNGBucket  string `toml:"png_bucket"`
	TextBucket string `toml:"text_bucket"`
}

func Load(filePath string, loggerInstance *logger.Logger) (*Config, error) {
	if filePath == "" {
		filePath = DefaultConfigFilename
	}

	configFile, openError := os.Open(filePath)
	if openError != nil {
		return nil, fmt.Errorf("failed to open config file '%s': %w", filePath, openError)
	}
	defer func() {
		if closeError := configFile.Close(); closeError != nil && loggerInstance != nil {
			loggerInstance.Warnf("Failed to close config file: %v", closeError)
		}
	}()

	var configuration Config
	decoder := toml.NewDecoder(configFile)
	if decodeError := decoder.Decode(&configuration); decodeError != nil {
		return nil, fmt.Errorf("failed to decode TOML configuration: %w", decodeError)
	}

	return &configuration, nil
}

func (configuration *Config) GetAPIKey() string {
	return os.Getenv(configuration.LLM.APIKeyEnvironmentVariable)
}

func (configuration *Config) GetLogFilePath(filename string) string {
	return filepath.Join(configuration.Service.LogDir, filename)
}
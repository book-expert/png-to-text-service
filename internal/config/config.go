/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */

package config

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/book-expert/logger"
	"github.com/pelletier/go-toml/v2"
)

// Config represents the full configuration structure for the service.
type Config struct {
	Service ServiceSettings
	LLM     LLMSettings
	NATS    NATSSettings
}

// ServiceSettings contains general service parameters.
type ServiceSettings struct {
	LogDirectory string
	Workers      int
}

// LLMSettings captures parameters for the Large Language Model provider.
type LLMSettings struct {
	APIKeyEnvironmentVariable string
	BaseURL                   string
	Model                     string
	MaxRetries                int
	TimeoutSeconds            int
	Temperature               float64
	MaxOutputTokens           int
	SystemInstruction         string
	ExtractionPrompt          string
}

// NATSSettings defines connection and consumer settings for NATS.
type NATSSettings struct {
	URL        string
	DLQSubject string
	Consumer   ConsumerSettings
}

// ConsumerSettings defines the JetStream consumer parameters.
type ConsumerSettings struct {
	DurableName string
}

// Prompts is used to unmarshal the prompts.toml file.
type Prompts struct {
	ExtractionPrompt  string `toml:"extraction_prompt"`
	SystemInstruction string `toml:"system_instruction"`
}

// Load retrieves the configuration from environment variables and prompts.toml.
func Load(promptConfigPath string, _ *logger.Logger) (*Config, error) {
	if promptConfigPath == "" {
		promptConfigPath = "prompts.toml"
	}

	var configuration Config

	// 1. Load from Environment Variables
	configuration.Service.LogDirectory = getEnvironmentVariable("PNG_TO_TEXT_LOG_DIR", "/home/niko/development/logs")
	configuration.Service.Workers = getEnvironmentVariableAsInteger("PNG_TO_TEXT_WORKERS", 5)

	configuration.LLM.APIKeyEnvironmentVariable = "GEMINI_API_KEY"
	configuration.LLM.BaseURL = getEnvironmentVariable("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com")
	configuration.LLM.Model = getEnvironmentVariable("PNG_TO_TEXT_LLM_MODEL", "gemini-2.5-flash")
	configuration.LLM.MaxRetries = getEnvironmentVariableAsInteger("PNG_TO_TEXT_MAX_RETRIES", 3)
	configuration.LLM.TimeoutSeconds = getEnvironmentVariableAsInteger("PNG_TO_TEXT_TIMEOUT_SECONDS", 90)
	configuration.LLM.Temperature = getEnvironmentVariableAsFloat("PNG_TO_TEXT_TEMPERATURE", 0.0)
	configuration.LLM.MaxOutputTokens = getEnvironmentVariableAsInteger("PNG_TO_TEXT_MAX_OUTPUT_TOKENS", 8192)

	configuration.NATS.URL = getEnvironmentVariable("NATS_ADDRESS", "nats://localhost:4222")
	configuration.NATS.DLQSubject = getEnvironmentVariable("PNG_TO_TEXT_DLQ_SUBJECT", "png.to.text.dlq")
	configuration.NATS.Consumer.DurableName = getEnvironmentVariable("PNG_TO_TEXT_DURABLE_NAME", "png-to-text-consumer")

	// 2. Load Prompts from prompts.toml
	promptFile, openError := os.Open(promptConfigPath)
	if openError == nil {
		defer func() { _ = promptFile.Close() }()
		var prompts Prompts
		decoder := toml.NewDecoder(promptFile)
		if decodeError := decoder.Decode(&prompts); decodeError == nil {
			configuration.LLM.ExtractionPrompt = prompts.ExtractionPrompt
			configuration.LLM.SystemInstruction = prompts.SystemInstruction
		}
	}

	// Fallback to env if prompts.toml failed or fields were empty
	if configuration.LLM.ExtractionPrompt == "" {
		configuration.LLM.ExtractionPrompt = os.Getenv("PNG_TO_TEXT_EXTRACTION_PROMPT")
	}
	if configuration.LLM.SystemInstruction == "" {
		configuration.LLM.SystemInstruction = os.Getenv("PNG_TO_TEXT_SYSTEM_INSTRUCTION")
	}

	return &configuration, nil
}

func getEnvironmentVariable(keyName, fallbackValue string) string {
	if value, exists := os.LookupEnv(keyName); exists {
		return value
	}
	return fallbackValue
}

func getEnvironmentVariableAsInteger(keyName string, fallbackValue int) int {
	valueString := getEnvironmentVariable(keyName, "")
	if valueString == "" {
		return fallbackValue
	}
	value, error := strconv.Atoi(valueString)
	if error != nil {
		return fallbackValue
	}
	return value
}

func getEnvironmentVariableAsFloat(keyName string, fallbackValue float64) float64 {
	valueString := getEnvironmentVariable(keyName, "")
	if valueString == "" {
		return fallbackValue
	}
	value, error := strconv.ParseFloat(valueString, 64)
	if error != nil {
		return fallbackValue
	}
	return value
}

// GetAPIKey resolves the actual API key value from the configured environment variable.
func (configuration *Config) GetAPIKey() string {
	return os.Getenv(configuration.LLM.APIKeyEnvironmentVariable)
}

// GetLogFilePath constructs an absolute path for a log file.
func (configuration *Config) GetLogFilePath(filename string) string {
	return filepath.Join(configuration.Service.LogDirectory, filename)
}

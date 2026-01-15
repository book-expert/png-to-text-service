/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */

package config

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/book-expert/logger"
)

// Config represents the full configuration structure for the service.
type Config struct {
	Service ServiceSettings
	LLM     LLMSettings
	NATS    NATSSettings
}

// ServiceSettings contains general service parameters.
type ServiceSettings struct {
	LogDir  string
	Workers int
}

// LLMSettings captures parameters for the Large Language Model provider.
type LLMSettings struct {
	APIKeyEnvironmentVariable string
	BaseURL                   string
	Model                     string
	MaxRetries                int
	TimeoutSeconds            int
	Temperature               float64
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
	Durable string
}

// Load retrieves the configuration from environment variables.
func Load(_ string, _ *logger.Logger) (*Config, error) {
	var configuration Config

	// Service Settings
	configuration.Service.LogDir = getEnv("PNG_TO_TEXT_LOG_DIR", "/home/niko/development/logs/tts-logs")
	configuration.Service.Workers = getEnvInt("PNG_TO_TEXT_WORKERS", 5)

	// LLM Settings
	configuration.LLM.APIKeyEnvironmentVariable = "GEMINI_API_KEY"
	configuration.LLM.BaseURL = getEnv("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com")
	configuration.LLM.Model = getEnv("PNG_TO_TEXT_LLM_MODEL", "gemini-2.5-flash")
	configuration.LLM.MaxRetries = getEnvInt("PNG_TO_TEXT_MAX_RETRIES", 3)
	configuration.LLM.TimeoutSeconds = getEnvInt("PNG_TO_TEXT_TIMEOUT_SECONDS", 90)
	configuration.LLM.Temperature = getEnvFloat("PNG_TO_TEXT_TEMPERATURE", 0.0)
	configuration.LLM.ExtractionPrompt = os.Getenv("PNG_TO_TEXT_EXTRACTION_PROMPT")
	configuration.LLM.SystemInstruction = os.Getenv("PNG_TO_TEXT_SYSTEM_INSTRUCTION")

	// NATS Settings
	configuration.NATS.URL = getEnv("NATS_ADDRESS", "nats://localhost:4222")
	configuration.NATS.DLQSubject = getEnv("PNG_TO_TEXT_DLQ_SUBJECT", "png.to.text.dlq")
	configuration.NATS.Consumer.Durable = getEnv("PNG_TO_TEXT_DURABLE_NAME", "png-to-text-consumer")

	return &configuration, nil
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return fallback
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return fallback
	}
	return value
}

func getEnvFloat(key string, fallback float64) float64 {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return fallback
	}
	return value
}

// GetAPIKey resolves the actual API key value from the configured environment variable.
func (configuration *Config) GetAPIKey() string {
	return os.Getenv(configuration.LLM.APIKeyEnvironmentVariable)
}

// GetLogFilePath constructs an absolute path for a log file.
func (configuration *Config) GetLogFilePath(filename string) string {
	return filepath.Join(configuration.Service.LogDir, filename)
}

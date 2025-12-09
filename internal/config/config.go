package config

import (
	"os"
	"path/filepath"

	"github.com/book-expert/logger"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Service struct {
		LogDir string `toml:"log_dir"`
	} `toml:"service"`

	LLM struct {
		APIKeyVariable string  `toml:"api_key_variable"`
		BaseURL        string  `toml:"base_url"`
		Model          string  `toml:"model"`
		MaxRetries     int     `toml:"max_retries"`
		TimeoutSeconds int     `toml:"timeout_seconds"`
		Temperature    float64 `toml:"temperature"`
		Prompts        struct {
			SystemInstruction string `toml:"system_instruction"`
			ExtractionPrompt  string `toml:"extraction_prompt"`
		} `toml:"prompts"`
	} `toml:"llm"`

	NATS struct {
		URL        string `toml:"url"`
		DLQSubject string `toml:"dlq_subject"`
		Consumer   struct {
			Stream  string `toml:"stream"`
			Subject string `toml:"subject"`
			Durable string `toml:"durable"`
		} `toml:"consumer"`
		Producer struct {
			Stream  string `toml:"stream"`
			Subject string `toml:"subject"`
		} `toml:"producer"`
		ObjectStore struct {
			PNGBucket  string `toml:"png_bucket"`
			TextBucket string `toml:"text_bucket"`
		} `toml:"object_store"`
	} `toml:"nats"`
}

func Load(path string, log *logger.Logger) (*Config, error) {
	if path == "" {
		path = "project.toml"
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			// We can't easily log here as we might not have a logger set up for this specific failure,
			// or it's just a read handle. But to satisfy lint:
			if log != nil {
				log.Warn("failed to close config file: %v", closeErr)
			}
		}
	}()

	var cfg Config
	decoder := toml.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) GetAPIKey() string {
	return os.Getenv(c.LLM.APIKeyVariable)
}

func (c *Config) GetLogFilePath(filename string) string {
	return filepath.Join(c.Service.LogDir, filename)
}
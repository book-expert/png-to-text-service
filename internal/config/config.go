/*
GOLDEN RULES & DEVELOPER MANIFESTO (THE NORTH STAR)
--------------------------------------------------------------------------------
"Work is love made visible. And if you cannot work with love but only with
distaste, it is better that you should leave your work and sit at the gate of
the temple and take alms of those who work with joy." — Kahlil Gibran

1.  LOVE AND CARE (Primary Driver)
    - This is a craft. Build with pride, honesty, and kindness.
    - If you put love in your work, you build something deserving of love.
    - Be helpful: Code is read more than written; optimize for the reader.

2.  WRITE WHAT YOU MEAN (Explicit > Implicit)
    - Use WHOLE WORDS: `RequestIdentifier` not `ReqID`.
    - No magic numbers: Move application settings to `project.toml`.
    - Secure by design: Keep API keys and secrets strictly in `.env`.
    - No ambiguity: If you assume something, document it.

3.  SIMPLE IS EFFICIENT (Minimal Viable Elegance)
    - Avoid over-engineering. Small interfaces, clear structs.
    - If a design requires a hack, stop. Redesign it with elegance.
    - Lean, Clean, Mean: Delete dead code immediately.

4.  NO BASELESS ASSUMPTIONS (Scientific Rigor)
    - Do not guess. Base decisions on documentation and proven patterns.
    - If you do not know, ask or verify.

5.  NON-BLOCKING & ROBUST
    - Never block the main goroutine. Use Context for cancellation.
    - Handle errors explicitly: Don't just return them, wrap them with context.

--------------------------------------------------------------------------------
EXAMPLES OF "LOVE AND CARE" IN THIS CONTEXT:
--------------------------------------------------------------------------------
(A) NAMING
    Indifferent:  func Gen(t string, v string)
    With Love:    func GenerateSoundscape(ctx context.Context, textPrompt string, voiceID string)
    *Why: The Agent reading this next year will know exactly what it does and that it is cancellable.*

(B) CONFIGURATION
    Indifferent:  const Timeout = 30 // Hardcoded
    With Love:    config.App.TimeoutSeconds // Loaded from project.toml
    *Why: Allows behavior tuning without recompiling or touching the codebase.*

(C) ERROR HANDLING
    Indifferent:  if err != nil { return err }
    With Love:    if err != nil { return fmt.Errorf("failed to initialize vox engine: %w", err) }
    *Why: Wrapping the error gives the user the 'trace of breadcrumbs' they need to fix it. That is kindness.*
--------------------------------------------------------------------------------
*/

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

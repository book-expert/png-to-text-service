// ./cmd/png-to-text-service/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/nnikolov3/logger"
	"github.com/nnikolov3/png-to-text-service/internal/augment"
	"github.com/nnikolov3/png-to-text-service/internal/ocr"
	"github.com/nnikolov3/png-to-text-service/internal/pipeline"
	"github.com/nnikolov3/png-to-text-service/internal/worker"
)

func main() {
	log := logger.New(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	cfg, err := loadConfig(log)
	if err != nil {
		log.Fatal("Failed to load configuration: %v", err)
	}

	// REMOVED: All MinIO and storage client initialization is gone.

	// Initialize OCR processor
	ocrProcessor := ocr.New(cfg.TesseractPath, cfg.TesseractLang, cfg.TesseractDPI)

	// Initialize Gemini processor for text augmentation
	geminiCfg := &augment.GeminiConfig{
		APIKey:           cfg.GeminiAPIKey,
		Models:           []string{"gemini-1.5-flash-latest"},
		Temperature:      0.7,
		TimeoutSeconds:   60,
		MaxRetries:       3,
		UsePromptBuilder: true,
	}
	geminiProcessor := augment.NewGeminiProcessor(geminiCfg, log)

	// Initialize the main processing Pipeline without the storage client.
	mainPipeline, err := pipeline.New(
		ocrProcessor,
		geminiProcessor,
		log,
		cfg.OutputDir,
		cfg.KeepTempFiles,
		10, // min text length
		&augment.AugmentationOptions{Type: "commentary"},
	)
	if err != nil {
		log.Fatal("Failed to initialize processing pipeline: %v", err)
	}

	// Initialize the NATS worker
	natsWorker, err := worker.New(
		cfg.NatsURL,
		"PNG_PROCESSING",
		"png.created",
		"png-text-workers",
		mainPipeline,
		log,
	)
	if err != nil {
		log.Fatal("Failed to initialize NATS worker: %v", err)
	}

	go func() {
		log.Info("Starting NATS worker...")
		if err := natsWorker.Run(ctx); err != nil {
			log.Error("NATS worker stopped with error: %v", err)
			cancel()
		}
	}()

	<-sigChan
	log.Info("Shutdown signal received, gracefully shutting down...")
	cancel()
	time.Sleep(2 * time.Second)
	log.Info("Shutdown complete.")
}

type appConfig struct {
	NatsURL       string
	GeminiAPIKey  string
	TesseractPath string
	TesseractLang string
	TesseractDPI  int
	OutputDir     string
	KeepTempFiles bool
}

func loadConfig(log *logger.Logger) (*appConfig, error) {
	dpi, err := strconv.Atoi(getEnv("TESSERACT_DPI", "300"))
	if err != nil {
		return nil, fmt.Errorf("invalid TESSERACT_DPI: %w", err)
	}

	keepFiles, err := strconv.ParseBool(getEnv("KEEP_TEMP_FILES", "false"))
	if err != nil {
		return nil, fmt.Errorf("invalid KEEP_TEMP_FILES: %w", err)
	}

	cfg := &appConfig{
		NatsURL:       getEnv("NATS_URL", "nats://127.0.0.1:4222"),
		GeminiAPIKey:  getEnvOrError("GEMINI_API_KEY"),
		TesseractPath: getEnv("TESSERACT_PATH", "tesseract"),
		TesseractLang: getEnv("TESSERACT_LANG", "eng"),
		TesseractDPI:  dpi,
		OutputDir:     getEnv("OUTPUT_DIR", "test_artifacts/text"),
		KeepTempFiles: keepFiles,
	}

	if cfg.GeminiAPIKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is required")
	}

	log.Info("Configuration loaded successfully")
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvOrError(key string) string {
	return getEnv(key, "")
}

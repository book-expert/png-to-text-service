// ./cmd/png-to-text-service/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/book-expert/logger"

	"github.com/book-expert/png-to-text-service/internal/augment"
	"github.com/book-expert/png-to-text-service/internal/config"
	"github.com/book-expert/png-to-text-service/internal/ocr"
	"github.com/book-expert/png-to-text-service/internal/pipeline"
	"github.com/book-expert/png-to-text-service/internal/worker"
)

func main() {
	log, err := logger.New(os.TempDir(), "png-to-text-service.log")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	cfg, err := config.Load("", log)
	if err != nil {
		log.Fatal("Failed to load configuration: %v", err)
	}

	// Initialize OCR processor
	ocrCfg := ocr.TesseractConfig{
		Language:       cfg.Tesseract.Language,
		OEM:            cfg.Tesseract.OEM,
		PSM:            cfg.Tesseract.PSM,
		DPI:            cfg.Tesseract.DPI,
		TimeoutSeconds: cfg.Tesseract.TimeoutSeconds,
	}
	ocrProcessor := ocr.NewProcessor(ocrCfg, log)

	// Initialize Gemini processor for text augmentation
	geminiAPIKey := cfg.GetAPIKey()
	if geminiAPIKey == "" {
		log.Fatal(
			"Failed to get Gemini API key. Ensure %s is set.",
			cfg.Gemini.APIKeyVariable,
		)
	}
	geminiCfg := &augment.GeminiConfig{
		APIKey:            geminiAPIKey,
		Models:            cfg.Gemini.Models,
		Temperature:       cfg.Gemini.Temperature,
		TimeoutSeconds:    cfg.Gemini.TimeoutSeconds,
		MaxRetries:        cfg.Gemini.MaxRetries,
		UsePromptBuilder:  cfg.Augmentation.UsePromptBuilder,
		TopK:              cfg.Gemini.TopK,
		TopP:              cfg.Gemini.TopP,
		MaxTokens:         cfg.Gemini.MaxTokens,
		RetryDelaySeconds: cfg.Gemini.RetryDelaySeconds,
	}
	geminiProcessor := augment.NewGeminiProcessor(geminiCfg, log)

	// Initialize the main processing Pipeline
	mainPipeline, err := pipeline.New(
		ocrProcessor,
		geminiProcessor,
		log,
		cfg.Paths.OutputDir,
		false, // keepTempFiles is a debug setting, not in project.toml
		10,    // minTextLength
		&augment.AugmentationOptions{
			Type:         augment.AugmentationType(cfg.Augmentation.Type),
			CustomPrompt: cfg.Augmentation.CustomPrompt,
		},
	)
	if err != nil {
		log.Fatal("Failed to initialize processing pipeline: %v", err)
	}

	// NATS URL is sourced from environment as it's infrastructure-specific
	natsURL := getEnv("NATS_URL", "nats://127.0.0.1:4222")

	// Initialize the NATS worker
	natsWorker, err := worker.New(
		natsURL,
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

// getEnv is a helper to read an environment variable with a fallback.
// Kept for infrastructure-specific settings like NATS_URL.
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

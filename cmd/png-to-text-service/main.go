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
	// A temporary logger for the bootstrap process
	log, err := logger.New(os.TempDir(), "png-to-text-bootstrap.log")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create bootstrap logger: %v\n", err)
		os.Exit(1)
	}

	// Load configuration using the central configurator
	cfg, err := config.Load("", log)
	if err != nil {
		log.Fatal("Failed to load configuration: %v", err)
	}

	// Initialize the final logger based on the loaded configuration
	log, err = logger.New(cfg.Paths.BaseLogsDir, "png-to-text-service.log")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create final logger: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize OCR processor from configuration
	ocrCfg := ocr.TesseractConfig{
		Language:       cfg.PNGToTextService.Tesseract.Language,
		OEM:            cfg.PNGToTextService.Tesseract.OEM,
		PSM:            cfg.PNGToTextService.Tesseract.PSM,
		DPI:            cfg.PNGToTextService.Tesseract.DPI,
		TimeoutSeconds: cfg.PNGToTextService.Tesseract.TimeoutSeconds,
	}
	ocrProcessor := ocr.NewProcessor(ocrCfg, log)

	// Initialize Gemini processor from configuration
	geminiAPIKey := cfg.GetAPIKey()
	if geminiAPIKey == "" {
		log.Fatal(
			"Failed to get Gemini API key. Ensure %s is set.",
			cfg.PNGToTextService.Gemini.APIKeyVariable,
		)
	}
	geminiCfg := &augment.GeminiConfig{
		APIKey:            geminiAPIKey,
		Models:            cfg.PNGToTextService.Gemini.Models,
		Temperature:       cfg.PNGToTextService.Gemini.Temperature,
		TimeoutSeconds:    cfg.PNGToTextService.Gemini.TimeoutSeconds,
		MaxRetries:        cfg.PNGToTextService.Gemini.MaxRetries,
		UsePromptBuilder:  cfg.PNGToTextService.Augmentation.UsePromptBuilder,
		TopK:              cfg.PNGToTextService.Gemini.TopK,
		TopP:              cfg.PNGToTextService.Gemini.TopP,
		MaxTokens:         cfg.PNGToTextService.Gemini.MaxTokens,
		RetryDelaySeconds: cfg.PNGToTextService.Gemini.RetryDelaySeconds,
	}
	geminiProcessor := augment.NewGeminiProcessor(geminiCfg, log)

	mainPipeline, err := pipeline.New(
		ocrProcessor,
		geminiProcessor,
		log,
		false, // keepTempFiles is a debug-only setting.
		10,    // minTextLength
		&augment.AugmentationOptions{
			Type: augment.AugmentationType(
				cfg.PNGToTextService.Augmentation.Type,
			),
			CustomPrompt: cfg.PNGToTextService.Augmentation.CustomPrompt,
		},
	)
	if err != nil {
		log.Fatal("Failed to initialize processing pipeline: %v", err)
	}

	// Initialize the NATS worker from configuration
	natsWorker, err := worker.New(
		cfg.NATS.URL,
		cfg.NATS.PNGStreamName,
		cfg.NATS.PNGCreatedSubject,
		cfg.NATS.PNGConsumerName,
		cfg.NATS.TextProcessedSubject,
		cfg.NATS.DeadLetterSubject,
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

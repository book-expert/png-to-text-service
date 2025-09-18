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

const (
	// MinTextLength defines the minimum length of text required for processing.
	MinTextLength = 10
	// ShutdownGracePeriodSeconds defines the duration to wait for graceful shutdown.
	ShutdownGracePeriodSeconds = 2
)

func setupLogger(logPath string) (*logger.Logger, error) {
	log, err := logger.New(logPath, "png-to-text-bootstrap.log")
	if err != nil {
		return nil, fmt.Errorf("failed to create bootstrap logger: %w", err)
	}

	return log, nil
}

func loadConfig(bootstrapLog *logger.Logger) (*config.Config, error) {
	cfg, err := config.Load("", bootstrapLog)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	return cfg, nil
}

func initOCRProcessor(cfg *config.Config, log *logger.Logger) *ocr.Processor {
	ocrCfg := ocr.TesseractConfig{
		Language:       cfg.PNGToTextService.Tesseract.Language,
		OEM:            cfg.PNGToTextService.Tesseract.OEM,
		PSM:            cfg.PNGToTextService.Tesseract.PSM,
		DPI:            cfg.PNGToTextService.Tesseract.DPI,
		TimeoutSeconds: cfg.PNGToTextService.Tesseract.TimeoutSeconds,
	}

	return ocr.NewProcessor(ocrCfg, log)
}

func initGeminiProcessor(cfg *config.Config, log *logger.Logger) *augment.GeminiProcessor {
	geminiAPIKey := cfg.GetAPIKey()
	if geminiAPIKey == "" {
		log.Fatal(
			"Failed to get Gemini API key. Ensure %s is set.",
			cfg.PNGToTextService.Gemini.APIKeyVariable,
		)
	}

	geminiCfg := &augment.GeminiConfig{
		APIKey:            geminiAPIKey,
		PromptTemplate:    "", // Added missing field
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

	return augment.NewGeminiProcessor(geminiCfg, log)
}

func initPipeline(
	ocrProcessor *ocr.Processor,
	geminiProcessor *augment.GeminiProcessor,
	cfg *config.Config,
	log *logger.Logger,
) (*pipeline.Pipeline, error) {
	mainPipeline, err := pipeline.New(
		ocrProcessor,
		geminiProcessor,
		log,
		false,         // keepTempFiles is a debug-only setting.
		MinTextLength, // minTextLength
		&augment.AugmentationOptions{
			Parameters: nil, // Added missing field
			Type: augment.AugmentationType(
				cfg.PNGToTextService.Augmentation.Type,
			),
			CustomPrompt: cfg.PNGToTextService.Augmentation.CustomPrompt,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize processing pipeline: %w", err)
	}

	return mainPipeline, nil
}

func initNATSWorker(
	cfg *config.Config,
	mainPipeline *pipeline.Pipeline,
	log *logger.Logger,
) (*worker.NatsWorker, error) {
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
		return nil, fmt.Errorf("failed to initialize NATS worker: %w", err)
	}

	return natsWorker, nil
}

func runWorker(ctx context.Context, natsWorker *worker.NatsWorker, log *logger.Logger) {
	go func() {
		log.Info("Starting NATS worker...")

		err := natsWorker.Run(ctx)
		if err != nil {
			log.Error("NATS worker stopped with error: %v", err)
			// No need to cancel context here, main will handle it.
		}
	}()
}

func run() error {
	// A temporary logger for the bootstrap process
	bootstrapLog, err := setupLogger(os.TempDir())
	if err != nil {
		return fmt.Errorf("failed to create bootstrap logger: %w", err)
	}

	// Load configuration using the central configurator
	cfg, err := loadConfig(bootstrapLog)
	if err != nil {
		bootstrapLog.Error("Failed to load configuration: %v", err)

		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize the final logger based on the loaded configuration
	log, err := setupLogger(cfg.Paths.BaseLogsDir)
	if err != nil {
		return fmt.Errorf("failed to create final logger: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize OCR processor from configuration
	ocrProcessor := initOCRProcessor(cfg, log)

	// Initialize Gemini processor from configuration
	geminiProcessor := initGeminiProcessor(cfg, log)

	mainPipeline, err := initPipeline(ocrProcessor, geminiProcessor, cfg, log)
	if err != nil {
		log.Error("Failed to initialize processing pipeline: %v", err)

		return fmt.Errorf("failed to initialize processing pipeline: %w", err)
	}

	// Initialize the NATS worker from configuration
	natsWorker, err := initNATSWorker(cfg, mainPipeline, log)
	if err != nil {
		log.Error("Failed to initialize NATS worker: %v", err)

		return fmt.Errorf("failed to initialize NATS worker: %w", err)
	}

	runWorker(ctx, natsWorker, log)

	<-sigChan
	log.Info("Shutdown signal received, gracefully shutting down...")
	time.Sleep(ShutdownGracePeriodSeconds * time.Second)
	log.Info("Shutdown complete.")

	return nil
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Service exited with error: %v\n", err)
		os.Exit(1)
	}
}

// ./cmd/png-to-text-service/main.go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/book-expert/logger"
	"github.com/nats-io/nats.go"

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
		PromptTemplate:    cfg.PNGToTextService.Prompts.Augmentation,
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
			Parameters: cfg.PNGToTextService.Augmentation.Parameters,
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

func setupJetStream(_ context.Context, jetstreamContext nats.JetStreamContext, cfg *config.Config) error {
	err := createStream(jetstreamContext, cfg.NATS.PNGStreamName, cfg.NATS.PNGCreatedSubject)
	if err != nil {
		return fmt.Errorf("failed to create PNG_PROCESSING stream: %w", err)
	}

	err = createStream(jetstreamContext, cfg.NATS.TextStreamName, cfg.NATS.TextProcessedSubject)
	if err != nil {
		return fmt.Errorf("failed to create TTS_JOBS stream: %w", err)
	}

	return nil
}

func createStream(jetstreamContext nats.JetStreamContext, streamName, subject string) error {
	streamCfg := &nats.StreamConfig{
		Name:                   streamName,
		Subjects:               []string{subject},
		Retention:              nats.WorkQueuePolicy,
		Description:            "",
		MaxConsumers:           -1,
		MaxMsgs:                -1,
		MaxBytes:               -1,
		Discard:                nats.DiscardOld,
		DiscardNewPerSubject:   false,
		MaxAge:                 0,
		MaxMsgsPerSubject:      -1,
		MaxMsgSize:             -1,
		Storage:                nats.FileStorage,
		Replicas:               1,
		NoAck:                  false,
		Duplicates:             0,
		Placement:              nil,
		Mirror:                 nil,
		Sources:                nil,
		Sealed:                 false,
		DenyDelete:             false,
		DenyPurge:              false,
		AllowRollup:            false,
		Compression:            nats.NoCompression,
		FirstSeq:               0,
		SubjectTransform:       nil,
		RePublish:              nil,
		AllowDirect:            false,
		MirrorDirect:           false,
		ConsumerLimits:         nats.StreamConsumerLimits{InactiveThreshold: 0, MaxAckPending: 0},
		Metadata:               nil,
		Template:               "",
		AllowMsgTTL:            false,
		SubjectDeleteMarkerTTL: 0,
	}

	_, err := jetstreamContext.AddStream(streamCfg)
	if err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		return fmt.Errorf("failed to create stream '%s': %w", streamName, err)
	}

	return nil
}

//nolint:ireturn // Returning nats.ObjectStore is idiomatic for NATS client usage.
func getPNGObjectStore(
	_ context.Context,
	jetstreamContext nats.JetStreamContext,
	cfg *config.Config,
) (nats.ObjectStore, error) {
	pngStore, err := jetstreamContext.ObjectStore(cfg.NATS.PNGObjectStoreBucket)
	if err != nil {
		return nil, fmt.Errorf("failed to bind to PNG object store: %w", err)
	}

	return pngStore, nil
}

//nolint:ireturn // Returning nats.ObjectStore is idiomatic for NATS client usage.
func getTextObjectStore(
	_ context.Context,
	jetstreamContext nats.JetStreamContext,
	cfg *config.Config,
) (nats.ObjectStore, error) {
	textStore, err := jetstreamContext.ObjectStore(cfg.NATS.TextObjectStoreBucket)
	if err != nil {
		return nil, fmt.Errorf("failed to bind to Text object store: %w", err)
	}

	return textStore, nil
}

//nolint:ireturn // Returning nats.JetStreamContext is idiomatic for NATS client usage.
func initNATSConnectionAndJetStream(cfg *config.Config) (*nats.Conn, nats.JetStreamContext, error) {
	natsConnection, err := nats.Connect(cfg.NATS.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	jetstreamContext, err := natsConnection.JetStream()
	if err != nil {
		natsConnection.Close()

		return nil, nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	return natsConnection, jetstreamContext, nil
}

func initNATSWorker(
	ctx context.Context,
	cfg *config.Config,
	mainPipeline *pipeline.Pipeline,
	log *logger.Logger,
	jetstreamContext nats.JetStreamContext,
) (*worker.NatsWorker, error) {
	// Get PNG object store
	pngStore, err := getPNGObjectStore(ctx, jetstreamContext, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get PNG object store: %w", err)
	}

	// Get Text object store
	textStore, err := getTextObjectStore(ctx, jetstreamContext, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get Text object store: %w", err)
	}

	natsWorker, err := worker.New(
		cfg.NATS.URL,
		cfg.NATS.PNGStreamName,
		cfg.NATS.PNGCreatedSubject,
		cfg.NATS.PNGConsumerName,
		cfg.NATS.TextProcessedSubject,
		cfg.NATS.DeadLetterSubject,
		mainPipeline,
		log,
		pngStore,
		textStore, // Pass the new textStore
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

	// Initialize NATS connection and JetStream context
	natsConnection, jetstreamContext, err := initNATSConnectionAndJetStream(cfg)
	if err != nil {
		return err
	}
	defer natsConnection.Close()

	// Setup JetStream streams
	err = setupJetStream(ctx, jetstreamContext, cfg)
	if err != nil {
		return fmt.Errorf("failed to setup JetStream streams: %w", err)
	}

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
	natsWorker, err := initNATSWorker(ctx, cfg, mainPipeline, log, jetstreamContext)
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

// ./cmd/png-to-text-service/main.go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/book-expert/configurator"
	"github.com/book-expert/logger"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/book-expert/png-to-text-service/internal/augment"
	"github.com/book-expert/png-to-text-service/internal/config"
	"github.com/book-expert/png-to-text-service/internal/ocr"
	"github.com/book-expert/png-to-text-service/internal/pipeline"
	"github.com/book-expert/png-to-text-service/internal/shared"
	"github.com/book-expert/png-to-text-service/internal/worker"
)

const (
	// MinTextLength defines the minimum length of text required for processing.
	MinTextLength = 10
	// ShutdownGracePeriodSeconds defines the duration to wait for graceful shutdown.
	ShutdownGracePeriodSeconds = 2
)

// Static errors to satisfy err113 (no dynamic error construction without wrapping) and
// to keep messages consistent and testable.
var (
	ErrObjectStoresCount = errors.New(
		"invalid configuration: expected at least 2 object stores (png and text)",
	)
	ErrConsumersCount = errors.New("invalid configuration: expected at least 1 consumer")
	ErrStreamsCount   = errors.New(
		"invalid configuration: expected at least 2 streams (input and output)",
	)
	ErrOutputStreamSubjects = errors.New(
		"invalid configuration: output stream must have at least 1 subject",
	)
	// ErrDeadLetterSubjectEmpty indicates the DLQ subject is missing from service config.
	ErrDeadLetterSubjectEmpty = errors.New("dead letter subject must be configured")
)

const (
	requiredObjectStoresCount   = 2
	requiredStreamsMinimumCount = 2
)

func cloneParameterMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}

	clone := make(map[string]any, len(src))
	for key, value := range src {
		clone[key] = value
	}

	return clone
}

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
		APIKey:               geminiAPIKey,
		CommentaryBasePrompt: cfg.PNGToTextService.Prompts.CommentaryBase,
		SummaryBasePrompt:    cfg.PNGToTextService.Prompts.SummaryBase,
		Models:               cfg.PNGToTextService.Gemini.Models,
		Temperature:          cfg.PNGToTextService.Gemini.Temperature,
		TimeoutSeconds:       cfg.PNGToTextService.Gemini.TimeoutSeconds,
		MaxRetries:           cfg.PNGToTextService.Gemini.MaxRetries,
		UsePromptBuilder:     cfg.PNGToTextService.Augmentation.UsePromptBuilder,
		TopK:                 cfg.PNGToTextService.Gemini.TopK,
		TopP:                 cfg.PNGToTextService.Gemini.TopP,
		MaxTokens:            cfg.PNGToTextService.Gemini.MaxTokens,
		RetryDelaySeconds:    cfg.PNGToTextService.Gemini.RetryDelaySeconds,
	}

	return augment.NewGeminiProcessor(geminiCfg, log)
}

func initPipeline(
	ocrProcessor *ocr.Processor,
	geminiProcessor *augment.GeminiProcessor,
	cfg *config.Config,
	log *logger.Logger,
) (*pipeline.Pipeline, error) {
	defaultOptions := &shared.AugmentationOptions{
		Parameters: cloneParameterMap(cfg.PNGToTextService.Augmentation.Parameters),
		Commentary: shared.AugmentationCommentaryOptions{
			Enabled: cfg.PNGToTextService.Augmentation.Defaults.Commentary.Enabled,
			CustomAdditions: strings.TrimSpace(
				cfg.PNGToTextService.Augmentation.Defaults.Commentary.CustomAdditions,
			),
		},
		Summary: shared.AugmentationSummaryOptions{
			Enabled:   cfg.PNGToTextService.Augmentation.Defaults.Summary.Enabled,
			Placement: cfg.PNGToTextService.Augmentation.Defaults.Summary.Placement,
			CustomAdditions: strings.TrimSpace(
				cfg.PNGToTextService.Augmentation.Defaults.Summary.CustomAdditions,
			),
		},
	}

	mainPipeline, err := pipeline.New(
		ocrProcessor,
		geminiProcessor,
		log,
		false,         // keepTempFiles is a debug-only setting.
		MinTextLength, // minTextLength
		defaultOptions,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize processing pipeline: %w", err)
	}

	return mainPipeline, nil
}

func setupJetStream(
	requestContext context.Context,
	jetstreamContext jetstream.JetStream,
	cfg *config.Config,
) error {
	createStreamsError := configurator.CreateOrUpdateStreams(
		requestContext,
		jetstreamContext,
		cfg.ServiceNATS.Streams,
	)
	if createStreamsError != nil {
		return fmt.Errorf("failed to create/update streams: %w", createStreamsError)
	}

	createConsumersError := configurator.CreateOrUpdateConsumers(
		requestContext,
		jetstreamContext,
		cfg.ServiceNATS.Consumers,
	)
	if createConsumersError != nil {
		return fmt.Errorf("failed to create/update consumers: %w", createConsumersError)
	}

	createStoresError := configurator.CreateOrUpdateObjectStores(
		requestContext,
		jetstreamContext,
		cfg.ServiceNATS.ObjectStores,
	)
	if createStoresError != nil {
		return fmt.Errorf("failed to create/update object stores: %w", createStoresError)
	}

	return nil
}

func ensureObjectStore(
	requestContext context.Context,
	jetstreamContext jetstream.JetStream,
	bucket string,
) error {
	storeConfig := jetstream.ObjectStoreConfig{
		Bucket:      bucket,
		Description: "",
		TTL:         0,
		MaxBytes:    -1,
		Storage:     jetstream.FileStorage,
		Replicas:    1,
		Placement:   nil,
		Compression: false,
		Metadata:    nil,
	}

	_, createObjectStoreError := jetstreamContext.CreateObjectStore(requestContext, storeConfig)
	if createObjectStoreError != nil {
		if errors.Is(createObjectStoreError, jetstream.ErrBucketExists) {
			_, lookupStoreError := jetstreamContext.ObjectStore(requestContext, bucket)
			if lookupStoreError != nil {
				return fmt.Errorf(
					"failed to access existing object store '%s': %w",
					bucket,
					lookupStoreError,
				)
			}

			return nil
		}

		return fmt.Errorf("failed to create object store '%s': %w", bucket, createObjectStoreError)
	}

	return nil
}

func initNATSWorker(
	requestContext context.Context,
	cfg *config.Config,
	mainPipeline *pipeline.Pipeline,
	log *logger.Logger,
	jetstreamContext jetstream.JetStream,
) (*worker.NatsWorker, error) {
	// Validate configuration shape before indexing
	validateConfigError := validateNATSConfigCounts(cfg)
	if validateConfigError != nil {
		return nil, validateConfigError
	}

	stores, resolveStoresError := resolveObjectStores(requestContext, jetstreamContext, cfg)
	if resolveStoresError != nil {
		return nil, resolveStoresError
	}

	pngStore := stores.png
	textStore := stores.text

	ttsDefaults := makeTTSDefaults(cfg)

	// Resolve the DLQ subject from service configuration (explicit per blueprint)
	dlqSubject := strings.TrimSpace(cfg.PNGToTextService.DeadLetterSubject)
	if dlqSubject == "" {
		return nil, ErrDeadLetterSubjectEmpty
	}

	natsWorker, newWorkerError := worker.New(
		requestContext,
		cfg.ServiceNATS.NATS.URL,
		cfg.ServiceNATS.Consumers[0].StreamName,
		cfg.ServiceNATS.Consumers[0].FilterSubject,
		cfg.ServiceNATS.Consumers[0].ConsumerName,
		cfg.ServiceNATS.Streams[1].Subjects[0],
		dlqSubject,
		mainPipeline,
		log,
		pngStore,
		textStore,
		ttsDefaults,
	)
	if newWorkerError != nil {
		return nil, fmt.Errorf("failed to initialize NATS worker: %w", newWorkerError)
	}

	return natsWorker, nil
}

// validateNATSConfigCounts ensures required counts are present for robust operation before indexing.
func validateNATSConfigCounts(cfg *config.Config) error {
	if len(cfg.ServiceNATS.ObjectStores) < requiredObjectStoresCount {
		return fmt.Errorf("%w: found %d", ErrObjectStoresCount, len(cfg.ServiceNATS.ObjectStores))
	}

	if len(cfg.ServiceNATS.Consumers) < 1 {
		return fmt.Errorf("%w: found %d", ErrConsumersCount, len(cfg.ServiceNATS.Consumers))
	}

	if len(cfg.ServiceNATS.Streams) < requiredStreamsMinimumCount {
		return fmt.Errorf("%w: found %d", ErrStreamsCount, len(cfg.ServiceNATS.Streams))
	}

	if len(cfg.ServiceNATS.Streams[1].Subjects) < 1 {
		return fmt.Errorf("%w", ErrOutputStreamSubjects)
	}

	return nil
}

// resolveObjectStores ensures both source and destination object stores exist and returns handles.
type resolvedStores struct {
	png  jetstream.ObjectStore
	text jetstream.ObjectStore
}

func resolveObjectStores(
	requestContext context.Context,
	jetstreamContext jetstream.JetStream,
	cfg *config.Config,
) (resolvedStores, error) {
	pngBucketName := cfg.ServiceNATS.ObjectStores[0].BucketName

	ensurePNGStoreError := ensureObjectStore(requestContext, jetstreamContext, pngBucketName)
	if ensurePNGStoreError != nil {
		return resolvedStores{}, fmt.Errorf(
			"failed to ensure PNG object store '%s': %w",
			pngBucketName,
			ensurePNGStoreError,
		)
	}

	pngStore, pngStoreLookupError := jetstreamContext.ObjectStore(requestContext, pngBucketName)
	if pngStoreLookupError != nil {
		return resolvedStores{}, fmt.Errorf(
			"failed to retrieve PNG object store '%s': %w",
			pngBucketName,
			pngStoreLookupError,
		)
	}

	textBucketName := cfg.ServiceNATS.ObjectStores[1].BucketName

	ensureTextStoreError := ensureObjectStore(requestContext, jetstreamContext, textBucketName)
	if ensureTextStoreError != nil {
		return resolvedStores{}, fmt.Errorf(
			"failed to ensure text object store '%s': %w",
			textBucketName,
			ensureTextStoreError,
		)
	}

	textStore, textStoreLookupError := jetstreamContext.ObjectStore(requestContext, textBucketName)
	if textStoreLookupError != nil {
		return resolvedStores{}, fmt.Errorf(
			"failed to retrieve text object store '%s': %w",
			textBucketName,
			textStoreLookupError,
		)
	}

	return resolvedStores{png: pngStore, text: textStore}, nil
}

// makeTTSDefaults constructs TTS default parameters from configuration.
func makeTTSDefaults(cfg *config.Config) worker.TTSDefaults {
	return worker.TTSDefaults{
		Voice:             cfg.PNGToTextService.TTSDefaults.Voice,
		Seed:              cfg.PNGToTextService.TTSDefaults.Seed,
		NGL:               cfg.PNGToTextService.TTSDefaults.NGL,
		TopP:              cfg.PNGToTextService.TTSDefaults.TopP,
		RepetitionPenalty: cfg.PNGToTextService.TTSDefaults.RepetitionPenalty,
		Temperature:       cfg.PNGToTextService.TTSDefaults.Temperature,
	}
}

// ensureDLQStream verifies that a stream covers the provided DLQ subject; if none does,
// it creates/updates a dedicated DLQ stream via configurator. No-op when subject is empty.
func ensureDLQStream(
	requestContext context.Context,
	jetstreamContext jetstream.JetStream,
	serviceNATS *configurator.ServiceNATSConfig,
	deadLetterSubject string,
) error {
	if deadLetterSubject == "" {
		return nil
	}

	for _, s := range serviceNATS.Streams {
		for _, subj := range s.Subjects {
			if subj == deadLetterSubject {
				return nil
			}
		}
	}

	dlq := configurator.StreamConfig{
		Name:     "dlq",
		Subjects: []string{deadLetterSubject},
	}

	dlqStreams := []configurator.StreamConfig{dlq}

	err := configurator.CreateOrUpdateStreams(requestContext, jetstreamContext, dlqStreams)
	if err != nil {
		return fmt.Errorf("ensure dlq stream: %w", err)
	}

	return nil
}

func runWorker(requestContext context.Context, natsWorker *worker.NatsWorker, log *logger.Logger) {
	go func() {
		log.Info("Starting NATS worker...")

		runError := natsWorker.Run(requestContext)
		if runError != nil {
			log.Error("NATS worker stopped with error: %v", runError)
			// No need to cancel context here, main will handle it.
		}
	}()
}

func initializeLoggingAndConfig() (*config.Config, *logger.Logger, error) {
	bootstrapLog, bootstrapErr := setupLogger(os.TempDir())
	if bootstrapErr != nil {
		return nil, nil, fmt.Errorf("failed to create bootstrap logger: %w", bootstrapErr)
	}

	cfg, loadErr := loadConfig(bootstrapLog)
	if loadErr != nil {
		bootstrapLog.Error("Failed to load configuration: %v", loadErr)

		closeLoggerQuietly(bootstrapLog)

		return nil, nil, fmt.Errorf("failed to load configuration: %w", loadErr)
	}

	serviceLog, serviceLogErr := setupLogger(cfg.Paths.BaseLogsDir)
	if serviceLogErr != nil {
		closeLoggerQuietly(bootstrapLog)

		return nil, nil, fmt.Errorf("failed to create final logger: %w", serviceLogErr)
	}

	closeLoggerQuietly(bootstrapLog)

	return cfg, serviceLog, nil
}

func initializePipeline(cfg *config.Config, serviceLog *logger.Logger) (*pipeline.Pipeline, error) {
	ocrProcessor := initOCRProcessor(cfg, serviceLog)
	geminiProcessor := initGeminiProcessor(cfg, serviceLog)

	mainPipeline, pipelineErr := initPipeline(ocrProcessor, geminiProcessor, cfg, serviceLog)
	if pipelineErr != nil {
		serviceLog.Error("Failed to initialize processing pipeline: %v", pipelineErr)

		return nil, fmt.Errorf("failed to initialize processing pipeline: %w", pipelineErr)
	}

	return mainPipeline, nil
}

func initializeWorker(
	requestContext context.Context,
	cfg *config.Config,
	mainPipeline *pipeline.Pipeline,
	serviceLog *logger.Logger,
	jetstreamContext jetstream.JetStream,
) (*worker.NatsWorker, error) {
	natsWorker, workerErr := initNATSWorker(
		requestContext,
		cfg,
		mainPipeline,
		serviceLog,
		jetstreamContext,
	)
	if workerErr != nil {
		serviceLog.Error("Failed to initialize NATS worker: %v", workerErr)

		return nil, fmt.Errorf("failed to initialize NATS worker: %w", workerErr)
	}

	return natsWorker, nil
}

func closeLoggerQuietly(logInstance *logger.Logger) {
	if logInstance == nil {
		return
	}

	closeErr := logInstance.Close()
	if closeErr != nil {
		fmt.Fprintf(os.Stderr, "failed to close logger: %v\n", closeErr)
	}
}

func run() error {
	cfg, serviceLog, initializationErr := initializeLoggingAndConfig()
	if initializationErr != nil {
		return initializationErr
	}

	requestContext, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()

	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)

	natsConnection, jetstreamContext, setupComponentsError := configurator.SetupNATSComponents(
		cfg.ServiceNATS.NATS,
	)
	if setupComponentsError != nil {
		return fmt.Errorf("setup NATS components: %w", setupComponentsError)
	}
	defer natsConnection.Close()

	setupJetStreamError := setupJetStream(requestContext, jetstreamContext, cfg)
	if setupJetStreamError != nil {
		return fmt.Errorf("failed to setup JetStream resources: %w", setupJetStreamError)
	}

	// Ensure a DLQ stream exists for the configured subject.
	dlqEnsureErr := ensureDLQStream(
		requestContext,
		jetstreamContext,
		&cfg.ServiceNATS,
		cfg.PNGToTextService.DeadLetterSubject,
	)
	if dlqEnsureErr != nil {
		return dlqEnsureErr
	}

	mainPipeline, initializePipelineError := initializePipeline(cfg, serviceLog)
	if initializePipelineError != nil {
		return initializePipelineError
	}

	natsWorker, initializeWorkerError := initializeWorker(
		requestContext,
		cfg,
		mainPipeline,
		serviceLog,
		jetstreamContext,
	)
	if initializeWorkerError != nil {
		return initializeWorkerError
	}

	runWorker(requestContext, natsWorker, serviceLog)

	<-signalChannel
	serviceLog.Info("Shutdown signal received, gracefully shutting down...")
	time.Sleep(ShutdownGracePeriodSeconds * time.Second)
	serviceLog.Info("Shutdown complete.")

	closeLoggerQuietly(serviceLog)

	return nil
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Service exited with error: %v\n", err)
		os.Exit(1)
	}
}

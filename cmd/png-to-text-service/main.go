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

	"github.com/book-expert/logger"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

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
	defaultOptions := &augment.AugmentationOptions{
		Parameters: cloneParameterMap(cfg.PNGToTextService.Augmentation.Parameters),
		Commentary: augment.AugmentationCommentaryOptions{
			Enabled:         cfg.PNGToTextService.Augmentation.Defaults.Commentary.Enabled,
			CustomAdditions: strings.TrimSpace(cfg.PNGToTextService.Augmentation.Defaults.Commentary.CustomAdditions),
		},
		Summary: augment.AugmentationSummaryOptions{
			Enabled:         cfg.PNGToTextService.Augmentation.Defaults.Summary.Enabled,
			Placement:       augment.SummaryPlacement(cfg.PNGToTextService.Augmentation.Defaults.Summary.Placement),
			CustomAdditions: strings.TrimSpace(cfg.PNGToTextService.Augmentation.Defaults.Summary.CustomAdditions),
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

func setupJetStream(_ context.Context, jetstreamContext nats.JetStreamContext, cfg *config.Config) error {
	err := createStream(jetstreamContext, cfg.NATS.PNGStreamName, cfg.NATS.PNGCreatedSubject)
	if err != nil {
		return fmt.Errorf("failed to create PNG_PROCESSING stream: %w", err)
	}

	err = createStream(jetstreamContext, cfg.NATS.TextStreamName, cfg.NATS.TextProcessedSubject)
	if err != nil {
		return fmt.Errorf("failed to create TTS_JOBS stream: %w", err)
	}

	if cfg.NATS.DeadLetterSubject != "" {
		dlqName := streamNameForSubject(cfg.NATS.DeadLetterSubject)

		err = createStream(jetstreamContext, dlqName, cfg.NATS.DeadLetterSubject)
		if err != nil {
			return fmt.Errorf("failed to create dead letter stream: %w", err)
		}
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

type objectStoreManager interface {
	CreateObjectStore(cfg *nats.ObjectStoreConfig) (nats.ObjectStore, error)
	ObjectStore(bucket string) (nats.ObjectStore, error)
}

type jetStreamResources struct {
	Connection *nats.Conn
	Context    nats.JetStreamContext
}

func (resources *jetStreamResources) Close() {
	if resources == nil {
		return
	}

	if resources.Connection != nil {
		resources.Connection.Close()
	}
}

func createJetStreamResources(cfg *config.Config) (*jetStreamResources, error) {
	natsConnection, connectErr := nats.Connect(cfg.NATS.URL)
	if connectErr != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", connectErr)
	}

	jetstreamContext, jetStreamErr := natsConnection.JetStream()
	if jetStreamErr != nil {
		natsConnection.Close()

		return nil, fmt.Errorf("failed to get JetStream context: %w", jetStreamErr)
	}

	return &jetStreamResources{
		Connection: natsConnection,
		Context:    jetstreamContext,
	}, nil
}

func ensureObjectStore(storeManager objectStoreManager, bucket string) error {
	storeConfig := new(nats.ObjectStoreConfig)
	storeConfig.Bucket = bucket

	_, createErr := storeManager.CreateObjectStore(storeConfig)
	if createErr != nil {
		if errors.Is(createErr, jetstream.ErrBucketExists) {
			_, lookupErr := storeManager.ObjectStore(bucket)
			if lookupErr != nil {
				return fmt.Errorf("failed to access existing object store '%s': %w", bucket, lookupErr)
			}

			return nil
		}

		return fmt.Errorf("failed to create object store '%s': %w", bucket, createErr)
	}

	return nil
}

func streamNameForSubject(subject string) string {
	replacer := strings.NewReplacer(
		".", "_",
		"*", "STAR",
		">", "GT",
	)

	return strings.ToUpper(replacer.Replace(subject))
}

func initNATSWorker(
	cfg *config.Config,
	mainPipeline *pipeline.Pipeline,
	log *logger.Logger,
	jetstreamContext nats.JetStreamContext,
) (*worker.NatsWorker, error) {
	pngBucket := cfg.NATS.PNGObjectStoreBucket

	ensurePNGStoreErr := ensureObjectStore(jetstreamContext, pngBucket)
	if ensurePNGStoreErr != nil {
		return nil, fmt.Errorf("failed to ensure PNG object store '%s': %w", pngBucket, ensurePNGStoreErr)
	}

	pngStore, pngStoreErr := jetstreamContext.ObjectStore(pngBucket)
	if pngStoreErr != nil {
		return nil, fmt.Errorf("failed to retrieve PNG object store '%s': %w", pngBucket, pngStoreErr)
	}

	textBucket := cfg.NATS.TextObjectStoreBucket

	ensureTextStoreErr := ensureObjectStore(jetstreamContext, textBucket)
	if ensureTextStoreErr != nil {
		return nil, fmt.Errorf("failed to ensure text object store '%s': %w", textBucket, ensureTextStoreErr)
	}

	textStore, textStoreErr := jetstreamContext.ObjectStore(textBucket)
	if textStoreErr != nil {
		return nil, fmt.Errorf("failed to retrieve text object store '%s': %w", textBucket, textStoreErr)
	}

	ttsDefaults := worker.TTSDefaults{
		Voice:             cfg.PNGToTextService.TTSDefaults.Voice,
		Seed:              cfg.PNGToTextService.TTSDefaults.Seed,
		NGL:               cfg.PNGToTextService.TTSDefaults.NGL,
		TopP:              cfg.PNGToTextService.TTSDefaults.TopP,
		RepetitionPenalty: cfg.PNGToTextService.TTSDefaults.RepetitionPenalty,
		Temperature:       cfg.PNGToTextService.TTSDefaults.Temperature,
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
		ttsDefaults,
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
	cfg *config.Config,
	mainPipeline *pipeline.Pipeline,
	serviceLog *logger.Logger,
	jetstreamContext nats.JetStreamContext,
) (*worker.NatsWorker, error) {
	natsWorker, workerErr := initNATSWorker(cfg, mainPipeline, serviceLog, jetstreamContext)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)

	jetstreamResources, jetStreamErr := createJetStreamResources(cfg)
	if jetStreamErr != nil {
		return jetStreamErr
	}
	defer jetstreamResources.Close()

	setupErr := setupJetStream(ctx, jetstreamResources.Context, cfg)
	if setupErr != nil {
		return fmt.Errorf("failed to setup JetStream streams: %w", setupErr)
	}

	mainPipeline, pipelineErr := initializePipeline(cfg, serviceLog)
	if pipelineErr != nil {
		return pipelineErr
	}

	natsWorker, workerErr := initializeWorker(cfg, mainPipeline, serviceLog, jetstreamResources.Context)
	if workerErr != nil {
		return workerErr
	}

	runWorker(ctx, natsWorker, serviceLog)

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

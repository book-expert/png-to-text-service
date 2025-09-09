// ./cmd/png-to-text-service/main.go
// PNG-to-text-service: A NATS-driven worker for OCR and AI Augmentation.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nnikolov3/logger"

	"github.com/nnikolov3/png-to-text-service/internal/config"
	"github.com/nnikolov3/png-to-text-service/internal/pipeline"
)

const (
	// appVersion holds the semantic version of the service.
	appVersion = "1.0.0"
	// jetStreamFetchTimeoutSeconds defines the timeout for fetching JetStream
	// messages.
	jetStreamFetchTimeoutSeconds = 5
	// jetStreamStreamName defines the name of the JetStream stream for PNG
	// processing.
	jetStreamStreamName = "PNG_PROCESSING"
	// jetStreamConsumerName defines the name of the consumer group for PNG
	// processing.
	jetStreamConsumerName = "png-text-workers"
	// jetStreamSubject defines the subject pattern for PNG processing jobs.
	jetStreamSubject = "png.created"
	// jetStreamMaxRetries defines the maximum number of redelivery attempts.
	jetStreamMaxRetries = 3
	// jetStreamAckWaitMinutes defines the maximum time to wait for message
	// acknowledgment.
	jetStreamAckWaitMinutes = 5
	// jetStreamMaxAckPending defines the maximum number of unacknowledged messages.
	jetStreamMaxAckPending = 10
	// jetStreamMaxAgeHours defines the maximum age of messages in the stream.
	jetStreamMaxAgeHours = 24
)

// Package-level static errors for clear, checkable error conditions.
var (
	ErrPngPathEmpty      = errors.New("pngPathInStorage cannot be empty")
	ErrJobIDEmpty        = errors.New("jobId cannot be empty")
	ErrInputPngPathEmpty = errors.New("input png path cannot be empty")
)

// PngCreatedJob represents the incoming job message from the 'png.created' topic.
type PngCreatedJob struct {
	PngPathInStorage string `json:"pngPathInStorage"`
	JobID            string `json:"jobId"`
}

// OcrCompletedJob represents the outgoing job message for the 'ocr.completed' topic.
type OcrCompletedJob struct {
	TextPathInStorage string `json:"textPathInStorage"`
	OriginalPngPath   string `json:"originalPngPath"`
	JobID             string `json:"jobId"`
}

// NatsConnection defines the interface for NATS connection operations needed by
// JobHandler.
// This allows for mocking in tests.
type NatsConnection interface {
	Publish(subj string, data []byte) error
}

// JetStreamMessage defines the interface for JetStream message operations.
// This allows for acknowledgment-based message processing.
type JetStreamMessage interface {
	Data() []byte
	Ack() error
	Nak() error
}

// JetStreamConsumer defines the interface for JetStream consumer operations.
// This allows for reliable message consumption with acknowledgments.
type JetStreamConsumer interface {
	FetchMessage() (*jetStreamMessageWrapper, error)
}

// JetStreamManager defines the interface for JetStream resource management.
// This allows for creating streams and consumers for reliable messaging.
type JetStreamManager interface {
	CreateStream(name string, subjects []string) error
	CreateConsumer(streamName, consumerName string) error
}

// jetStreamMessageWrapper wraps a NATS JetStream message to implement JetStreamMessage
// interface.
type jetStreamMessageWrapper struct {
	msg *nats.Msg
}

func (w *jetStreamMessageWrapper) Data() []byte {
	return w.msg.Data
}

func (w *jetStreamMessageWrapper) Ack() error {
	ackErr := w.msg.Ack()
	if ackErr != nil {
		return fmt.Errorf("failed to acknowledge message: %w", ackErr)
	}

	return nil
}

func (w *jetStreamMessageWrapper) Nak() error {
	nakErr := w.msg.Nak()
	if nakErr != nil {
		return fmt.Errorf("failed to nack message: %w", nakErr)
	}

	return nil
}

// jetStreamConsumerWrapper wraps a NATS JetStream consumer to implement JetStreamConsumer
// interface.
type jetStreamConsumerWrapper struct {
	subscription *nats.Subscription
}

func (w *jetStreamConsumerWrapper) FetchMessage() (*jetStreamMessageWrapper, error) {
	msg, fetchErr := w.subscription.NextMsg(
		jetStreamFetchTimeoutSeconds * time.Second,
	)
	if fetchErr != nil {
		return nil, fmt.Errorf("failed to fetch message: %w", fetchErr)
	}

	return &jetStreamMessageWrapper{msg: msg}, nil
}

// jetStreamManagerWrapper wraps a NATS JetStream context to implement JetStreamManager
// interface.
type jetStreamManagerWrapper struct {
	js nats.JetStreamContext
}

func (w *jetStreamManagerWrapper) CreateStream(name string, subjects []string) error {
	_, streamErr := w.js.AddStream(&nats.StreamConfig{
		Name:                 name,
		Subjects:             subjects,
		Retention:            nats.WorkQueuePolicy,
		Storage:              nats.FileStorage,
		MaxAge:               jetStreamMaxAgeHours * time.Hour,
		Description:          "",
		MaxConsumers:         0,
		MaxMsgs:              0,
		MaxBytes:             0,
		Discard:              nats.DiscardOld,
		DiscardNewPerSubject: false,
		MaxMsgsPerSubject:    0,
		MaxMsgSize:           0,
		Replicas:             0,
		NoAck:                false,
		Duplicates:           0,
		Placement:            nil,
		Mirror:               nil,
		Sources:              nil,
		Sealed:               false,
		DenyDelete:           false,
		DenyPurge:            false,
		AllowRollup:          false,
		Compression:          nats.NoCompression,
		FirstSeq:             0,
		SubjectTransform:     nil,
		RePublish:            nil,
		AllowDirect:          false,
		MirrorDirect:         false,
		ConsumerLimits: nats.StreamConsumerLimits{
			InactiveThreshold: 0,
			MaxAckPending:     0,
		},
		Metadata:               nil,
		Template:               "",
		AllowMsgTTL:            false,
		SubjectDeleteMarkerTTL: 0,
	})
	if streamErr != nil {
		return fmt.Errorf("failed to add stream: %w", streamErr)
	}

	return nil
}

func (w *jetStreamManagerWrapper) CreateConsumer(streamName, consumerName string) error {
	_, consumerErr := w.js.AddConsumer(streamName, &nats.ConsumerConfig{
		Name:               consumerName,
		Durable:            "",
		Description:        "",
		DeliverPolicy:      nats.DeliverAllPolicy,
		OptStartSeq:        0,
		OptStartTime:       nil,
		AckPolicy:          nats.AckExplicitPolicy,
		AckWait:            jetStreamAckWaitMinutes * time.Minute,
		MaxDeliver:         jetStreamMaxRetries,
		BackOff:            nil,
		FilterSubject:      "",
		FilterSubjects:     nil,
		ReplayPolicy:       nats.ReplayInstantPolicy,
		RateLimit:          0,
		SampleFrequency:    "",
		MaxWaiting:         0,
		MaxAckPending:      jetStreamMaxAckPending,
		FlowControl:        false,
		Heartbeat:          0,
		HeadersOnly:        false,
		MaxRequestBatch:    0,
		MaxRequestExpires:  0,
		MaxRequestMaxBytes: 0,
		DeliverSubject:     "",
		DeliverGroup:       "",
		InactiveThreshold:  0,
		Replicas:           0,
		MemoryStorage:      false,
		Metadata:           nil,
	})
	if consumerErr != nil {
		return fmt.Errorf("failed to add consumer: %w", consumerErr)
	}

	return nil
}

// PipelineProcessor defines the interface for the processing pipeline.
// This allows for mocking in tests.
type PipelineProcessor interface {
	ProcessSingle(ctx context.Context, inputPath, outputPath string) error
}

// JobHandler orchestrates the processing of a single NATS message.
type JobHandler struct {
	conn      NatsConnection
	pipeline  PipelineProcessor
	logger    *logger.Logger
	appConfig *config.Config
}

// NewJobHandler creates a new handler with its required dependencies.
func NewJobHandler(
	conn NatsConnection,
	proc PipelineProcessor,
	loggerInstance *logger.Logger,
	cfg *config.Config,
) *JobHandler {
	return &JobHandler{
		conn:      conn,
		pipeline:  proc,
		logger:    loggerInstance,
		appConfig: cfg,
	}
}

// Handle is the callback for the NATS subscription, processing a single job.
func (h *JobHandler) Handle(msg *nats.Msg) {
	job, unmarshalErr := parseJobMessage(msg.Data)
	if unmarshalErr != nil {
		h.logger.Error("Failed to unmarshal job JSON: %v", unmarshalErr)

		return
	}

	h.logger.Info("Received job %s for PNG %s", job.JobID, job.PngPathInStorage)

	outputPath, pathErr := generateOutputPath(
		h.appConfig.Paths.OutputDir,
		job.PngPathInStorage,
	)
	if pathErr != nil {
		h.logger.Error(
			"Failed to generate output path for job %s: %v",
			job.JobID,
			pathErr,
		)

		return
	}

	procErr := h.runPipelineProcessing(job.PngPathInStorage, outputPath)
	if procErr != nil {
		h.logger.Error(
			"Failed to process PNG %s for job %s: %v",
			job.PngPathInStorage,
			job.JobID,
			procErr,
		)

		return
	}

	pubErr := h.publishCompletionEvent(job, outputPath)
	if pubErr != nil {
		h.logger.Error(
			"Failed to publish completion event for job %s: %v",
			job.JobID,
			pubErr,
		)

		return
	}

	h.logger.Success(
		"Successfully processed and published result for job %s",
		job.JobID,
	)
}

// ProcessJetStreamMessage processes a single JetStream message with acknowledgments.
// This method replaces the callback-based Handle method for JetStream integration.
func (h *JobHandler) ProcessJetStreamMessage(msg JetStreamMessage) error {
	job, parseErr := h.parseAndValidateMessage(msg)
	if parseErr != nil {
		return parseErr
	}

	h.logger.Info("Received job %s for PNG %s", job.JobID, job.PngPathInStorage)

	outputPath, pathErr := h.generateAndValidateOutputPath(msg, job)
	if pathErr != nil {
		return pathErr
	}

	processErr := h.processJobWithRetry(msg, job, outputPath)
	if processErr != nil {
		return processErr
	}

	return h.publishAndAcknowledge(msg, job, outputPath)
}

// parseAndValidateMessage parses and validates the JetStream message.
func (h *JobHandler) parseAndValidateMessage(
	msg JetStreamMessage,
) (PngCreatedJob, error) {
	job, parseErr := parseJobMessage(msg.Data())
	if parseErr != nil {
		h.logger.Error("Failed to unmarshal job JSON: %v", parseErr)
		h.acknowledgeMessage(msg, "malformed")

		return PngCreatedJob{}, fmt.Errorf("message parsing failed: %w", parseErr)
	}

	return job, nil
}

// generateAndValidateOutputPath generates and validates the output path.
func (h *JobHandler) generateAndValidateOutputPath(
	msg JetStreamMessage,
	job PngCreatedJob,
) (string, error) {
	outputPath, pathErr := generateOutputPath(
		h.appConfig.Paths.OutputDir,
		job.PngPathInStorage,
	)
	if pathErr != nil {
		h.logger.Error(
			"Failed to generate output path for job %s: %v",
			job.JobID,
			pathErr,
		)
		h.acknowledgeMessage(msg, "path error")

		return "", fmt.Errorf("output path generation failed: %w", pathErr)
	}

	return outputPath, nil
}

// processJobWithRetry processes the job and handles retries.
func (h *JobHandler) processJobWithRetry(
	msg JetStreamMessage,
	job PngCreatedJob,
	outputPath string,
) error {
	procErr := h.runPipelineProcessing(job.PngPathInStorage, outputPath)
	if procErr != nil {
		h.logger.Error(
			"Failed to process PNG %s for job %s: %v",
			job.PngPathInStorage,
			job.JobID,
			procErr,
		)
		h.nackMessage(msg, "processing error")

		return fmt.Errorf("pipeline processing failed: %w", procErr)
	}

	return nil
}

// publishAndAcknowledge publishes the completion event and acknowledges the message.
func (h *JobHandler) publishAndAcknowledge(
	msg JetStreamMessage,
	job PngCreatedJob,
	outputPath string,
) error {
	pubErr := h.publishCompletionEvent(job, outputPath)
	if pubErr != nil {
		h.logger.Error(
			"Failed to publish completion event for job %s: %v",
			job.JobID,
			pubErr,
		)
		h.nackMessage(msg, "publish error")

		return fmt.Errorf("completion event publishing failed: %w", pubErr)
	}

	ackErr := msg.Ack()
	if ackErr != nil {
		h.logger.Error("Failed to acknowledge successful message: %v", ackErr)

		return fmt.Errorf("message acknowledgment failed: %w", ackErr)
	}

	h.logger.Success(
		"Successfully processed and published result for job %s",
		job.JobID,
	)

	return nil
}

// acknowledgeMessage acknowledges a message and logs any errors.
func (h *JobHandler) acknowledgeMessage(msg JetStreamMessage, errorContext string) {
	ackErr := msg.Ack()
	if ackErr != nil {
		h.logger.Error(
			"Failed to acknowledge message after %s: %v",
			errorContext,
			ackErr,
		)
	}
}

// nackMessage nacks a message and logs any errors.
func (h *JobHandler) nackMessage(msg JetStreamMessage, errorContext string) {
	nakErr := msg.Nak()
	if nakErr != nil {
		h.logger.Error(
			"Failed to nack message after %s: %v",
			errorContext,
			nakErr,
		)
	}
}

// runPipelineProcessing executes the core OCR and augmentation logic with a timeout.
func (h *JobHandler) runPipelineProcessing(pngPath, outputPath string) error {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(h.appConfig.Settings.TimeoutSeconds)*time.Second,
	)
	defer cancel()

	processErr := h.pipeline.ProcessSingle(ctx, pngPath, outputPath)
	if processErr != nil {
		return fmt.Errorf("pipeline.ProcessSingle failed: %w", processErr)
	}

	return nil
}

// publishCompletionEvent sends a message to NATS indicating the job is finished.
func (h *JobHandler) publishCompletionEvent(job PngCreatedJob, outputPath string) error {
	completionEvent := OcrCompletedJob{
		TextPathInStorage: outputPath,
		OriginalPngPath:   job.PngPathInStorage,
		JobID:             job.JobID,
	}

	payload, marshErr := json.Marshal(completionEvent)
	if marshErr != nil {
		return fmt.Errorf("failed to marshal completion event: %w", marshErr)
	}

	pubErr := h.conn.Publish("ocr.completed", payload)
	if pubErr != nil {
		return fmt.Errorf("failed to publish completion event: %w", pubErr)
	}

	return nil
}

// parseJobMessage decodes the incoming NATS message data into a PngCreatedJob struct.
func parseJobMessage(data []byte) (PngCreatedJob, error) {
	var job PngCreatedJob

	unmarshalErr := json.Unmarshal(data, &job)
	if unmarshalErr != nil {
		return PngCreatedJob{}, fmt.Errorf("json.Unmarshal: %w", unmarshalErr)
	}

	if job.PngPathInStorage == "" {
		return PngCreatedJob{}, ErrPngPathEmpty
	}

	if job.JobID == "" {
		return PngCreatedJob{}, ErrJobIDEmpty
	}

	return job, nil
}

// generateOutputPath creates the destination path for the processed text file.
func generateOutputPath(outputDir, pngPath string) (string, error) {
	if pngPath == "" {
		return "", ErrInputPngPathEmpty
	}

	baseName := filepath.Base(pngPath)
	txtName := strings.TrimSuffix(baseName, filepath.Ext(baseName)) + ".txt"

	return filepath.Join(outputDir, txtName), nil
}

// configureJetStreamResources creates the necessary JetStream streams and consumers.
func configureJetStreamResources(jsManager JetStreamManager) error {
	// Create the PNG processing stream
	streamErr := jsManager.CreateStream(
		jetStreamStreamName,
		[]string{jetStreamSubject},
	)
	if streamErr != nil {
		return fmt.Errorf(
			"failed to create stream %s: %w",
			jetStreamStreamName,
			streamErr,
		)
	}

	// Create the consumer for PNG processing
	consumerErr := jsManager.CreateConsumer(
		jetStreamStreamName,
		jetStreamConsumerName,
	)
	if consumerErr != nil {
		return fmt.Errorf(
			"failed to create consumer %s: %w",
			jetStreamConsumerName,
			consumerErr,
		)
	}

	return nil
}

// --- Application Entrypoint ---

// main is the entrypoint for the service.
func main() {
	err := run()
	if err != nil {
		log.Fatalf("Fatal application error: %v", err)
	}
}

// run contains the main application logic to ensure deferred calls are executed
// correctly.
func run() error {
	appConfig, configErr := loadConfiguration()
	if configErr != nil {
		// Use standard logger because custom logger isn't initialized yet.
		log.Fatalf("Failed to load configuration: %v", configErr)
	}

	loggerInstance, loggerErr := initializeLogger(appConfig)
	if loggerErr != nil {
		log.Fatalf("Failed to initialize logger: %v", loggerErr)
	}

	defer func() {
		closeErr := loggerInstance.Close()
		if closeErr != nil {
			log.Printf("Error closing logger: %v", closeErr)
		}
	}()

	loggerInstance.Info("PNG-to-text-service worker v%s starting up...", appVersion)

	natsConnection, connErr := nats.Connect(nats.DefaultURL)
	if connErr != nil {
		return fmt.Errorf("failed to connect to NATS: %w", connErr)
	}
	defer natsConnection.Close()

	loggerInstance.Info(
		"Connected to NATS server at %s",
		natsConnection.ConnectedUrl(),
	)

	return subscribeAndRunJetStream(natsConnection, appConfig, loggerInstance)
}

// subscribeAndRunJetStream sets up JetStream subscription and waits for a shutdown
// signal.
func subscribeAndRunJetStream(
	natsConnection *nats.Conn,
	appConfig *config.Config,
	loggerInstance *logger.Logger,
) error {
	// Create JetStream context
	jetStreamContext, jsErr := natsConnection.JetStream()
	if jsErr != nil {
		return fmt.Errorf("failed to create JetStream context: %w", jsErr)
	}

	// Configure JetStream resources
	jsManager := &jetStreamManagerWrapper{js: jetStreamContext}

	configErr := configureJetStreamResources(jsManager)
	if configErr != nil {
		return fmt.Errorf(
			"failed to configure JetStream resources: %w",
			configErr,
		)
	}

	loggerInstance.Info("JetStream resources configured successfully")

	// Create processing pipeline
	processingPipeline, pipeErr := pipeline.NewPipeline(appConfig, loggerInstance)
	if pipeErr != nil {
		return fmt.Errorf("failed to create pipeline: %w", pipeErr)
	}

	// Create job handler
	handler := NewJobHandler(
		natsConnection,
		processingPipeline,
		loggerInstance,
		appConfig,
	)

	// Create JetStream pull subscription
	subscription, subErr := jetStreamContext.PullSubscribe(
		jetStreamSubject,
		jetStreamConsumerName,
	)
	if subErr != nil {
		return fmt.Errorf("failed to create JetStream subscription: %w", subErr)
	}

	loggerInstance.Info(
		"Worker is running with JetStream, subscribed to '%s' with consumer '%s'...",
		jetStreamSubject,
		jetStreamConsumerName,
	)

	// Create consumer wrapper
	consumer := &jetStreamConsumerWrapper{subscription: subscription}

	// Start processing messages
	return processJetStreamMessages(consumer, handler, loggerInstance)
}

// processJetStreamMessages continuously processes messages from JetStream.
func processJetStreamMessages(
	consumer JetStreamConsumer,
	handler *JobHandler,
	loggerInstance *logger.Logger,
) error {
	quitSignal := make(chan os.Signal, 1)
	signal.Notify(quitSignal, syscall.SIGINT, syscall.SIGTERM)

	for {
		if shouldStop(quitSignal) {
			loggerInstance.Info(
				"Shutdown signal received, stopping message processing...",
			)

			return nil
		}

		processSingleMessage(consumer, handler, loggerInstance)
	}
}

// shouldStop checks if a shutdown signal has been received.
func shouldStop(quitSignal <-chan os.Signal) bool {
	select {
	case <-quitSignal:
		return true
	default:
		return false
	}
}

// processSingleMessage fetches and processes a single JetStream message.
func processSingleMessage(
	consumer JetStreamConsumer,
	handler *JobHandler,
	loggerInstance *logger.Logger,
) {
	msg, fetchErr := consumer.FetchMessage()
	if fetchErr != nil {
		handleErr := handleFetchError(fetchErr, loggerInstance)
		if handleErr != nil {
			loggerInstance.Error("Critical fetch error: %v", handleErr)
		}

		return
	}

	processErr := handler.ProcessJetStreamMessage(msg)
	if processErr != nil {
		loggerInstance.Error(
			"Failed to process JetStream message: %v",
			processErr,
		)
	}
}

// handleFetchError processes fetch errors and determines if processing should continue.
func handleFetchError(fetchErr error, loggerInstance *logger.Logger) error {
	if errors.Is(fetchErr, nats.ErrTimeout) {
		return nil // Normal timeout, continue processing
	}

	loggerInstance.Error("Failed to fetch message: %v", fetchErr)

	return fetchErr
}

// --- Configuration and Logger Initialization ---

// loadConfiguration finds and loads the `project.toml` file.
func loadConfiguration() (*config.Config, error) {
	workingDir, wdErr := os.Getwd()
	if wdErr != nil {
		return nil, fmt.Errorf("could not get working directory: %w", wdErr)
	}

	_, configPath, findErr := config.FindProjectRoot(workingDir)
	if findErr != nil {
		return nil, fmt.Errorf(
			"could not find project configuration: %w",
			findErr,
		)
	}

	appConfig, loadErr := config.Load(configPath)
	if loadErr != nil {
		return nil, fmt.Errorf(
			"could not load configuration from %s: %w",
			configPath,
			loadErr,
		)
	}

	return appConfig, nil
}

// initializeLogger creates and configures the logger instance based on the application
// config.
func initializeLogger(appConfig *config.Config) (*logger.Logger, error) {
	logFileName := fmt.Sprintf(
		"png-to-text-service_%s.log",
		time.Now().Format("2006-01-02_15-04-05"),
	)

	loggerInstance, newErr := logger.New(appConfig.Logging.Dir, logFileName)
	if newErr != nil {
		return nil, fmt.Errorf("could not create logger: %w", newErr)
	}

	return loggerInstance, nil
}

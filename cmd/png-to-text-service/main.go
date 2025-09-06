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

	return subscribeAndRun(natsConnection, appConfig, loggerInstance)
}

// subscribeAndRun sets up the NATS subscription and waits for a shutdown signal.
func subscribeAndRun(
	natsConnection *nats.Conn,
	appConfig *config.Config,
	loggerInstance *logger.Logger,
) error {
	processingPipeline, pipeErr := pipeline.NewPipeline(appConfig, loggerInstance)
	if pipeErr != nil {
		return fmt.Errorf("failed to create pipeline: %w", pipeErr)
	}

	handler := NewJobHandler(
		natsConnection,
		processingPipeline,
		loggerInstance,
		appConfig,
	)

	subscription, subErr := natsConnection.QueueSubscribe(
		"png.created",
		"ocr_workers",
		handler.Handle,
	)
	if subErr != nil {
		return fmt.Errorf(
			"failed to subscribe to NATS topic 'png.created': %w",
			subErr,
		)
	}

	loggerInstance.Info(
		"Worker is running, subscribed to 'png.created' on queue 'ocr_workers'...",
	)

	return waitForShutdown(subscription, loggerInstance)
}

// waitForShutdown blocks until an OS signal is received, then gracefully drains the
// subscription.
func waitForShutdown(
	subscription *nats.Subscription,
	loggerInstance *logger.Logger,
) error {
	quitSignal := make(chan os.Signal, 1)
	signal.Notify(quitSignal, syscall.SIGINT, syscall.SIGTERM)
	<-quitSignal

	loggerInstance.Info("Shutdown signal received, draining NATS subscription...")

	drainErr := subscription.Drain()
	if drainErr != nil {
		return fmt.Errorf("failed to drain NATS subscription: %w", drainErr)
	}

	loggerInstance.Info("Shutdown complete.")

	return nil
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

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/book-expert/logger"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/book-expert/png-to-text-service/internal/config"
	"github.com/book-expert/png-to-text-service/internal/llm"
	"github.com/book-expert/png-to-text-service/internal/worker"
)

const (
	// Configuration Constants
	ConfigFileName = "project.toml"
	LogFileName    = "png-to-text.log"
)

// Application encapsulates the service dependencies and lifecycle management.
type Application struct {
	configuration    *config.Config
	logger           *logger.Logger
	natsConnection   *nats.Conn
	jetStreamContext jetstream.JetStream
	workerInstance   *worker.Worker
}

func main() {
	// Create a root context that cancels on system interrupts.
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	if err := runApplication(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Fatal application error: %v\n", err)
		os.Exit(1)
	}
}

// runApplication orchestrates the startup sequence.
func runApplication(ctx context.Context) error {
	// 1. Initialize Application State
	app, err := newApplication(ctx)
	if err != nil {
		return err
	}

	// Ensure resources are cleaned up on exit.
	defer app.cleanup()

	// Correct: Using Infof
	app.logger.Infof("Starting PNG-to-Text Service...")

	// 2. Start the Worker Loop
	// Correct: Using Infof with formatting
	app.logger.Infof("Worker initializing on subject: %s", app.configuration.NATS.Consumer.Subject)

	if err := app.workerInstance.Start(ctx); err != nil {
		// Only log if it's not a normal shutdown signal
		if !errors.Is(err, context.Canceled) {
			// Correct: Using Errorf
			app.logger.Errorf("Worker stopped unexpectedly: %v", err)
			return err
		}
	}

	app.logger.Infof("Shutdown complete.")
	return nil
}

// newApplication handles the complexity of wiring dependencies together.
func newApplication(ctx context.Context) (*Application, error) {
	// 1. Load Configuration
	discardLogger, _ := logger.New("", "")
	cfg, err := config.Load(ConfigFileName, discardLogger)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	// 2. Setup Real Logger
	appLogger, err := logger.New(cfg.Service.LogDir, LogFileName)
	if err != nil {
		return nil, fmt.Errorf("setup logger: %w", err)
	}

	// 3. Setup LLM Processor
	apiKey := cfg.GetAPIKey()
	if apiKey == "" {
		return nil, errors.New("LLM API key not found in environment variables")
	}

	llmConfig := &llm.Config{
		APIKey:            apiKey,
		Model:             cfg.LLM.Model,
		Temperature:       cfg.LLM.Temperature,
		TimeoutSeconds:    cfg.LLM.TimeoutSeconds,
		MaxRetries:        cfg.LLM.MaxRetries,
		SystemInstruction: cfg.LLM.SystemInstruction,
		ExtractionPrompt:  cfg.LLM.ExtractionPrompt,
	}

	llmProcessor, err := llm.NewProcessor(ctx, llmConfig, appLogger)
	if err != nil {
		return nil, fmt.Errorf("setup LLM processor: %w", err)
	}

	// 4. Setup NATS
	natsConn, jsContext, err := setupNATS(cfg)
	if err != nil {
		return nil, fmt.Errorf("setup NATS: %w", err)
	}

	// 5. Bind Object Stores
	pngStore, err := jsContext.ObjectStore(context.Background(), cfg.NATS.ObjectStore.PNGBucket)
	if err != nil {
		natsConn.Close()
		return nil, fmt.Errorf("bind PNG object store (%s): %w", cfg.NATS.ObjectStore.PNGBucket, err)
	}

	textStore, err := jsContext.ObjectStore(context.Background(), cfg.NATS.ObjectStore.TextBucket)
	if err != nil {
		natsConn.Close()
		return nil, fmt.Errorf("bind Text object store (%s): %w", cfg.NATS.ObjectStore.TextBucket, err)
	}

	// 6. Initialize Worker
	workerInstance := worker.New(
		jsContext,
		cfg.NATS.Consumer.Stream,
		cfg.NATS.Consumer.Durable,
		cfg.NATS.Consumer.Subject,
		cfg.NATS.Producer.Subject,
		llmProcessor,
		appLogger,
		pngStore,
		textStore,
		cfg.Service.Workers,
	)

	return &Application{
		configuration:    cfg,
		logger:           appLogger,
		natsConnection:   natsConn,
		jetStreamContext: jsContext,
		workerInstance:   workerInstance,
	}, nil
}

// cleanup closes open connections and flushes logs.
func (app *Application) cleanup() {
	if app.natsConnection != nil {
		app.natsConnection.Close()
	}
	if app.logger != nil {
		if err := app.logger.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to close logger: %v\n", err)
		}
	}
}

// setupNATS initializes the NATS connection and JetStream context.
func setupNATS(cfg *config.Config) (*nats.Conn, jetstream.JetStream, error) {
	natsConnection, err := nats.Connect(cfg.NATS.URL)
	if err != nil {
		return nil, nil, err
	}

	jetStreamContext, err := jetstream.New(natsConnection)
	if err != nil {
		natsConnection.Close()
		return nil, nil, err
	}

	return natsConnection, jetStreamContext, nil
}

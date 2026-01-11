/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */

package main

import (
	"context"
	"errors"
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
	rootContext, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	if runError := runApplication(rootContext); runError != nil {
		// Since application failed to start, we may not have a logger yet.
		// However, we should try to use the logger if it was created in runApplication.
		// For simplicity in main, we exit. runApplication should have logged it.
		os.Exit(1)
	}
}

// runApplication orchestrates the startup sequence.
func runApplication(rootContext context.Context) error {
	// 1. Initialize Application State
	application, applicationError := newApplication(rootContext)
	if applicationError != nil {
		// Log to stderr only if we don't have a logger yet (handled in newApplication)
		return applicationError
	}

	// Ensure resources are cleaned up on exit.
	defer application.cleanup()

	application.logger.Infof("Starting PNG-to-Text Service...")

	// 2. Start the Worker Loop
	application.logger.Infof("Worker initializing on subject: %s", application.configuration.NATS.Consumer.Subject)

	if workerError := application.workerInstance.Start(rootContext); workerError != nil {
		// Only log if it's not a normal shutdown signal
		if !errors.Is(workerError, context.Canceled) {
			application.logger.Errorf("Worker stopped unexpectedly: %v", workerError)
			return workerError
		}
	}

	application.logger.Infof("Shutdown complete.")
	return nil
}

// newApplication handles the complexity of wiring dependencies together.
func newApplication(rootContext context.Context) (*Application, error) {
	// 1. Load Configuration
	discardLogger, _ := logger.New("", "")
	configuration, configLoadError := config.Load(ConfigFileName, discardLogger)
	if configLoadError != nil {
		return nil, configLoadError
	}

	// 2. Setup Real Logger
	appLogger, loggerInitError := logger.New(configuration.Service.LogDir, LogFileName)
	if loggerInitError != nil {
		return nil, loggerInitError
	}

	// 3. Setup LLM Processor
	apiKey := configuration.GetAPIKey()
	if apiKey == "" {
		return nil, errors.New("LLM API key not found in environment variables")
	}

	llmConfig := &llm.Config{
		APIKey:            apiKey,
		Model:             configuration.LLM.Model,
		Temperature:       configuration.LLM.Temperature,
		TimeoutSeconds:    configuration.LLM.TimeoutSeconds,
		MaxRetries:        configuration.LLM.MaxRetries,
		SystemInstruction: configuration.LLM.SystemInstruction,
		ExtractionPrompt:  configuration.LLM.ExtractionPrompt,
	}

	llmProcessor, llmInitError := llm.NewProcessor(rootContext, llmConfig, appLogger)
	if llmInitError != nil {
		return nil, llmInitError
	}

	// 4. Setup NATS
	natsConnection, jetStream, natsInitError := setupNATS(configuration)
	if natsInitError != nil {
		return nil, natsInitError
	}

	// 5. Bind Object Stores (Create if missing)
	pngStore, pngStoreError := jetStream.ObjectStore(context.Background(), configuration.NATS.ObjectStore.PNGBucket)
	if pngStoreError != nil {
		appLogger.Infof("Object Store '%s' not found, attempting to create...", configuration.NATS.ObjectStore.PNGBucket)
		pngStore, pngStoreError = jetStream.CreateObjectStore(context.Background(), jetstream.ObjectStoreConfig{
			Bucket: configuration.NATS.ObjectStore.PNGBucket,
		})
		if pngStoreError != nil {
			natsConnection.Close()
			return nil, pngStoreError
		}
	}

	textStore, textStoreError := jetStream.ObjectStore(context.Background(), configuration.NATS.ObjectStore.TextBucket)
	if textStoreError != nil {
		appLogger.Infof("Object Store '%s' not found, attempting to create...", configuration.NATS.ObjectStore.TextBucket)
		textStore, textStoreError = jetStream.CreateObjectStore(context.Background(), jetstream.ObjectStoreConfig{
			Bucket: configuration.NATS.ObjectStore.TextBucket,
		})
		if textStoreError != nil {
			natsConnection.Close()
			return nil, textStoreError
		}
	}

	// 6. Initialize Worker
	workerInstance := worker.New(
		jetStream,
		configuration.NATS.Consumer.Stream,
		configuration.NATS.Consumer.Durable,
		configuration.NATS.Consumer.Subject,
		configuration.NATS.Producer.Subject,
		configuration.NATS.Producer.StartedSubject,
		llmProcessor,
		appLogger,
		pngStore,
		textStore,
		configuration.Service.Workers,
	)

	return &Application{
		configuration:    configuration,
		logger:           appLogger,
		natsConnection:   natsConnection,
		jetStreamContext: jetStream,
		workerInstance:   workerInstance,
	}, nil
}

// cleanup closes open connections and flushes logs.
func (application *Application) cleanup() {
	if application.natsConnection != nil {
		application.natsConnection.Close()
	}
	if application.logger != nil {
		_ = application.logger.Close()
	}
}

// setupNATS initializes the NATS connection and JetStream context.
func setupNATS(configuration *config.Config) (*nats.Conn, jetstream.JetStream, error) {
	natsConnection, natsConnectError := nats.Connect(configuration.NATS.URL)
	if natsConnectError != nil {
		return nil, nil, natsConnectError
	}

	jetStreamContext, jetStreamInitError := jetstream.New(natsConnection)
	if jetStreamInitError != nil {
		natsConnection.Close()
		return nil, nil, jetStreamInitError
	}

	// Ensure the Producer stream exists
	_, streamError := jetStreamContext.Stream(context.Background(), configuration.NATS.Producer.Stream)
	if streamError != nil {
		_, createError := jetStreamContext.CreateStream(context.Background(), jetstream.StreamConfig{
			Name:     configuration.NATS.Producer.Stream,
			Subjects: []string{configuration.NATS.Producer.Stream + ".*"}, // Catch-all for the stream prefix
			Storage:  jetstream.FileStorage,
		})
		if createError != nil {
			// If creation failed, try one more time to get it (in case of a race)
			_, retryError := jetStreamContext.Stream(context.Background(), configuration.NATS.Producer.Stream)
			if retryError != nil {
				natsConnection.Close()
				return nil, nil, createError
			}
		}
	}

	return natsConnection, jetStreamContext, nil
}
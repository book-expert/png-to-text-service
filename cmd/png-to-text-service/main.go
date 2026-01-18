/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/book-expert/common-events"
	"github.com/book-expert/logger"
	"github.com/book-expert/png-to-text-service/internal/config"
	"github.com/book-expert/png-to-text-service/internal/llm"
	"github.com/book-expert/png-to-text-service/internal/processor"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	LogFileName = "png-to-text-service.log"
)

// Application encapsulates the service dependencies.
type Application struct {
	configuration    *config.Config
	serviceLogger    *logger.Logger
	natsConnection   *nats.Conn
	jetStreamContext jetstream.JetStream
	processor        *processor.Processor
}

func main() {
	rootContext, cancelStopSignal := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancelStopSignal()

	if executionError := run(rootContext); executionError != nil {
		logDirectory := os.Getenv("LOG_DIR")
		if logDirectory == "" {
			logDirectory = os.TempDir()
		}
		exitLogger, loggerInitializationError := logger.New(logDirectory, LogFileName)
		if loggerInitializationError == nil {
			exitLogger.Errorf("Fatal application error: %v", executionError)
			_ = exitLogger.Close()
		}
		os.Exit(1)
	}
}

func run(parentContext context.Context) error {
	application, applicationInitializationError := newApplication(parentContext)
	if applicationInitializationError != nil {
		return applicationInitializationError
	}
	defer application.cleanup()

	application.serviceLogger.Infof("Starting PNG-to-Text Service...")
	return application.processor.Start(parentContext)
}

func newApplication(parentContext context.Context) (*Application, error) {
	logDirectory := os.Getenv("LOG_DIR")
	if logDirectory == "" {
		logDirectory = os.TempDir()
	}

	// 1. Initial Logger for Config Loading
	initialLogger, _ := logger.New(logDirectory, LogFileName)
	configuration, configurationLoadError := config.Load("", initialLogger)
	if configurationLoadError != nil {
		_ = initialLogger.Close()
		return nil, configurationLoadError
	}

	// 2. Final Service Logger
	serviceLogger, loggerInitializationError := logger.New(logDirectory, LogFileName)
	if loggerInitializationError != nil {
		_ = initialLogger.Close()
		return nil, loggerInitializationError
	}
	_ = initialLogger.Close()

	// 3. NATS setup
	natsConnection, jetStreamContext, natsSetupError := setupNATS(configuration)
	if natsSetupError != nil {
		_ = serviceLogger.Close()
		return nil, natsSetupError
	}

	// 4. Object Stores
	pngObjectStore, pngStoreError := getObjectStore(parentContext, jetStreamContext, events.BucketPngFiles)
	if pngStoreError != nil {
		natsConnection.Close()
		_ = serviceLogger.Close()
		return nil, pngStoreError
	}

	textObjectStore, textStoreError := getObjectStore(parentContext, jetStreamContext, events.BucketTextFiles)
	if textStoreError != nil {
		natsConnection.Close()
		_ = serviceLogger.Close()
		return nil, textStoreError
	}

	// 5. LLM processor
	llmConfiguration := llm.Config{
		APIKey:            configuration.GetAPIKey(),
		Model:             configuration.LLM.Model,
		Temperature:       configuration.LLM.Temperature,
		MaxOutputTokens:   configuration.LLM.MaxOutputTokens,
		TimeoutSeconds:    configuration.LLM.TimeoutSeconds,
		MaxRetries:        configuration.LLM.MaxRetries,
		SystemInstruction: configuration.LLM.SystemInstruction,
		ExtractionPrompt:  configuration.LLM.ExtractionPrompt,
	}
	llmProcessor, llmInitializationError := llm.NewProcessor(parentContext, &llmConfiguration, serviceLogger)
	if llmInitializationError != nil {
		natsConnection.Close()
		_ = serviceLogger.Close()
		return nil, llmInitializationError
	}

	// 6. Processor
	processorInstance, processorInitializationError := processor.NewProcessor(
		natsConnection,
		jetStreamContext,
		jetStreamContext,
		events.StreamPngFiles,
		events.SubjectPngCreated,
		configuration.NATS.Consumer.DurableName,
		events.SubjectTextCreated,
		llmProcessor,
		serviceLogger,
		pngObjectStore,
		textObjectStore,
		configuration.Service.Workers,
	)
	if processorInitializationError != nil {
		natsConnection.Close()
		_ = serviceLogger.Close()
		return nil, processorInitializationError
	}

	return &Application{
		configuration:    configuration,
		serviceLogger:    serviceLogger,
		natsConnection:   natsConnection,
		jetStreamContext: jetStreamContext,
		processor:        processorInstance,
	}, nil
}

func (application *Application) cleanup() {
	if application.natsConnection != nil {
		application.natsConnection.Close()
	}
	if application.serviceLogger != nil {
		_ = application.serviceLogger.Close()
	}
}

func setupNATS(configuration *config.Config) (*nats.Conn, jetstream.JetStream, error) {
	natsConnection, connectionError := nats.Connect(configuration.NATS.URL)
	if connectionError != nil {
		return nil, nil, connectionError
	}

	jetStreamContext, jetStreamError := jetstream.New(natsConnection)
	if jetStreamError != nil {
		natsConnection.Close()
		return nil, nil, jetStreamError
	}

	return natsConnection, jetStreamContext, nil
}

func getObjectStore(parentContext context.Context, jetStream jetstream.JetStream, bucket string) (jetstream.ObjectStore, error) {
	return jetStream.ObjectStore(parentContext, bucket)
}

/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/book-expert/logger"
	"github.com/book-expert/png-to-text-service/internal/config"
	"github.com/book-expert/png-to-text-service/internal/llm"
	"github.com/book-expert/png-to-text-service/internal/worker"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	ConfigFileName = "project.toml"
	LogFileName    = "png-to-text-service.log"
)

// Application encapsulates the service dependencies.
type Application struct {
	configuration    *config.Config
	logger           *logger.Logger
	natsConnection   *nats.Conn
	jetStreamContext jetstream.JetStream
	workerInstance   *worker.Worker
}

func main() {
	rootContext, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	if executionError := run(rootContext); executionError != nil {
		logDirectory := os.Getenv("LOG_DIR")
		if logDirectory == "" {
			logDirectory = os.TempDir()
		}
		exitLogger, loggerError := logger.New(logDirectory, LogFileName)
		if loggerError == nil {
			exitLogger.Errorf("Fatal application error: %v", executionError)
			_ = exitLogger.Close()
		}
		os.Exit(1)
	}
}

func run(parentContext context.Context) error {
	application, applicationError := newApplication(parentContext)
	if applicationError != nil {
		return applicationError
	}
	defer application.cleanup()

	application.logger.Infof("Starting PNG-to-Text Service...")
	return application.workerInstance.Start(parentContext)
}

func newApplication(parentContext context.Context) (*Application, error) {
	logDirectory := os.Getenv("LOG_DIR")
	if logDirectory == "" {
		logDirectory = os.TempDir()
	}

	// 1. Initial Logger for Config Loading
	initLogger, _ := logger.New(logDirectory, LogFileName)
	configuration, configLoadError := config.Load(ConfigFileName, initLogger)
	if configLoadError != nil {
		_ = initLogger.Close()
		return nil, configLoadError
	}

	// 2. Final Logger
	appLogger, loggerInitError := logger.New(logDirectory, LogFileName)
	if loggerInitError != nil {
		_ = initLogger.Close()
		return nil, loggerInitError
	}
	_ = initLogger.Close()

	// 3. NATS setup
	natsConnection, jetStreamContext, natsSetupError := setupNATS(configuration)
	if natsSetupError != nil {
		_ = appLogger.Close()
		return nil, natsSetupError
	}

	// 4. Stores
	pngStore, pngStoreError := getObjectStore(parentContext, jetStreamContext, configuration.NATS.ObjectStore.PNGBucket)
	if pngStoreError != nil {
		natsConnection.Close()
		_ = appLogger.Close()
		return nil, pngStoreError
	}

	textStore, textStoreError := getObjectStore(parentContext, jetStreamContext, configuration.NATS.ObjectStore.TextBucket)
	if textStoreError != nil {
		natsConnection.Close()
		_ = appLogger.Close()
		return nil, textStoreError
	}

	// 5. LLM client
	llmConfig := llm.Config{
		APIKey:            configuration.GetAPIKey(),
		Model:             configuration.LLM.Model,
		Temperature:       configuration.LLM.Temperature,
		TimeoutSeconds:    configuration.LLM.TimeoutSeconds,
		MaxRetries:        configuration.LLM.MaxRetries,
		SystemInstruction: configuration.LLM.SystemInstruction,
		ExtractionPrompt:  configuration.LLM.ExtractionPrompt,
	}
	llmClient, llmInitError := llm.NewProcessor(parentContext, &llmConfig, appLogger)
	if llmInitError != nil {
		natsConnection.Close()
		_ = appLogger.Close()
		return nil, llmInitError
	}

	// 6. Worker
	workerInstance := worker.New(
		natsConnection,
		jetStreamContext,
		configuration.NATS.Consumer.Stream,
		configuration.NATS.Consumer.Durable,
		configuration.NATS.Consumer.Subject,
		configuration.NATS.Producer.Subject,
		configuration.NATS.Producer.StartedSubject,
		llmClient,
		appLogger,
		pngStore,
		textStore,
		configuration.Service.Workers,
	)

	return &Application{
		configuration:    configuration,
		logger:           appLogger,
		natsConnection:   natsConnection,
		jetStreamContext: jetStreamContext,
		workerInstance:   workerInstance,
	}, nil
}

func (application *Application) cleanup() {
	if application.natsConnection != nil {
		application.natsConnection.Close()
	}
	if application.logger != nil {
		_ = application.logger.Close()
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

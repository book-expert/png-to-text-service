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

	if err := run(rootContext); err != nil {
		logDir := os.Getenv("LOG_DIR")
		if logDir == "" {
			logDir = os.TempDir()
		}
		exitLogger, loggerErr := logger.New(logDir, LogFileName)
		if loggerErr == nil {
			exitLogger.Errorf("Fatal application error: %v", err)
			_ = exitLogger.Close()
		}
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	application, err := newApplication(ctx)
	if err != nil {
		return err
	}
	defer application.cleanup()

	application.logger.Infof("Starting PNG-to-Text Service...")
	return application.workerInstance.Start(ctx)
}

func newApplication(ctx context.Context) (*Application, error) {
	logDir := os.Getenv("LOG_DIR")
	if logDir == "" {
		logDir = os.TempDir()
	}

	// 1. Initial Logger for Config Loading
	initLogger, _ := logger.New(logDir, LogFileName)
	configuration, err := config.Load(ConfigFileName, initLogger)
	if err != nil {
		_ = initLogger.Close()
		return nil, err
	}

	// 2. Final Logger
	appLogger, err := logger.New(logDir, LogFileName)
	if err != nil {
		_ = initLogger.Close()
		return nil, err
	}
	_ = initLogger.Close()

	// 3. NATS setup
	natsConnection, jetStreamContext, err := setupNATS(configuration)
	if err != nil {
		_ = appLogger.Close()
		return nil, err
	}

	// 4. Stores
	pngStore, err := ensureObjectStore(ctx, jetStreamContext, configuration.NATS.ObjectStore.PNGBucket)
	if err != nil {
		natsConnection.Close()
		_ = appLogger.Close()
		return nil, err
	}

	textStore, err := ensureObjectStore(ctx, jetStreamContext, configuration.NATS.ObjectStore.TextBucket)
	if err != nil {
		natsConnection.Close()
		_ = appLogger.Close()
		return nil, err
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
	llmClient, err := llm.NewProcessor(ctx, &llmConfig, appLogger)
	if err != nil {
		natsConnection.Close()
		_ = appLogger.Close()
		return nil, err
	}

	// 6. Worker
	workerInstance := worker.New(
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

func (app *Application) cleanup() {
	if app.natsConnection != nil {
		app.natsConnection.Close()
	}
	if app.logger != nil {
		_ = app.logger.Close()
	}
}

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

	// Ensure the Producer stream exists
	_, err = jetStreamContext.Stream(context.Background(), cfg.NATS.Producer.Stream)
	if err != nil {
		_, createErr := jetStreamContext.CreateStream(context.Background(), jetstream.StreamConfig{
			Name:     cfg.NATS.Producer.Stream,
			Subjects: []string{cfg.NATS.Producer.Stream + ".*"}, // Catch-all for the stream prefix
			Storage:  jetstream.FileStorage,
		})
		if createErr != nil {
			// If creation failed, try one more time to get it (in case of a race)
			_, retryErr := jetStreamContext.Stream(context.Background(), cfg.NATS.Producer.Stream)
			if retryErr != nil {
				natsConnection.Close()
				return nil, nil, createErr
			}
		}
	}

	return natsConnection, jetStreamContext, nil
}

func ensureObjectStore(ctx context.Context, js jetstream.JetStream, bucket string) (jetstream.ObjectStore, error) {
	store, err := js.ObjectStore(ctx, bucket)
	if err != nil {
		store, err = js.CreateObjectStore(ctx, jetstream.ObjectStoreConfig{
			Bucket:  bucket,
			Storage: jetstream.FileStorage,
		})
		if err != nil {
			// Try one more time to bind in case of race condition
			store, err = js.ObjectStore(ctx, bucket)
			if err != nil {
				return nil, err
			}
		}
	}
	return store, nil
}

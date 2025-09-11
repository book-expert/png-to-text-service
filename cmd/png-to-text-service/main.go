// ./cmd/png-to-text-service/main.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nnikolov3/logger"

	"github.com/nnikolov3/png-to-text-service/internal/augment"
	"github.com/nnikolov3/png-to-text-service/internal/config"
	"github.com/nnikolov3/png-to-text-service/internal/pipeline"
)

const (
	pngCreatedSubject  = "png.created"
	pngConsumerName    = "png-text-workers"
	pngStreamName      = "PNG_PROCESSING"
	pngObjectStoreName = "PNG_FILES"
)

// PngJob defines the structure for incoming job messages.
type PngJob struct {
	ObjectName          string                      `json:"objectName"`
	AugmentationOptions augment.AugmentationOptions `json:"augmentationOptions"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	appConfig, err := loadConfiguration()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	appLogger, err := initializeLogger(appConfig)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer appLogger.Close()

	if err := run(ctx, appConfig, appLogger); err != nil {
		appLogger.Fatal("Fatal application error: %v", err)
	}
	appLogger.Info("Application shut down gracefully.")
}

func run(ctx context.Context, appConfig *config.Config, appLogger *logger.Logger) error {
	nc, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer nc.Close()
	appLogger.Info("Connected to NATS server at %s", nc.ConnectedUrl())

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("failed to create JetStream context: %w", err)
	}

	if err := setupJetStream(ctx, js, appLogger); err != nil {
		return fmt.Errorf("failed to set up JetStream resources: %w", err)
	}

	consumer, err := js.Consumer(ctx, pngStreamName, pngConsumerName)
	if err != nil {
		return fmt.Errorf("failed to get consumer: %w", err)
	}

	processingPipeline, err := pipeline.NewPipeline(appConfig, appLogger)
	if err != nil {
		return fmt.Errorf("failed to create pipeline: %w", err)
	}

	appLogger.Info("Worker is running, listening for jobs on '%s'...", pngCreatedSubject)
	return processMessages(ctx, consumer, js, processingPipeline, appConfig)
}

func setupJetStream(ctx context.Context, js jetstream.JetStream, appLogger *logger.Logger) error {
	stream, err := js.Stream(ctx, pngStreamName)
	if err != nil {
		return fmt.Errorf(
			"could not get stream handle for '%s': %w. Ensure the producer service is running first",
			pngStreamName,
			err,
		)
	}
	appLogger.Info("Found stream '%s'.", pngStreamName)

	_, err = stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       pngConsumerName,
		FilterSubject: pngCreatedSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}
	appLogger.Info("Consumer '%s' is ready.", pngConsumerName)

	return nil
}

func processMessages(
	ctx context.Context,
	consumer jetstream.Consumer,
	js jetstream.JetStream,
	procPipeline *pipeline.Pipeline,
	appConfig *config.Config,
) error {
	pngStore, err := js.ObjectStore(ctx, pngObjectStoreName)
	if err != nil {
		return fmt.Errorf("failed to bind to object store '%s': %w", pngObjectStoreName, err)
	}

	for {
		if ctx.Err() != nil {
			return nil
		}
		batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, nats.ErrTimeout) {
				continue
			}
			log.Printf("Error fetching messages: %v", err)
			continue
		}
		for msg := range batch.Messages() {
			handleMessage(ctx, msg, pngStore, procPipeline, appConfig)
		}
		if batch.Error() != nil {
			log.Printf("Error during message batch processing: %v", batch.Error())
		}
	}
}

func handleMessage(
	ctx context.Context,
	msg jetstream.Msg,
	pngStore jetstream.ObjectStore,
	procPipeline *pipeline.Pipeline,
	appConfig *config.Config,
) {
	var job PngJob
	if err := json.Unmarshal(msg.Data(), &job); err != nil {
		log.Printf("Failed to unmarshal job JSON: %v. Acknowledging to discard.", err)
		msg.Ack()
		return
	}

	if job.ObjectName == "" {
		log.Println("Received job with empty object name. Acknowledging to discard.")
		msg.Ack()
		return
	}
	log.Printf("Received job for PNG object: %s", job.ObjectName)
	msg.InProgress()

	obj, err := pngStore.Get(ctx, job.ObjectName)
	if err != nil {
		log.Printf("Failed to get object '%s': %v", job.ObjectName, err)
		if errors.Is(err, jetstream.ErrObjectNotFound) {
			msg.Ack()
		} else {
			msg.Nak()
		}
		return
	}
	defer obj.Close()

	tempFile, err := os.CreateTemp("", "ocr-*.png")
	if err != nil {
		log.Printf("Failed to create temp file for '%s': %v", job.ObjectName, err)
		msg.Nak()
		return
	}
	defer os.Remove(tempFile.Name())

	if _, err := io.Copy(tempFile, obj); err != nil {
		log.Printf("Failed to write to temp file for '%s': %v", job.ObjectName, err)
		tempFile.Close()
		msg.Nak()
		return
	}
	tempFile.Close()

	txtFileName := strings.TrimSuffix(job.ObjectName, filepath.Ext(job.ObjectName)) + ".txt"
	finalOutputPath := filepath.Join(appConfig.Paths.OutputDir, txtFileName)

	if err := procPipeline.ProcessSingle(ctx, tempFile.Name(), finalOutputPath, &job.AugmentationOptions); err != nil {
		log.Printf("Pipeline failed for '%s': %v", job.ObjectName, err)
		msg.Nak()
		return
	}

	if err := msg.Ack(); err != nil {
		log.Printf("Failed to acknowledge message for '%s': %v", job.ObjectName, err)
	} else {
		log.Printf("Successfully processed and acknowledged job for '%s'.", job.ObjectName)
	}
}

func loadConfiguration() (*config.Config, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("could not get working directory: %w", err)
	}
	_, configPath, err := config.FindProjectRoot(wd)
	if err != nil {
		return nil, fmt.Errorf("could not find project configuration: %w", err)
	}
	return config.Load(configPath)
}

func initializeLogger(appConfig *config.Config) (*logger.Logger, error) {
	logFileName := fmt.Sprintf(
		"png-to-text-service_%s.log",
		time.Now().Format("2006-01-02_15-04-05"),
	)
	logFilePath := appConfig.GetLogFilePath(logFileName)
	return logger.New(filepath.Dir(logFilePath), filepath.Base(logFilePath))
}

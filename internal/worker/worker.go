// DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS

/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */

package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/book-expert/common-events"
	"github.com/book-expert/common-worker"
	"github.com/book-expert/logger"
	"github.com/book-expert/png-to-text-service/internal/core"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// MessageProcessingTimeout defines the maximum duration allowed for processing a single image text extraction job.
	MessageProcessingTimeout = 600 * time.Second
)

// JetStreamPublisher defines the interface for publishing messages to JetStream.
type JetStreamPublisher interface {
	Publish(requestContext context.Context, subject string, data []byte, options ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// Worker manages the lifecycle of processing image-to-text conversion requests from NATS.
type Worker struct {
	baseWorker         *worker.Worker[*events.PngCreatedEvent]
	jetStreamPublisher JetStreamPublisher
	producerSubject    string
	pngStore           jetstream.ObjectStore
	textStore          jetstream.ObjectStore
	llm                core.LLMProcessor
	logger             *logger.Logger
}

// New initializes a new Worker with all necessary dependencies.
func New(
	natsConnection *nats.Conn,
	jetStreamContext jetstream.JetStream,
	jetStreamPublisher JetStreamPublisher,
	subscriptionStream string,
	subscriptionSubject string,
	consumerDurableName string,
	producerSubject string,
	pngStore jetstream.ObjectStore,
	textStore jetstream.ObjectStore,
	llm core.LLMProcessor,
	serviceLogger *logger.Logger,
	workerCount int,
) (*Worker, error) {
	pngWorker := &Worker{
		jetStreamPublisher: jetStreamPublisher,
		producerSubject:    producerSubject,
		pngStore:           pngStore,
		textStore:          textStore,
		llm:                llm,
		logger:             serviceLogger,
	}

	workerConfiguration := worker.Config{
		StreamName:    subscriptionStream,
		ConsumerName:  consumerDurableName,
		FilterSubject: subscriptionSubject,
		WorkerCount:   workerCount,
		MaxDeliver:    5,
	}

	pngWorker.baseWorker = worker.New(natsConnection, jetStreamContext, serviceLogger, workerConfiguration, pngWorker.handleMessage)
	return pngWorker, nil
}

// Run executes the main worker loop.
func (pngWorker *Worker) Run(systemContext context.Context) error {
	return pngWorker.baseWorker.Start(systemContext)
}

func (pngWorker *Worker) handleMessage(requestContext context.Context, event *events.PngCreatedEvent, message jetstream.Msg) error {
	parentContext, cancelProcessing := context.WithTimeout(requestContext, MessageProcessingTimeout)
	defer cancelProcessing()

	pngWorker.logger.Infof("Processing: %s (Page %d)", event.PngKey, event.PageNumber)

	if workflowError := pngWorker.executeWorkflow(parentContext, event); workflowError != nil {
		pngWorker.logger.Errorf("Workflow failed [%s]: %v", event.PngKey, workflowError)
		// Nak with delay to allow retry
		_ = message.NakWithDelay(10 * time.Second)
		return workflowError
	}

	pngWorker.logger.Successf("Completed: %s", event.PngKey)
	return nil
}

func (pngWorker *Worker) executeWorkflow(parentContext context.Context, event *events.PngCreatedEvent) error {
	// Step 1: Download image
	pngData, downloadError := pngWorker.downloadPNG(parentContext, event.PngKey)
	if downloadError != nil {
		return fmt.Errorf("download: %w", downloadError)
	}

	// Step 2: Extract text via LLM
	extractedText, llmError := pngWorker.llm.ProcessImage(parentContext, event.PngKey, pngData, event.Settings)
	if llmError != nil {
		return fmt.Errorf("llm: %w", llmError)
	}

	// JSON format the extracted text segments for the tts-service contract
	// tts-service expects a JSON array of strings: []string
	jsonText, _ := json.Marshal([]string{extractedText})

	// Step 3: Store (Idempotent)
	textKey, storeError := pngWorker.storeText(parentContext, event, string(jsonText))
	if storeError != nil {
		return fmt.Errorf("store: %w", storeError)
	}

	// Step 4: Publish
	if completionError := pngWorker.publishCompletion(parentContext, event, textKey); completionError != nil {
		return fmt.Errorf("publish: %w", completionError)
	}

	return nil
}

func (pngWorker *Worker) downloadPNG(parentContext context.Context, key string) ([]byte, error) {
	object, getError := pngWorker.pngStore.Get(parentContext, key)
	if getError != nil {
		return nil, getError
	}
	defer func() {
		_ = object.Error()
	}()

	data := make([]byte, object.Info().Size)
	_, readError := object.Read(data)
	return data, readError
}

func (pngWorker *Worker) storeText(parentContext context.Context, event *events.PngCreatedEvent, content string) (string, error) {
	baseName := strings.TrimSuffix(event.PngKey, ".png")
	textKey := baseName + ".json"

	metadata := jetstream.ObjectMeta{
		Name:        textKey,
		Description: fmt.Sprintf("Text extraction for %s", event.PngKey),
	}

	_, putError := pngWorker.textStore.Put(parentContext, metadata, bytes.NewReader([]byte(content)))
	return textKey, putError
}

func (pngWorker *Worker) publishCompletion(parentContext context.Context, source *events.PngCreatedEvent, textKey string) error {
	event := events.TextProcessedEvent{
		Header: events.EventHeader{
			WorkflowIdentifier: source.Header.WorkflowIdentifier,
			UserIdentifier:     source.Header.UserIdentifier,
			TenantIdentifier:   source.Header.TenantIdentifier,
			EventIdentifier:    uuid.New().String(),
			Timestamp:          time.Now().UTC(),
		},
		PngKey:     source.PngKey,
		TextKey:    textKey,
		PageNumber: source.PageNumber,
		TotalPages: source.TotalPages,
		Settings:   source.Settings,
	}

	data, _ := json.Marshal(event)
	_, publishError := pngWorker.jetStreamPublisher.Publish(parentContext, pngWorker.producerSubject, data)
	return publishError
}

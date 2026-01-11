/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */

package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/book-expert/common-events"
	"github.com/book-expert/common-worker"
	"github.com/book-expert/logger"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// LLMProcessor defines the contract for text extraction.
type LLMProcessor interface {
	ProcessImage(parentContext context.Context, objectID string, pngData []byte, settings *events.JobSettings) (string, error)
}

// Worker coordinates the extraction of text from images using an LLM.
type Worker struct {
	baseWorker      *worker.Worker[*events.PNGCreatedEvent]
	natsConn        *nats.Conn
	jetStream       jetstream.JetStream
	pngStore        jetstream.ObjectStore
	textStore       jetstream.ObjectStore
	llm             LLMProcessor
	logger          *logger.Logger
	producerSubject string
	startedSubject  string
}

// New creates a strictly typed Worker using common-worker.
func New(
	natsConn *nats.Conn,
	jetStream jetstream.JetStream,
	streamName, consumerName, filterSubject string,
	producerSubject string,
	startedSubject string,
	llm LLMProcessor,
	loggerInstance *logger.Logger,
	pngStore jetstream.ObjectStore,
	textStore jetstream.ObjectStore,
	workerCount int,
) *Worker {
	pngWorker := &Worker{
		natsConn:        natsConn,
		jetStream:       jetStream,
		pngStore:        pngStore,
		textStore:       textStore,
		llm:             llm,
		logger:          loggerInstance,
		producerSubject: producerSubject,
		startedSubject:  startedSubject,
	}

	config := worker.Config{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: filterSubject,
		WorkerCount:   workerCount,
		MaxDeliver:    5,
	}

	pngWorker.baseWorker = worker.New(natsConn, jetStream, loggerInstance, config, pngWorker.handleMessage)
	return pngWorker
}

// Start initiates the blocking consumption loop.
func (pngWorker *Worker) Start(parentContext context.Context) error {
	return pngWorker.baseWorker.Start(parentContext)
}

func (pngWorker *Worker) handleMessage(parentContext context.Context, event *events.PNGCreatedEvent, jetStreamMessage jetstream.Msg) error {
	pngWorker.logger.Infof("Processing: %s (Page %d)", event.PNGKey, event.PageNumber)

	if workflowError := pngWorker.executeWorkflow(parentContext, event); workflowError != nil {
		pngWorker.logger.Errorf("Workflow failed [%s]: %v", event.PNGKey, workflowError)
		return workflowError
	}

	pngWorker.logger.Successf("Completed: %s", event.PNGKey)
	return nil
}

func (pngWorker *Worker) executeWorkflow(parentContext context.Context, event *events.PNGCreatedEvent) error {
	// Step 0: Publish Extraction Started
	if startedError := pngWorker.publishExtractionStarted(parentContext, event); startedError != nil {
		pngWorker.logger.Warnf("Failed to publish extraction started event: %v", startedError)
	}

	// Step 1: Download
	pngData, downloadError := pngWorker.downloadPNG(parentContext, event.PNGKey)
	if downloadError != nil {
		return fmt.Errorf("download: %w", downloadError)
	}

	// Step 2: LLM Extraction
	extractedText, llmError := pngWorker.llm.ProcessImage(parentContext, event.PNGKey, pngData, event.Settings)
	if llmError != nil {
		return fmt.Errorf("llm: %w", llmError)
	}

	// Step 3: Store (Idempotent)
	// We wrap the text in a JSON array of strings to maintain a consistent contract
	// with the tts-service, which expects this format for segment-based processing.
	jsonText, marshalError := json.Marshal([]string{extractedText})
	if marshalError != nil {
		return fmt.Errorf("marshal text: %w", marshalError)
	}

	textKey, storeError := pngWorker.storeText(parentContext, event, string(jsonText))
	if storeError != nil {
		return fmt.Errorf("store: %w", storeError)
	}

	// Step 4: Publish Next Event
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
	defer func() { _ = object.Close() }()

	return io.ReadAll(object)
}

func (pngWorker *Worker) storeText(parentContext context.Context, event *events.PNGCreatedEvent, content string) (string, error) {
	// Idempotency Fix: Derive text filename from PNG filename.
	baseName := strings.TrimSuffix(event.PNGKey, ".png")
	objectKey := fmt.Sprintf("%s.txt", baseName)

	metadata := jetstream.ObjectMeta{
		Name:        objectKey,
		Description: fmt.Sprintf("Text extraction for %s", event.PNGKey),
	}

	_, putError := pngWorker.textStore.Put(parentContext, metadata, bytes.NewReader([]byte(content)))
	if putError != nil {
		return "", putError
	}
	return objectKey, nil
}

func (pngWorker *Worker) publishExtractionStarted(parentContext context.Context, source *events.PNGCreatedEvent) error {
	if pngWorker.startedSubject == "" {
		return nil
	}

	extractionStartedEvent := events.ExtractionStartedEvent{
		Header:     source.Header,
		PageNumber: source.PageNumber,
		TotalPages: source.TotalPages,
	}

	data, marshalError := json.Marshal(extractionStartedEvent)
	if marshalError != nil {
		return marshalError
	}

	_, publishError := pngWorker.jetStream.Publish(parentContext, pngWorker.startedSubject, data)
	return publishError
}

func (pngWorker *Worker) publishCompletion(parentContext context.Context, source *events.PNGCreatedEvent, textKey string) error {
	textProcessedEvent := events.TextProcessedEvent{
		Header:     source.Header,
		PNGKey:     source.PNGKey,
		TextKey:    textKey,
		PageNumber: source.PageNumber,
		TotalPages: source.TotalPages,
		Settings:   source.Settings,
	}

	data, marshalError := json.Marshal(textProcessedEvent)
	if marshalError != nil {
		return marshalError
	}

	_, publishError := pngWorker.jetStream.Publish(parentContext, pngWorker.producerSubject, data)
	return publishError
}

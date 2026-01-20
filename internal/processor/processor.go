/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */

package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/book-expert/common-events"
	worker "github.com/book-expert/common-worker"
	"github.com/book-expert/logger"
	"github.com/book-expert/png-to-text-service/internal/llm"
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

// Processor coordinates the extraction of text from PNG images using Large Language Models.
type Processor struct {
	engine             *worker.Worker[*events.PngCreatedEvent]
	jetStreamPublisher JetStreamPublisher
	producerSubject    string
	pngObjectStore     jetstream.ObjectStore
	textObjectStore    jetstream.ObjectStore
	llmProcessor       *llm.Processor
	serviceLogger      *logger.Logger
}

// NewProcessor initializes a new Processor with all necessary dependencies.
func NewProcessor(
	natsConnection *nats.Conn,
	jetStreamContext jetstream.JetStream,
	jetStreamPublisher JetStreamPublisher,
	subscriptionStream string,
	subscriptionSubject string,
	consumerDurableName string,
	producerSubject string,
	llmProcessor *llm.Processor,
	serviceLogger *logger.Logger,
	pngObjectStore jetstream.ObjectStore,
	textObjectStore jetstream.ObjectStore,
	workerCount int,
) (*Processor, error) {
	pngProcessor := &Processor{
		jetStreamPublisher: jetStreamPublisher,
		producerSubject:    producerSubject,
		pngObjectStore:     pngObjectStore,
		textObjectStore:    textObjectStore,
		llmProcessor:       llmProcessor,
		serviceLogger:      serviceLogger,
	}

	workerConfiguration := worker.Config{
		StreamName:    subscriptionStream,
		ConsumerName:  consumerDurableName,
		FilterSubject: subscriptionSubject,
		WorkerCount:   workerCount,
		MaxDeliver:    5,
	}

	pngProcessor.engine = worker.New(natsConnection, jetStreamContext, serviceLogger, workerConfiguration, pngProcessor.handleMessage)
	return pngProcessor, nil
}

// Start executes the underlying processor engine.
func (processor *Processor) Start(systemContext context.Context) error {
	return processor.engine.Start(systemContext)
}

func (processor *Processor) handleMessage(requestContext context.Context, event *events.PngCreatedEvent, message jetstream.Msg) error {
	parentContext, cancelProcessing := context.WithTimeout(requestContext, MessageProcessingTimeout)
	defer cancelProcessing()

	processor.serviceLogger.Infof("Processing: %s (Page %d)", event.PngKey, event.PageNumber)

	// Lifecycle: Initialized
	processor.publishLifecycleEvent(parentContext, event, "", events.SubjectTextInitialized)

	textKey, workflowError := processor.executeWorkflow(parentContext, event)
	if workflowError != nil {
		processor.serviceLogger.Errorf("Workflow failed [%s]: %v", event.PngKey, workflowError)
		// Nak with delay to allow retry
		_ = message.NakWithDelay(10 * time.Second)
		return workflowError
	}

	// Lifecycle: Completed
	processor.publishLifecycleEvent(parentContext, event, textKey, events.SubjectTextCompleted)

	processor.serviceLogger.Successf("Completed: %s", event.PngKey)
	return nil
}

func (processor *Processor) executeWorkflow(parentContext context.Context, event *events.PngCreatedEvent) (string, error) {
	// Lifecycle: Ready
	processor.publishLifecycleEvent(parentContext, event, "", events.SubjectTextReady)

	// Step 1: Download image
	pngData, downloadError := processor.downloadPNG(parentContext, event.PngKey)
	if downloadError != nil {
		return "", fmt.Errorf("download: %w", downloadError)
	}

	// Lifecycle: Started (LLM active)
	processor.publishLifecycleEvent(parentContext, event, "", events.SubjectTextStarted)

	// Step 2: Extract text via LLM
	extractedText, llmError := processor.llmProcessor.ProcessImage(parentContext, event.PngKey, pngData, event.Settings, "text/plain", "")
	if llmError != nil {
		return "", fmt.Errorf("llm: %w", llmError)
	}

	processor.serviceLogger.Infof("Extracted Text: %s", extractedText)

	// Step 3: Store (Idempotent)
	textKey, storeError := processor.storeText(parentContext, event, extractedText)
	if storeError != nil {
		return "", fmt.Errorf("store: %w", storeError)
	}

	// Step 4: Publish Created (triggers next step)
	if completionError := processor.publishCreated(parentContext, event, textKey); completionError != nil {
		return "", fmt.Errorf("publish created: %w", completionError)
	}

	return textKey, nil
}

func (processor *Processor) publishLifecycleEvent(ctx context.Context, source *events.PngCreatedEvent, textKey, subject string) {
	var data []byte
	header := events.EventHeader{
		WorkflowIdentifier: source.Header.WorkflowIdentifier,
		UserIdentifier:     source.Header.UserIdentifier,
		TenantIdentifier:   source.Header.TenantIdentifier,
		EventIdentifier:    uuid.New().String(),
		Timestamp:          time.Now().UTC(),
	}

	switch subject {
	case events.SubjectTextInitialized:
		event := events.TextInitializedEvent{Header: header}
		data, _ = json.Marshal(event)
	case events.SubjectTextReady:
		event := events.TextReadyEvent{Header: header}
		data, _ = json.Marshal(event)
	case events.SubjectTextStarted:
		event := events.TextStartedEvent{
			Header:     header,
			PageNumber: source.PageNumber,
			TotalPages: source.TotalPages,
		}
		data, _ = json.Marshal(event)
	case events.SubjectTextCompleted:
		event := events.TextCompletedEvent{
			Header:     header,
			TextKey:    textKey,
			PageNumber: source.PageNumber,
			TotalPages: source.TotalPages,
		}
		data, _ = json.Marshal(event)
	default:
		// Fallback to minimal container if unknown
		container := struct {
			Header events.EventHeader `json:"Header"`
		}{Header: header}
		data, _ = json.Marshal(container)
	}

	_, _ = processor.jetStreamPublisher.Publish(ctx, subject, data)
}

func (processor *Processor) downloadPNG(parentContext context.Context, key string) ([]byte, error) {
	object, getError := processor.pngObjectStore.Get(parentContext, key)
	if getError != nil {
		return nil, getError
	}
	defer func() {
		_ = object.Close()
	}()

	info, _ := object.Info()
	data := make([]byte, info.Size)
	_, readError := object.Read(data)
	return data, readError
}

func (processor *Processor) storeText(parentContext context.Context, event *events.PngCreatedEvent, content string) (string, error) {
	baseName := strings.TrimSuffix(event.PngKey, ".png")
	textKey := baseName + ".txt"

	metadata := jetstream.ObjectMeta{
		Name:        textKey,
		Description: fmt.Sprintf("Text extraction for %s", event.PngKey),
	}

	_, putError := processor.textObjectStore.Put(parentContext, metadata, bytes.NewReader([]byte(content)))
	return textKey, putError
}

func (processor *Processor) publishCreated(parentContext context.Context, source *events.PngCreatedEvent, textKey string) error {
	event := events.TextCreatedEvent{
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
	_, publishError := processor.jetStreamPublisher.Publish(parentContext, processor.producerSubject, data)
	return publishError
}

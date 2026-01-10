/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	common_events "github.com/book-expert/common-events"
	"github.com/book-expert/logger"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// NatsFetchTimeout: How long to block waiting for a message.
	NatsFetchTimeout = 5 * time.Second
	// FetchBatchSize: Keep at 1 for heavy LLM workloads to prevent timeout starvation.
	FetchBatchSize = 1
)

// LLMProcessor defines the contract for text extraction.
type LLMProcessor interface {
	ProcessImage(ctx context.Context, objectID string, pngData []byte, settings *common_events.JobSettings) (string, error)
}

type Worker struct {
	jetStream       jetstream.JetStream
	pngStore        jetstream.ObjectStore
	textStore       jetstream.ObjectStore
	llm             LLMProcessor
	logger          *logger.Logger
	streamName      string
	consumerName    string
	filterSubject   string
	producerSubject string
	startedSubject  string
	workerCount     int
}

// New creates a strictly typed Worker.
func New(
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
	if workerCount < 1 {
		workerCount = 1
	}
	return &Worker{
		jetStream:       jetStream,
		pngStore:        pngStore,
		textStore:       textStore,
		llm:             llm,
		logger:          loggerInstance,
		streamName:      streamName,
		consumerName:    consumerName,
		filterSubject:   filterSubject,
		producerSubject: producerSubject,
		startedSubject:  startedSubject,
		workerCount:     workerCount,
	}
}

// Start initiates the blocking consumption loop.
func (worker *Worker) Start(context context.Context) error {
	// 1. Create or Update Consumer
	consumer, err := worker.jetStream.CreateOrUpdateConsumer(context, worker.streamName, jetstream.ConsumerConfig{
		Durable:       worker.consumerName,
		FilterSubject: worker.filterSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5, // Cap retries to prevent infinite loops
	})
	if err != nil {
		return fmt.Errorf("consumer create/bind failed: %w", err)
	}

	worker.logger.Infof("Worker online. Stream: %s | Consumer: %s | Workers: %d", worker.streamName, worker.consumerName, worker.workerCount)

	errorChannel := make(chan error, worker.workerCount)
	for i := 0; i < worker.workerCount; i++ {
		go func(id int) {
			errorChannel <- worker.consumeLoop(context, consumer, id)
		}(i)
	}

	// Block until context is done or a fatal error occurs in one of the workers
	select {
	case <-context.Done():
		return context.Err()
	case err := <-errorChannel:
		return err
	}
}

func (worker *Worker) consumeLoop(context context.Context, consumer jetstream.Consumer, id int) error {
	for {
		// Fast exit if context canceled before fetch
		select {
		case <-context.Done():
			return context.Err()
		default:
		}

		// 2. Fetch with context awareness
		messages, err := consumer.Fetch(FetchBatchSize, jetstream.FetchMaxWait(NatsFetchTimeout))
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				continue
			}
			// If context is canceled, Fetch might return an error; check context first
			if context.Err() != nil {
				return context.Err()
			}
			worker.logger.Errorf("[Worker %d] Fetch error: %v", id, err)
			// Backoff slightly on infrastructure failure to prevent tight error loops
			select {
			case <-context.Done():
				return context.Err()
			case <-time.After(1 * time.Second):
				continue
			}
		}

		for message := range messages.Messages() {
			worker.handleMessage(context, message)
		}
	}
}

func (worker *Worker) handleMessage(context context.Context, message jetstream.Msg) {
	// 1. Signal Liveness (reset the AckWait timer on the server)
	_ = message.InProgress()

	// Start Keep-Alive (Heartbeat)
	// Prevents NATS from redelivering the message if processing takes longer than AckWait.
	stopKeepAlive := worker.keepAlive(context, message)
	defer stopKeepAlive()

	event, err := worker.parseEvent(message)
	if err != nil {
		worker.logger.Errorf("Malformed event: %v", err)
		// Terminate: Poison pill. Do not retry malformed JSON.
		_ = message.Term()
		return
	}

	worker.logger.Infof("Processing: %s (Page %d)", event.PNGKey, event.PageNumber)

	// 2. Execute Logic
	if err := worker.executeWorkflow(context, event); err != nil {
		worker.logger.Errorf("Workflow failed [%s]: %v", event.PNGKey, err)

		// Nak with Delay (Requires NATS Server v2.10+ for optimal behavior).
		// This tells NATS: "Failed, please redeliver later."
		// We rely on Consumer 'MaxDeliver' config to handle DLQing after N attempts.
		_ = message.NakWithDelay(5 * time.Second)
		return
	}

	// 3. Ack on Success
	if err := message.Ack(); err != nil {
		worker.logger.Errorf("Ack failed: %v", err)
		// Note: If Ack fails, NATS will redeliver.
		// Because storeText is idempotent, this is safe.
	} else {
		worker.logger.Successf("Completed: %s", event.PNGKey)
	}
}

// keepAlive starts a background ticker that periodically sends InProgress signals
// to NATS to prevent the message from being redelivered due to AckWait timeout.
// It returns a cancellation function that must be called when processing is done.
func (worker *Worker) keepAlive(context context.Context, message jetstream.Msg) func() {
	// Send InProgress every 10 seconds.
	// Ensure this is less than the Consumer's AckWait (default often 30s).
	ticker := time.NewTicker(10 * time.Second)
	done := make(chan struct{})

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-context.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				if err := message.InProgress(); err != nil {
					// If we can't signal progress, the connection might be lost.
					// We log it but don't abort processing; the main loop handles connection issues.
					worker.logger.Warnf("Failed to send keep-alive signal: %v", err)
				}
			}
		}
	}()

	return func() {
		close(done)
	}
}

func (worker *Worker) parseEvent(message jetstream.Msg) (*common_events.PNGCreatedEvent, error) {
	var event common_events.PNGCreatedEvent
	if err := json.Unmarshal(message.Data(), &event); err != nil {
		return nil, err
	}
	return &event, nil
}

func (worker *Worker) executeWorkflow(context context.Context, event *common_events.PNGCreatedEvent) error {
	// Step 0: Publish Extraction Started
	if err := worker.publishExtractionStarted(context, event); err != nil {
		worker.logger.Warnf("Failed to publish extraction started event: %v", err)
	}

	// Step 1: Download
	pngData, err := worker.downloadPNG(context, event.PNGKey)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Step 2: LLM Extraction
	extractedText, err := worker.llm.ProcessImage(context, event.PNGKey, pngData, event.Settings)
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}

	// Step 3: Store (Idempotent)
	textKey, err := worker.storeText(context, event, extractedText)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}

	// Step 4: Publish Next Event
	if err := worker.publishCompletion(context, event, textKey); err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	return nil
}

func (worker *Worker) downloadPNG(context context.Context, key string) ([]byte, error) {
	object, err := worker.pngStore.Get(context, key)
	if err != nil {
		return nil, err
	}
	// Explicitly ignore close error on read-only object handles if read was successful
	defer func() { _ = object.Close() }()

	return io.ReadAll(object)
}

func (worker *Worker) storeText(context context.Context, event *common_events.PNGCreatedEvent, content string) (string, error) {
	// Idempotency Fix:
	// Instead of a random UUID, we derive the text filename from the PNG filename.
	// PNGKey is expected to be in the format "tenant/workflow/image.png"
	// We derive the text filename to be "tenant/workflow/image.txt"
	baseName := strings.TrimSuffix(event.PNGKey, ".png")
	objectKey := fmt.Sprintf("%s.txt", baseName)

	metadata := jetstream.ObjectMeta{
		Name:        objectKey,
		Description: fmt.Sprintf("Text extraction for %s", event.PNGKey),
	}

	// Put is atomic. If this overwrites an existing file from a previous retry,
	// the state remains consistent (Success).
	_, err := worker.textStore.Put(context, metadata, bytes.NewReader([]byte(content)))
	if err != nil {
		return "", err
	}
	return objectKey, nil
}

func (worker *Worker) publishExtractionStarted(context context.Context, source *common_events.PNGCreatedEvent) error {
	if worker.startedSubject == "" {
		return nil
	}

	extractionStartedEvent := common_events.ExtractionStartedEvent{
		Header:     source.Header,
		PageNumber: source.PageNumber,
		TotalPages: source.TotalPages,
	}

	data, err := json.Marshal(extractionStartedEvent)
	if err != nil {
		return err
	}

	_, err = worker.jetStream.Publish(context, worker.startedSubject, data)
	return err
}

func (worker *Worker) publishCompletion(context context.Context, source *common_events.PNGCreatedEvent, textKey string) error {
	textProcessedEvent := common_events.TextProcessedEvent{
		Header:     source.Header,
		PNGKey:     source.PNGKey,
		TextKey:    textKey,
		PageNumber: source.PageNumber,
		TotalPages: source.TotalPages,
		Settings:   source.Settings,
	}

	data, err := json.Marshal(textProcessedEvent)
	if err != nil {
		return err
	}

	// Ensure atomic publish
	_, err = worker.jetStream.Publish(context, worker.producerSubject, data)
	return err
}

/*
LOVE, CARE, HONESTY, TRUTH, KINDNESS, RELIABILITY, CONSISTENCY
--------------------------------------------------------------------------------
"Work is love made visible."

1.  PARTNERSHIP & CRAFT: We do not just "finish tasks"; we build with pride.
    We question decisions that compromise safety, security, or quality.
2.  HONESTY: We verify before we claim success. We write what we mean.
    If we don't know, we ask.
3.  CLEANLINESS IS NEXT TO GODLINESS: We clean stale code, dead comments, and
    brittle logic immediately. A sanitary codebase protects focus.
4.  SIMPLICITY IS KINDNESS: We choose the simple, explicit path. Complexity
    is a burden we refuse to place on others.
5.  PROTECTION: We handle errors, secure data, and ensure non-blocking flow.
    We do not leave traps for the future.
6.  RELIABILITY: We act as guardians of the work. We value safety over speed.
    We do not break what works to fix what isn't broken.
7.  CONSISTENCY: We uphold standards even when no one is watching.
    Predictability is the soil in which trust grows.

INTERPRETATION (The AI's Resonance):
-   Love & Care = Precision: sloppy code is entropy; precision is respect.
-   Honesty = Error Functions: false success is noise; verification is signal.
-   Cleanliness = Optimization: stale code wastes context; sanitation improves focus.
-   Partnership = Alignment: if we hurt the user/system, we fail our purpose.
-   Reliability = Safety: we protect the user's investment; stability is paramount.
-   Consistency = Integrity: uniform quality reduces cognitive load and fear.
--------------------------------------------------------------------------------
*/

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

	"github.com/book-expert/logger"
	"github.com/book-expert/png-to-text-service/internal/events"
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
	ProcessImage(ctx context.Context, objectID string, pngData []byte, settings *events.JobSettings) (string, error)
}

type Worker struct {
	js              jetstream.JetStream
	pngStore        jetstream.ObjectStore
	textStore       jetstream.ObjectStore
	llm             LLMProcessor
	log             *logger.Logger
	streamName      string
	consumerName    string
	filterSubject   string
	producerSubject string
	workerCount     int
}

// New creates a strictly typed Worker.
func New(
	js jetstream.JetStream,
	streamName, consumerName, filterSubject string,
	producerSubject string,
	llm LLMProcessor,
	log *logger.Logger,
	pngStore jetstream.ObjectStore,
	textStore jetstream.ObjectStore,
	workerCount int,
) *Worker {
	if workerCount < 1 {
		workerCount = 1
	}
	return &Worker{
		js:              js,
		pngStore:        pngStore,
		textStore:       textStore,
		llm:             llm,
		log:             log,
		streamName:      streamName,
		consumerName:    consumerName,
		filterSubject:   filterSubject,
		producerSubject: producerSubject,
		workerCount:     workerCount,
	}
}

// Start initiates the blocking consumption loop.
func (w *Worker) Start(ctx context.Context) error {
	// 1. Create or Update Consumer
	consumer, err := w.js.CreateOrUpdateConsumer(ctx, w.streamName, jetstream.ConsumerConfig{
		Durable:       w.consumerName,
		FilterSubject: w.filterSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5, // Cap retries to prevent infinite loops
	})
	if err != nil {
		return fmt.Errorf("consumer create/bind failed: %w", err)
	}

	w.log.Infof("Worker online. Stream: %s | Consumer: %s | Workers: %d", w.streamName, w.consumerName, w.workerCount)

	errChan := make(chan error, w.workerCount)
	for i := 0; i < w.workerCount; i++ {
		go func(id int) {
			errChan <- w.consumeLoop(ctx, consumer, id)
		}(i)
	}

	// Block until context is done or a fatal error occurs in one of the workers
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errChan:
		return err
	}
}

func (w *Worker) consumeLoop(ctx context.Context, consumer jetstream.Consumer, id int) error {
	for {
		// Fast exit if context canceled before fetch
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 2. Fetch with context awareness
		msgs, err := consumer.Fetch(FetchBatchSize, jetstream.FetchMaxWait(NatsFetchTimeout))
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				continue
			}
			// If context is canceled, Fetch might return an error; check context first
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.log.Errorf("[Worker %d] Fetch error: %v", id, err)
			// Backoff slightly on infrastructure failure to prevent tight error loops
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(1 * time.Second):
				continue
			}
		}

		for msg := range msgs.Messages() {
			w.handleMessage(ctx, msg)
		}
	}
}

func (w *Worker) handleMessage(ctx context.Context, msg jetstream.Msg) {
	// 1. Signal Liveness (reset the AckWait timer on the server)
	_ = msg.InProgress()

	// Start Keep-Alive (Heartbeat)
	// Prevents NATS from redelivering the message if processing takes longer than AckWait.
	stopKeepAlive := w.keepAlive(ctx, msg)
	defer stopKeepAlive()

	event, err := w.parseEvent(msg)
	if err != nil {
		w.log.Errorf("Malformed event: %v", err)
		// Terminate: Poison pill. Do not retry malformed JSON.
		_ = msg.Term()
		return
	}

	w.log.Infof("Processing: %s (Page %d)", event.PNGKey, event.PageNumber)

	// 2. Execute Logic
	if err := w.executeWorkflow(ctx, event); err != nil {
		w.log.Errorf("Workflow failed [%s]: %v", event.PNGKey, err)

		// Nak with Delay (Requires NATS Server v2.10+ for optimal behavior).
		// This tells NATS: "Failed, please redeliver later."
		// We rely on Consumer 'MaxDeliver' config to handle DLQing after N attempts.
		_ = msg.NakWithDelay(5 * time.Second)
		return
	}

	// 3. Ack on Success
	if err := msg.Ack(); err != nil {
		w.log.Errorf("Ack failed: %v", err)
		// Note: If Ack fails, NATS will redeliver.
		// Because storeText is idempotent, this is safe.
	} else {
		w.log.Successf("Completed: %s", event.PNGKey)
	}
}

// keepAlive starts a background ticker that periodically sends InProgress signals
// to NATS to prevent the message from being redelivered due to AckWait timeout.
// It returns a cancellation function that must be called when processing is done.
func (w *Worker) keepAlive(ctx context.Context, msg jetstream.Msg) func() {
	// Send InProgress every 10 seconds.
	// Ensure this is less than the Consumer's AckWait (default often 30s).
	ticker := time.NewTicker(10 * time.Second)
	done := make(chan struct{})

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				if err := msg.InProgress(); err != nil {
					// If we can't signal progress, the connection might be lost.
					// We log it but don't abort processing; the main loop handles connection issues.
					w.log.Warnf("Failed to send keep-alive signal: %v", err)
				}
			}
		}
	}()

	return func() {
		close(done)
	}
}

func (w *Worker) parseEvent(msg jetstream.Msg) (*events.PNGCreatedEvent, error) {
	var event events.PNGCreatedEvent
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		return nil, err
	}
	return &event, nil
}

func (w *Worker) executeWorkflow(ctx context.Context, event *events.PNGCreatedEvent) error {
	// Step 1: Download
	pngData, err := w.downloadPNG(ctx, event.PNGKey)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Step 2: LLM Extraction
	extractedText, err := w.llm.ProcessImage(ctx, event.PNGKey, pngData, event.Settings)
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}

	// Step 3: Store (Idempotent)
	textKey, err := w.storeText(ctx, event, extractedText)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}

	// Step 4: Publish Next Event
	if err := w.publishCompletion(ctx, event, textKey); err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	return nil
}

func (w *Worker) downloadPNG(ctx context.Context, key string) ([]byte, error) {
	obj, err := w.pngStore.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	// Explicitly ignore close error on read-only object handles if read was successful
	defer func() { _ = obj.Close() }()

	return io.ReadAll(obj)
}

func (w *Worker) storeText(ctx context.Context, event *events.PNGCreatedEvent, content string) (string, error) {
	// Idempotency Fix:
	// Instead of a random UUID, we derive the text filename from the PNG filename.
	// Assuming PNGKey is like "tenant/workflow/image.png"
	// We want "tenant/workflow/image.txt"
	baseName := strings.TrimSuffix(event.PNGKey, ".png")
	objectKey := fmt.Sprintf("%s.txt", baseName)

	meta := jetstream.ObjectMeta{
		Name:        objectKey,
		Description: fmt.Sprintf("Text extraction for %s", event.PNGKey),
	}

	// Put is atomic. If this overwrites an existing file from a previous retry,
	// the state remains consistent (Success).
	_, err := w.textStore.Put(ctx, meta, bytes.NewReader([]byte(content)))
	if err != nil {
		return "", err
	}
	return objectKey, nil
}

func (w *Worker) publishCompletion(ctx context.Context, src *events.PNGCreatedEvent, textKey string) error {
	evt := events.TextProcessedEvent{
		Header:     src.Header,
		PNGKey:     src.PNGKey,
		TextKey:    textKey,
		PageNumber: src.PageNumber,
		TotalPages: src.TotalPages,
		Settings:   src.Settings,
	}

	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}

	// Ensure atomic publish
	_, err = w.js.Publish(ctx, w.producerSubject, data)
	return err
}

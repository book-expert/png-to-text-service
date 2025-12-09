package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/book-expert/events"
	"github.com/book-expert/logger"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	natsFetchTimeout = 5 * time.Second
	dlqMaxRetries    = 3
	dlqBackoff       = 5 * time.Second
)

// LLMProcessor defines the interface for the processing logic.
type LLMProcessor interface {
	ProcessImage(ctx context.Context, objectID string, pngData []byte) (string, error)
}

// NatsWorker manages the NATS connection and message consumption.
type NatsWorker struct {
	jetstream         jetstream.JetStream
	pngStore          jetstream.ObjectStore
	textStore         jetstream.ObjectStore
	llm               LLMProcessor
	logger            *logger.Logger
	streamName        string
	subject           string
	consumer          string
	outputSubject     string
	deadLetterSubject string
}

// New creates a new NatsWorker.
func New(
	js jetstream.JetStream,
	streamName, subject, consumer, outputSubject, deadLetterSubject string,
	llm LLMProcessor,
	log *logger.Logger,
	pngStore jetstream.ObjectStore,
	textStore jetstream.ObjectStore,
) *NatsWorker {
	return &NatsWorker{
		jetstream:         js,
		pngStore:          pngStore,
		textStore:         textStore,
		streamName:        streamName,
		subject:           subject,
		consumer:          consumer,
		llm:               llm,
		logger:            log,
		outputSubject:     outputSubject,
		deadLetterSubject: deadLetterSubject,
	}
}

// Start begins the worker's message processing loop.
func (w *NatsWorker) Start(ctx context.Context) error {
	// Get the existing consumer
	consumer, err := w.jetstream.Consumer(ctx, w.streamName, w.consumer)
	if err != nil {
		return fmt.Errorf("get consumer: %w", err)
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(natsFetchTimeout))
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				continue
			}
			w.logger.Error("Fetch failed: %v", err)
			continue
		}

		for msg := range batch.Messages() {
			w.handleMessage(ctx, msg)
		}
	}
}

func (w *NatsWorker) handleMessage(ctx context.Context, msg jetstream.Msg) {
	var event events.PNGCreatedEvent
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		w.logger.Error("Unmarshal failed: %v", err)
		if ackErr := msg.Ack(); ackErr != nil {
			w.logger.Error("Ack failed: %v", ackErr)
		}
		return
	}

	w.logger.Info("Processing PNG: %s", event.PNGKey)
	
	if progErr := msg.InProgress(); progErr != nil {
		w.logger.Warn("InProgress failed: %v", progErr)
	}

	if err := w.process(ctx, &event); err != nil {
		w.logger.Error("Processing failed for %s: %v", event.PNGKey, err)
		w.handleFailure(ctx, msg)
		return
	}

	if err := msg.Ack(); err != nil {
		w.logger.Error("Ack failed: %v", err)
	} else {
		w.logger.Success("Completed: %s", event.PNGKey)
	}
}

func (w *NatsWorker) process(ctx context.Context, event *events.PNGCreatedEvent) error {
	// 1. Fetch PNG
	pngData, err := w.fetchPNG(ctx, event.PNGKey)
	if err != nil {
		return err
	}

	// 2. Process via LLM
	text, err := w.llm.ProcessImage(ctx, event.PNGKey, pngData)
	if err != nil {
		return err
	}

	// 3. Store Result
	textKey, err := w.storeText(ctx, event, text)
	if err != nil {
		return err
	}

	// 4. Publish Event
	return w.publishEvent(ctx, event, textKey)
}

func (w *NatsWorker) fetchPNG(ctx context.Context, key string) ([]byte, error) {
	obj, err := w.pngStore.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get png: %w", err)
	}
	defer func() {
		if closeErr := obj.Close(); closeErr != nil {
			w.logger.Warn("failed to close png object: %v", closeErr)
		}
	}()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read png: %w", err)
	}
	return data, nil
}

func (w *NatsWorker) storeText(ctx context.Context, event *events.PNGCreatedEvent, text string) (string, error) {
	key := fmt.Sprintf(
		"%s/%s/text_%s.txt",
		event.Header.TenantID,
		event.Header.WorkflowID,
		uuid.NewString(),
	)

	meta := jetstream.ObjectMeta{
		Name:        key,
		Description: fmt.Sprintf("Text for %s", event.PNGKey),
	}

	_, err := w.textStore.Put(ctx, meta, bytes.NewReader([]byte(text)))
	if err != nil {
		return "", fmt.Errorf("store text: %w", err)
	}
	return key, nil
}

func (w *NatsWorker) publishEvent(ctx context.Context, sourceEvent *events.PNGCreatedEvent, textKey string) error {
	evt := events.TextProcessedEvent{
		Header:     sourceEvent.Header,
		PNGKey:     sourceEvent.PNGKey,
		TextKey:    textKey,
		PageNumber: sourceEvent.PageNumber,
		TotalPages: sourceEvent.TotalPages,
		// TTS fields are left empty/default as per simplification requirements
	}

	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	_, err = w.jetstream.Publish(ctx, w.outputSubject, data)
	if err != nil {
		return fmt.Errorf("publish event: %w", err)
	}
	return nil
}

func (w *NatsWorker) handleFailure(ctx context.Context, msg jetstream.Msg) {
	if w.deadLetterSubject == "" {
		// Allow NATS redelivery if no DLQ is configured
		if nakErr := msg.Nak(); nakErr != nil {
			w.logger.Error("Nak failed: %v", nakErr)
		}
		return
	}

	for i := 0; i < dlqMaxRetries; i++ {
		if _, err := w.jetstream.Publish(ctx, w.deadLetterSubject, msg.Data()); err == nil {
			if ackErr := msg.Ack(); ackErr != nil {
				w.logger.Error("Ack after DLQ failed: %v", ackErr)
			}
			return
		}
		time.Sleep(dlqBackoff)
	}

	// If DLQ fails, NAK to retry later
	if nakErr := msg.Nak(); nakErr != nil {
		w.logger.Error("Nak failed after DLQ fail: %v", nakErr)
	}
}

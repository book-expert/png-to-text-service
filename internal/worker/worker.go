// Package worker provides a NATS worker for processing image-to-text tasks.
package worker

import (
	"bytes" // New import
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/book-expert/events"
	"github.com/book-expert/logger"
	"github.com/google/uuid" // New import
	"github.com/nats-io/nats.go"
)

const (
	// NatsConnectTimeoutSeconds defines the timeout for NATS connection attempts.
	NatsConnectTimeoutSeconds = 10
	// NatsMaxReconnectAttempts defines the maximum number of reconnect attempts for NATS.
	NatsMaxReconnectAttempts = 5
	// NatsFetchMaxWaitSeconds defines the maximum time to wait for messages during a fetch operation.
	NatsFetchMaxWaitSeconds = 5
)

// Pipeline defines the interface for the processing logic.
type Pipeline interface {
	Process(ctx context.Context, objectID string, data []byte) (string, error)
}

// TTSDefaults holds default parameters for downstream text-to-speech processing.
type TTSDefaults struct {
	Voice             string
	Seed              int
	NGL               int
	TopP              float64
	RepetitionPenalty float64
	Temperature       float64
}

// NatsWorker manages the NATS connection and message consumption.
type NatsWorker struct {
	jetstream         nats.JetStreamContext
	pngStore          nats.ObjectStore
	textStore         nats.ObjectStore // New field for TEXT_FILES object store
	pipeline          Pipeline
	nc                *nats.Conn
	logger            *logger.Logger
	streamName        string
	subject           string
	consumer          string
	outputSubject     string
	deadLetterSubject string
	ttsDefaults       TTSDefaults
}

// New creates a new NatsWorker.
func New(
	natsURL, streamName, subject, consumer, outputSubject, deadLetterSubject string,
	pipeline Pipeline,
	log *logger.Logger,
	pngStore nats.ObjectStore,
	textStore nats.ObjectStore, // New parameter
	ttsDefaults TTSDefaults,
) (*NatsWorker, error) {
	natsConn, err := nats.Connect(
		natsURL,
		nats.Timeout(NatsConnectTimeoutSeconds*time.Second),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(NatsMaxReconnectAttempts),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}

	log.Info("Connected to NATS server at %s", natsURL)

	jetstream, err := natsConn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get JetStream context: %w", err)
	}

	// Ensure the stream exists.
	_, streamInfoErr := jetstream.StreamInfo(streamName)
	if streamInfoErr != nil {
		return nil, fmt.Errorf("stream '%s' not found: %w", streamName, streamInfoErr)
	}

	log.Info("Found stream '%s'.", streamName)

	return &NatsWorker{
		nc:                natsConn,
		jetstream:         jetstream,
		pngStore:          pngStore,
		textStore:         textStore, // Initialize new field
		streamName:        streamName,
		subject:           subject,
		consumer:          consumer,
		pipeline:          pipeline,
		logger:            log,
		outputSubject:     outputSubject,
		deadLetterSubject: deadLetterSubject,
		ttsDefaults:       ttsDefaults,
	}, nil
}

// Run starts the worker's message processing loop.
func (w *NatsWorker) Run(ctx context.Context) error {
	sub, err := w.jetstream.PullSubscribe(
		w.subject,
		w.consumer,
		nats.BindStream(w.streamName),
	)
	if err != nil {
		return fmt.Errorf("pull subscribe: %w", err)
	}

	w.logger.Info("Consumer '%s' is ready.", w.consumer)
	w.logger.Info("Worker is running, listening for jobs on '%s'...", w.subject)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Context canceled, worker shutting down.")

			return nil
		default:
			msgs, err := sub.Fetch(1, nats.MaxWait(NatsFetchMaxWaitSeconds*time.Second))
			if err != nil {
				if errors.Is(err, nats.ErrTimeout) {
					continue // No messages, just loop again.
				}

				w.logger.Error("Fetch messages: %v", err)

				continue
			}

			if len(msgs) > 0 {
				w.handleMsg(ctx, msgs[0])
			}
		}
	}
}

func (w *NatsWorker) handleMsg(ctx context.Context, msg *nats.Msg) {
	// handleMsg processes a single NATS message.
	startTime := time.Now()

	metaErr := w.checkMessageMetadata(msg)
	if metaErr != nil {
		w.handleMessageMetadataError(msg, metaErr)

		return
	}

	var event events.PNGCreatedEvent

	err := json.Unmarshal(msg.Data, &event)
	if err != nil {
		w.handleMessageMetadataError(
			msg,
			fmt.Errorf("failed to unmarshal PNGCreatedEvent: %w", err),
		)

		return
	}

	w.logger.Info("Processing job for object: %s", event.PNGKey)

	textKey, processErr := w.processAndPublishText(ctx, msg, &event) // Get textKey here
	if processErr != nil {
		w.handleMessagePipelineError(msg, event.PNGKey, processErr)

		return
	}

	w.logger.Success(
		"Processed %s and published TextProcessedEvent with TextKey %s in %s",
		event.PNGKey, textKey, time.Since(startTime), // Use textKey here
	)

	ackErr := msg.Ack()
	if ackErr != nil {
		w.logger.Error(
			"failed to acknowledge successful message for object %s: %v",
			event.PNGKey,
			ackErr,
		)
	}
}

func (w *NatsWorker) processAndPublishText(
	ctx context.Context,
	_ *nats.Msg,
	event *events.PNGCreatedEvent,
) (string, error) {
	pngBytes, readErr := w.fetchPNGBytes(event)
	if readErr != nil {
		return "", readErr
	}

	processedText, processErr := w.pipeline.Process(ctx, event.PNGKey, pngBytes)
	if processErr != nil {
		return "", fmt.Errorf("pipeline failed for '%s': %w", event.PNGKey, processErr)
	}

	textObjectKey, storeErr := w.storeProcessedText(event, processedText)
	if storeErr != nil {
		return "", storeErr
	}

	publishErr := w.publishTextProcessedEvent(event, textObjectKey)
	if publishErr != nil {
		return "", publishErr
	}

	return textObjectKey, nil
}

func (w *NatsWorker) fetchPNGBytes(event *events.PNGCreatedEvent) ([]byte, error) {
	pngDataReader, objectStoreErr := w.pngStore.Get(event.PNGKey)
	if objectStoreErr != nil {
		return nil, fmt.Errorf("failed to get PNG '%s' from object store: %w", event.PNGKey, objectStoreErr)
	}

	defer func() {
		closeErr := pngDataReader.Close()
		if closeErr != nil {
			w.logger.Error("failed to close pngDataReader: %v", closeErr)
		}
	}()

	pngDataBytes, readErr := io.ReadAll(pngDataReader)
	if readErr != nil {
		return nil, fmt.Errorf("failed to read PNG data for '%s': %w", event.PNGKey, readErr)
	}

	return pngDataBytes, nil
}

func generateTextObjectKey(event *events.PNGCreatedEvent) string {
	return fmt.Sprintf(
		"%s/%s/text_%s.txt",
		event.Header.TenantID,
		event.Header.WorkflowID,
		uuid.NewString(),
	)
}

func (w *NatsWorker) storeProcessedText(event *events.PNGCreatedEvent, processedText string) (string, error) {
	textObjectKey := generateTextObjectKey(event)
	objectDescription := fmt.Sprintf(
		"Processed text for PNG: %s, Page: %d/%d",
		event.PNGKey,
		event.PageNumber,
		event.TotalPages,
	)

	objectMeta := nats.ObjectMeta{
		Name:        textObjectKey,
		Description: objectDescription,
		Headers:     nil,
		Metadata:    nil,
		Opts:        nil,
	}

	_, uploadErr := w.textStore.Put(&objectMeta, bytes.NewReader([]byte(processedText)))
	if uploadErr != nil {
		return "", fmt.Errorf("failed to upload processed text to object store: %w", uploadErr)
	}

	return textObjectKey, nil
}

func (w *NatsWorker) publishTextProcessedEvent(event *events.PNGCreatedEvent, textObjectKey string) error {
	defaults := w.ttsDefaults

	processedEvent := events.TextProcessedEvent{
		Header:            event.Header,
		PNGKey:            event.PNGKey,
		TextKey:           textObjectKey,
		PageNumber:        event.PageNumber,
		TotalPages:        event.TotalPages,
		Voice:             defaults.Voice,
		Seed:              defaults.Seed,
		NGL:               defaults.NGL,
		TopP:              defaults.TopP,
		RepetitionPenalty: defaults.RepetitionPenalty,
		Temperature:       defaults.Temperature,
	}

	eventJSON, marshalErr := json.Marshal(processedEvent)
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal TextProcessedEvent: %w", marshalErr)
	}

	_, publishErr := w.jetstream.Publish(w.outputSubject, eventJSON)
	if publishErr != nil {
		return fmt.Errorf("failed to publish TextProcessedEvent: %w", publishErr)
	}

	return nil
}

func (w *NatsWorker) checkMessageMetadata(msg *nats.Msg) error {
	_, metaErr := msg.Metadata()
	if metaErr != nil {
		return fmt.Errorf("failed to get message metadata: %w", metaErr)
	}

	return nil
}

func (w *NatsWorker) handleMessageMetadataError(msg *nats.Msg, metaErr error) {
	w.logger.Error(
		"Failed to get message metadata: %v. Acknowledging to discard.",
		metaErr,
	)

	ackErr := msg.Ack()
	if ackErr != nil {
		w.logger.Error("failed to acknowledge message: %v", ackErr)
	}
}

func (w *NatsWorker) handleMessagePipelineError(msg *nats.Msg, objectID string, pipelineErr error) {
	w.logger.Error("Pipeline failed for '%s': %v", objectID, pipelineErr)

	_, pubErr := w.jetstream.Publish(w.deadLetterSubject, msg.Data)
	if pubErr != nil {
		w.logger.Error(
			"Failed to publish message to dead-letter subject for object %s: %v",
			objectID,
			pubErr,
		)
	}

	ackErr := msg.Ack()
	if ackErr != nil {
		w.logger.Error(
			"failed to acknowledge failed message for object %s: %v",
			objectID,
			ackErr,
		)
	}
}

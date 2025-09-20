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
}

// New creates a new NatsWorker.
func New(
	natsURL, streamName, subject, consumer, outputSubject, deadLetterSubject string,
	pipeline Pipeline,
	log *logger.Logger,
	pngStore nats.ObjectStore,
	textStore nats.ObjectStore, // New parameter
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
	pngData, err := w.pngStore.Get(event.PNGKey)
	if err != nil {
		return "", fmt.Errorf("failed to get PNG '%s' from object store: %w", event.PNGKey, err)
	}

	defer func() {
		closeErr := pngData.Close()
		if closeErr != nil {
			w.logger.Error("failed to close pngData: %v", closeErr)
		}
	}()

	pngBytes, err := io.ReadAll(pngData)
	if err != nil {
		return "", fmt.Errorf("failed to read PNG data for '%s': %w", event.PNGKey, err)
	}

	text, err := w.pipeline.Process(ctx, event.PNGKey, pngBytes)
	if err != nil {
		return "", fmt.Errorf("pipeline failed for '%s': %w", event.PNGKey, err)
	}

	// Generate a unique key for the text object
	textKey := fmt.Sprintf("%s/%s/text_%s.txt", event.Header.TenantID, event.Header.WorkflowID, uuid.NewString())

	// Upload the text to the object store
	_, uploadErr := w.textStore.Put(&nats.ObjectMeta{
		Name: textKey,
		Description: fmt.Sprintf("Processed text for PNG: %s, Page: %d/%d",
			event.PNGKey, event.PageNumber, event.TotalPages),
		Headers:  nil,
		Metadata: nil,
		Opts:     nil,
	}, bytes.NewReader([]byte(text)))
	if uploadErr != nil {
		return "", fmt.Errorf("failed to upload processed text to object store: %w", uploadErr)
	}

	// Construct and publish the TextProcessedEvent
	textEvent := events.TextProcessedEvent{
		Header:            event.Header,
		PNGKey:            event.PNGKey,
		TextKey:           textKey, // Use the key from the uploaded object
		PageNumber:        event.PageNumber,
		TotalPages:        event.TotalPages,
		Voice:             "",  // Initialize with zero value
		Seed:              0,   // Initialize with zero value
		NGL:               0,   // Initialize with zero value
		TopP:              0.0, // Initialize with zero value
		RepetitionPenalty: 0.0, // Initialize with zero value
		Temperature:       0.0, // Initialize with zero value
	}

	eventJSON, marshalErr := json.Marshal(textEvent)
	if marshalErr != nil {
		return "", fmt.Errorf("failed to marshal TextProcessedEvent: %w", marshalErr)
	}

	_, publishErr := w.jetstream.Publish(w.outputSubject, eventJSON)
	if publishErr != nil {
		return "", fmt.Errorf("failed to publish TextProcessedEvent: %w", publishErr)
	}

	return textKey, nil // Return the textKey
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

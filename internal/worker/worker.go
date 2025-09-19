// Package worker provides a NATS worker for processing image-to-text tasks.
package worker

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/book-expert/logger"
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

// handleMsg processes a single NATS message.
func (w *NatsWorker) handleMsg(ctx context.Context, msg *nats.Msg) {
	startTime := time.Now()

	meta, metaErr := msg.Metadata()
	if metaErr != nil {
		w.handleMessageMetadataError(msg, metaErr)

		return
	}

	var event events.PNGCreatedEvent
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		w.handleProcessingError(msg, fmt.Errorf("failed to unmarshal PNGCreatedEvent: %w", err))
		return
	}

	w.logger.Info("Processing job for object: %s", event.PNGKey)

	pngData, err := w.pngStore.Get(ctx, event.PNGKey)
	if err != nil {
		w.handleProcessingError(msg, fmt.Errorf("failed to get PNG '%s' from object store: %w", event.PNGKey, err))
		return
	}
	defer pngData.Close()

	pngBytes, err := io.ReadAll(pngData)
	if err != nil {
		w.handleProcessingError(msg, fmt.Errorf("failed to read PNG data for '%s': %w", event.PNGKey, err))
		return
	}

	text, err := w.pipeline.Process(ctx, event.PNGKey, pngBytes)

	_, publishErr := w.jetstream.Publish(w.outputSubject, []byte(processedText))
	if publishErr != nil {
		w.handleMessagePublishError(msg, objectID, publishErr)

		return
	}

	w.logger.Success("Processed %s in %s", objectID, time.Since(startTime))

	ackErr := msg.Ack()
	if ackErr != nil {
		w.logger.Error(
			"failed to acknowledge successful message for object %s: %v",
			objectID,
			ackErr,
		)
	}
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

func (w *NatsWorker) handleMessagePublishError(msg *nats.Msg, objectID string, publishErr error) {
	w.logger.Error(
		"Failed to publish processed text for object %s: %v",
		objectID,
		publishErr,
	)

	ackErr := msg.Ack()
	if ackErr != nil {
		w.logger.Error(
			"failed to acknowledge failed message for object %s: %v",
			objectID,
			ackErr,
		)
	}
}

func (w *NatsWorker) handleEmptyMessage(msg *nats.Msg, objectID string) {
	w.logger.Error("Received empty message for object %s. Acknowledging to discard.", objectID)

	ackErr := msg.Ack()
	if ackErr != nil {
		w.logger.Error("failed to acknowledge empty message for object %s: %v", objectID, ackErr)
	}
}

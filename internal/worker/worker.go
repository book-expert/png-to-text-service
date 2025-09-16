// ./internal/worker/worker.go
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

// Pipeline defines the interface for the processing logic.
type Pipeline interface {
	Process(ctx context.Context, objectID string, data []byte) (string, error)
}

// NatsWorker manages the NATS connection and message consumption.
type NatsWorker struct {
	js                nats.JetStreamContext
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
	nc, err := nats.Connect(
		natsURL,
		nats.Timeout(10*time.Second),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(5),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}

	log.Info("Connected to NATS server at %s", natsURL)

	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get JetStream context: %w", err)
	}

	// Ensure the stream exists.
	if _, err := js.StreamInfo(streamName); err != nil {
		return nil, fmt.Errorf("stream '%s' not found: %w", streamName, err)
	}

	log.Info("Found stream '%s'.", streamName)

	return &NatsWorker{
		nc:                nc,
		js:                js,
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
	sub, err := w.js.PullSubscribe(
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
			msgs, err := sub.Fetch(1, nats.MaxWait(5*time.Second))
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

	meta, err := msg.Metadata()
	if err != nil {
		w.logger.Error(
			"Failed to get message metadata: %v. Acknowledging to discard.",
			err,
		)
		if err := msg.Ack(); err != nil {
			w.logger.Error("failed to acknowledge message: %v", err)
		}

		return
	}

	objectID := "seq-" + strconv.FormatUint(meta.Sequence.Stream, 10)

	pngData := msg.Data
	if len(pngData) == 0 {
		w.logger.Warn(
			"Received empty message for object %s. Acknowledging to discard.",
			objectID,
		)
		if err := msg.Ack(); err != nil {
			w.logger.Error(
				"failed to acknowledge empty message for object %s: %v",
				objectID,
				err,
			)
		}

		return
	}

	processedText, err := w.pipeline.Process(ctx, objectID, pngData)
	if err != nil {
		w.logger.Error("Pipeline failed for '%s': %v", objectID, err)
		// Publish to dead-letter subject
		if _, pubErr := w.js.Publish(w.deadLetterSubject, msg.Data); pubErr != nil {
			w.logger.Error(
				"Failed to publish message to dead-letter subject for object %s: %v",
				objectID,
				pubErr,
			)
		}

		if ackErr := msg.Ack(); ackErr != nil {
			w.logger.Error(
				"failed to acknowledge failed message for object %s: %v",
				objectID,
				ackErr,
			)
		}

		return
	}

	// Publish the processed text to the output subject.
	if _, err := w.js.Publish(w.outputSubject, []byte(processedText)); err != nil {
		w.logger.Error(
			"Failed to publish processed text for object %s: %v",
			objectID,
			err,
		)
		// We still acknowledge the message to prevent it from being re-processed
		// endlessly.
		// A more advanced system might use a dead-letter queue here.
		if err := msg.Ack(); err != nil {
			w.logger.Error(
				"failed to acknowledge failed message for object %s: %v",
				objectID,
				err,
			)
		}
		return
	}

	w.logger.Success("Processed %s in %s", objectID, time.Since(startTime))
	if err := msg.Ack(); err != nil {
		w.logger.Error(
			"failed to acknowledge successful message for object %s: %v",
			objectID,
			err,
		)
	}
}

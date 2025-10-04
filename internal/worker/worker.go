// Package worker manages NATS consumption, orchestrates OCR and augmentation,
// and publishes results and failures to appropriate subjects and stores.
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

	"github.com/book-expert/events"
	"github.com/book-expert/logger"
	"github.com/book-expert/png-to-text-service/internal/augment"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// NatsConnectTimeoutSeconds defines the timeout for NATS connection attempts.
	NatsConnectTimeoutSeconds = 10
	// NatsMaxReconnectAttempts defines the maximum number of reconnect attempts for NATS.
	NatsMaxReconnectAttempts = 5
	// NatsFetchMaxWaitSeconds defines the maximum time to wait for messages during a fetch operation.
	NatsFetchMaxWaitSeconds = 5

	// DLQPublishMaxRetries defines how many times to retry publishing to the dead-letter subject on failure.
	DLQPublishMaxRetries = 3
	// DLQPublishBackoffSeconds defines backoff between DLQ publish attempts.
	DLQPublishBackoffSeconds = 5
)

// Pipeline defines the interface for the processing logic.
type Pipeline interface {
	Process(ctx context.Context, objectID string, data []byte, overrides *augment.AugmentationOptions) (string, error)
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
	jetstream         jetstream.JetStream
	pngStore          jetstream.ObjectStore
	textStore         jetstream.ObjectStore // New field for TEXT_FILES object store
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
    requestContext context.Context,
    natsURL, streamName, subject, consumer, outputSubject, deadLetterSubject string,
    pipeline Pipeline,
    log *logger.Logger,
    pngStore jetstream.ObjectStore,
    textStore jetstream.ObjectStore, // New parameter
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

    jetstream, newJetstreamError := jetstream.New(natsConn)
    if newJetstreamError != nil {
        return nil, fmt.Errorf("get JetStream context: %w", newJetstreamError)
    }

    // Ensure the stream exists.
    stream, getStreamError := jetstream.Stream(requestContext, streamName)
    if getStreamError != nil {
        return nil, fmt.Errorf("failed to get stream '%s': %w", streamName, getStreamError)
    }

    _, streamInfoError := stream.Info(requestContext)
    if streamInfoError != nil {
        return nil, fmt.Errorf("failed to get stream info for '%s': %w", streamName, streamInfoError)
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
func (w *NatsWorker) Run(requestContext context.Context) error {
	consumer, getConsumerError := w.jetstream.Consumer(requestContext, w.streamName, w.consumer)
	if getConsumerError != nil {
		return fmt.Errorf("failed to get consumer: %w", getConsumerError)
	}

	w.logger.Info("Consumer '%s' is ready.", w.consumer)
	w.logger.Info("Worker is running, listening for jobs on '%s'...", w.subject)

	for {
		select {
		case <-requestContext.Done():
			w.logger.Info("Context canceled, worker shutting down.")

			return nil
		default:
			batch, fetchBatchError := consumer.Fetch(1, jetstream.FetchMaxWait(NatsFetchMaxWaitSeconds*time.Second))
			if fetchBatchError != nil {
				if errors.Is(fetchBatchError, nats.ErrTimeout) {
					continue // No messages, just loop again.
				}

				w.logger.Error("Fetch messages: %v", fetchBatchError)

				continue
			}

			for msg := range batch.Messages() {
				w.handleMsg(requestContext, msg)
			}
		}
	}
}

func (w *NatsWorker) handleMsg(requestContext context.Context, msg jetstream.Msg) {
	// handleMsg processes a single NATS message.
	startTime := time.Now()

	checkMetadataError := w.checkMessageMetadata(msg)
	if checkMetadataError != nil {
		w.handleMessageMetadataError(msg, checkMetadataError)

		return
	}

	var event events.PNGCreatedEvent

	unmarshalEventError := json.Unmarshal(msg.Data(), &event)
	if unmarshalEventError != nil {
		w.handleMessageMetadataError(
			msg,
			fmt.Errorf("failed to unmarshal PNGCreatedEvent: %w", unmarshalEventError),
		)

		return
	}

	w.logger.Info("Processing job for object: %s", event.PNGKey)

	textKey, processAndPublishError := w.processAndPublishText(requestContext, &event)
	if processAndPublishError != nil {
		w.handleMessagePipelineError(requestContext, msg, event.PNGKey, processAndPublishError)

		return
	}

	w.logger.Success(
		"Processed %s and published TextProcessedEvent with TextKey %s in %s",
		event.PNGKey, textKey, time.Since(startTime), // Use textKey here
	)

	acknowledgeError := msg.Ack()
	if acknowledgeError != nil {
		w.logger.Error(
			"failed to acknowledge successful message for object %s: %v",
			event.PNGKey,
			acknowledgeError,
		)
	}
}

func (w *NatsWorker) processAndPublishText(
	requestContext context.Context,
	event *events.PNGCreatedEvent,
) (string, error) {
	pngBytes, fetchPNGError := w.fetchPNGBytes(requestContext, event)
	if fetchPNGError != nil {
		return "", fetchPNGError
	}

	options := buildAugmentationOptions(event.Augmentation)

	processedText, processPipelineError := w.pipeline.Process(requestContext, event.PNGKey, pngBytes, options)
	if processPipelineError != nil {
		return "", fmt.Errorf("pipeline failed for '%s': %w", event.PNGKey, processPipelineError)
	}

	textObjectKey, storeProcessedError := w.storeProcessedText(requestContext, event, processedText)
	if storeProcessedError != nil {
		return "", storeProcessedError
	}

	publishProcessedEventError := w.publishTextProcessedEvent(requestContext, event, textObjectKey)
	if publishProcessedEventError != nil {
		return "", publishProcessedEventError
	}

	return textObjectKey, nil
}

func (w *NatsWorker) fetchPNGBytes(requestContext context.Context, event *events.PNGCreatedEvent) ([]byte, error) {
	pngDataReader, getObjectError := w.pngStore.Get(requestContext, event.PNGKey)
	if getObjectError != nil {
		return nil, fmt.Errorf("failed to get PNG '%s' from object store: %w", event.PNGKey, getObjectError)
	}

	defer func() {
		closeErr := pngDataReader.Close()
		if closeErr != nil {
			w.logger.Error("failed to close pngDataReader: %v", closeErr)
		}
	}()

	pngDataBytes, readPNGError := io.ReadAll(pngDataReader)
	if readPNGError != nil {
		return nil, fmt.Errorf("failed to read PNG data for '%s': %w", event.PNGKey, readPNGError)
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

func (w *NatsWorker) storeProcessedText(
	requestContext context.Context,
	event *events.PNGCreatedEvent,
	processedText string,
) (string, error) {
	textObjectKey := generateTextObjectKey(event)
	objectDescription := fmt.Sprintf(
		"Processed text for PNG: %s, Page: %d/%d",
		event.PNGKey,
		event.PageNumber,
		event.TotalPages,
	)

	objectMeta := jetstream.ObjectMeta{
		Name:        textObjectKey,
		Description: objectDescription,
		Headers:     nil,
		Metadata:    nil,
		Opts:        nil,
	}

	_, putObjectError := w.textStore.Put(requestContext, objectMeta, bytes.NewReader([]byte(processedText)))
	if putObjectError != nil {
		return "", fmt.Errorf("failed to upload processed text to object store: %w", putObjectError)
	}

	return textObjectKey, nil
}

func (w *NatsWorker) publishTextProcessedEvent(
	requestContext context.Context,
	event *events.PNGCreatedEvent,
	textObjectKey string,
) error {
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

	eventJSON, marshalEventError := json.Marshal(processedEvent)
	if marshalEventError != nil {
		return fmt.Errorf("failed to marshal TextProcessedEvent: %w", marshalEventError)
	}

	_, publishEventError := w.jetstream.Publish(requestContext, w.outputSubject, eventJSON)
	if publishEventError != nil {
		return fmt.Errorf("failed to publish TextProcessedEvent: %w", publishEventError)
	}

	return nil
}

func buildAugmentationOptions(
	prefs *events.AugmentationPreferences,
) *augment.AugmentationOptions {
	if prefs == nil {
		return nil
	}

	commentaryInstructions := strings.TrimSpace(prefs.Commentary.CustomInstructions)
	summaryInstructions := strings.TrimSpace(prefs.Summary.CustomInstructions)

	placement := sanitizeSummaryPlacement(prefs.Summary.Placement)

	return &augment.AugmentationOptions{
		Parameters: nil,
		Commentary: augment.AugmentationCommentaryOptions{
			Enabled:         prefs.Commentary.Enabled,
			CustomAdditions: commentaryInstructions,
		},
		Summary: augment.AugmentationSummaryOptions{
			Enabled:         prefs.Summary.Enabled,
			Placement:       placement,
			CustomAdditions: summaryInstructions,
		},
	}
}

func sanitizeSummaryPlacement(placement events.SummaryPlacement) augment.SummaryPlacement {
	switch placement {
	case events.SummaryPlacementTop,
		events.SummaryPlacementBottom:
		return placement
	default:
		return events.SummaryPlacementBottom
	}
}

func (w *NatsWorker) checkMessageMetadata(msg jetstream.Msg) error {
	_, metaErr := msg.Metadata()
	if metaErr != nil {
		return fmt.Errorf("failed to get message metadata: %w", metaErr)
	}

	return nil
}

func (w *NatsWorker) handleMessageMetadataError(msg jetstream.Msg, metaErr error) {
	w.logger.Error(
		"Failed to get message metadata: %v. Acknowledging to discard.",
		metaErr,
	)

	ackErr := msg.Ack()
	if ackErr != nil {
		w.logger.Error("failed to acknowledge message: %v", ackErr)
	}
}

func (w *NatsWorker) handleMessagePipelineError(
	requestContext context.Context,
	msg jetstream.Msg,
	objectID string,
	pipelineErr error,
) {
	w.logger.Error("Pipeline failed for '%s': %v", objectID, pipelineErr)

	// Dead-letter subject is required by blueprint. If missing, do not ack to allow redelivery.
	if strings.TrimSpace(w.deadLetterSubject) == "" {
		w.logger.Error("Dead-letter subject not configured; message will be redelivered for object %s", objectID)
		// Intentionally do not Ack; allow redelivery based on consumer configuration.
		return
	}

	// Attempt DLQ publish with bounded retries and backoff.
	var lastPublishError error

	for attempt := 1; attempt <= DLQPublishMaxRetries; attempt++ {
		_, publishDLQError := w.jetstream.Publish(requestContext, w.deadLetterSubject, msg.Data())
		if publishDLQError == nil {
			// DLQ publish succeeded; acknowledge the original message.
			acknowledgeError := msg.Ack()
			if acknowledgeError != nil {
				w.logger.Error(
					"failed to acknowledge failed message for object %s after DLQ publish: %v",
					objectID,
					acknowledgeError,
				)
			}

			return
		}

		lastPublishError = publishDLQError
		w.logger.Error(
			"DLQ publish attempt %d/%d failed for object %s: %v",
			attempt,
			DLQPublishMaxRetries,
			objectID,
			publishDLQError,
		)

		time.Sleep(time.Duration(DLQPublishBackoffSeconds) * time.Second)
	}

	// Exhausted retries; do not Ack to avoid message loss. Allow redelivery.
	w.logger.Error(
		"DLQ publish exhausted retries for object %s; preserving message for redelivery: %v",
		objectID,
		lastPublishError,
	)
}

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

	jetstream, err := jetstream.New(natsConn)
	if err != nil {
		return nil, fmt.Errorf("get JetStream context: %w", err)
	}

	// Ensure the stream exists.
	ctx := context.Background()

	stream, err := jetstream.Stream(ctx, streamName)
	if err != nil {
		return nil, fmt.Errorf("failed to get stream '%s': %w", streamName, err)
	}

	_, err = stream.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get stream info for '%s': %w", streamName, err)
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
	consumer, err := w.jetstream.Consumer(ctx, w.streamName, w.consumer)
	if err != nil {
		return fmt.Errorf("failed to get consumer: %w", err)
	}

	w.logger.Info("Consumer '%s' is ready.", w.consumer)
	w.logger.Info("Worker is running, listening for jobs on '%s'...", w.subject)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Context canceled, worker shutting down.")

			return nil
		default:
			batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(NatsFetchMaxWaitSeconds*time.Second))
			if err != nil {
				if errors.Is(err, nats.ErrTimeout) {
					continue // No messages, just loop again.
				}

				w.logger.Error("Fetch messages: %v", err)

				continue
			}

			for msg := range batch.Messages() {
				w.handleMsg(ctx, msg)
			}
		}
	}
}

func (w *NatsWorker) handleMsg(ctx context.Context, msg jetstream.Msg) {
	// handleMsg processes a single NATS message.
	startTime := time.Now()

	metaErr := w.checkMessageMetadata(msg)
	if metaErr != nil {
		w.handleMessageMetadataError(msg, metaErr)

		return
	}

	var event events.PNGCreatedEvent

	err := json.Unmarshal(msg.Data(), &event)
	if err != nil {
		w.handleMessageMetadataError(
			msg,
			fmt.Errorf("failed to unmarshal PNGCreatedEvent: %w", err),
		)

		return
	}

	w.logger.Info("Processing job for object: %s", event.PNGKey)

	textKey, processErr := w.processAndPublishText(ctx, &event) // Get textKey here
	if processErr != nil {
		w.handleMessagePipelineError(ctx, msg, event.PNGKey, processErr)

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
	event *events.PNGCreatedEvent,
) (string, error) {
	pngBytes, readErr := w.fetchPNGBytes(ctx, event)
	if readErr != nil {
		return "", readErr
	}

	options := buildAugmentationOptions(event.Augmentation)

	processedText, processErr := w.pipeline.Process(ctx, event.PNGKey, pngBytes, options)
	if processErr != nil {
		return "", fmt.Errorf("pipeline failed for '%s': %w", event.PNGKey, processErr)
	}

	textObjectKey, storeErr := w.storeProcessedText(ctx, event, processedText)
	if storeErr != nil {
		return "", storeErr
	}

	publishErr := w.publishTextProcessedEvent(ctx, event, textObjectKey)
	if publishErr != nil {
		return "", publishErr
	}

	return textObjectKey, nil
}

func (w *NatsWorker) fetchPNGBytes(ctx context.Context, event *events.PNGCreatedEvent) ([]byte, error) {
	pngDataReader, objectStoreErr := w.pngStore.Get(ctx, event.PNGKey)
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

func (w *NatsWorker) storeProcessedText(
	ctx context.Context,
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

	_, uploadErr := w.textStore.Put(ctx, objectMeta, bytes.NewReader([]byte(processedText)))
	if uploadErr != nil {
		return "", fmt.Errorf("failed to upload processed text to object store: %w", uploadErr)
	}

	return textObjectKey, nil
}

func (w *NatsWorker) publishTextProcessedEvent(
	ctx context.Context,
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

	eventJSON, marshalErr := json.Marshal(processedEvent)
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal TextProcessedEvent: %w", marshalErr)
	}

	_, publishErr := w.jetstream.Publish(ctx, w.outputSubject, eventJSON)
	if publishErr != nil {
		return fmt.Errorf("failed to publish TextProcessedEvent: %w", publishErr)
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
    ctx context.Context,
    msg jetstream.Msg,
    objectID string,
    pipelineErr error,
) {
	w.logger.Error("Pipeline failed for '%s': %v", objectID, pipelineErr)

	_, pubErr := w.jetstream.Publish(ctx, w.deadLetterSubject, msg.Data())
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

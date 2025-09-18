// Package worker_test contains tests for the NATS worker.
package worker_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/book-expert/logger"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/book-expert/png-to-text-service/internal/config"
	"github.com/book-expert/png-to-text-service/internal/worker"
)

var (
	errPipelineError = errors.New("pipeline error")
)

// mockPipeline is a mock implementation of the worker.Pipeline for testing.
type mockPipeline struct {
	ProcessFunc func(ctx context.Context, objectID string, data []byte) (string, error)
}

func (m *mockPipeline) Process(
	ctx context.Context,
	objectID string,
	data []byte,
) (string, error) {
	return m.ProcessFunc(ctx, objectID, data)
}

func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(t.TempDir(), "test.log")
	require.NoError(t, err)

	return log
}

// newTestConfig is a helper that returns a valid, fully-populated config struct.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()

	return &config.Config{
		Project: config.Project{
			Name:        "test-project",
			Version:     "1.0.0",
			Description: "Test Description",
		},
		Paths: config.PathsConfig{
			BaseLogsDir: t.TempDir(),
		},
		NATS: config.NATSConfig{
			URL:                      "nats://127.0.0.1:4222",
			PDFStreamName:            "PDF_JOBS",
			PDFConsumerName:          "pdf-workers",
			PDFCreatedSubject:        "pdf.created",
			PDFObjectStoreBucket:     "PDF_FILES",
			PNGStreamName:            "PNG_PROCESSING",
			PNGConsumerName:          "png-text-workers",
			PNGCreatedSubject:        "png.created",
			PNGObjectStoreBucket:     "PNG_FILES",
			TextObjectStoreBucket:    "TEXT_FILES",
			TTSStreamName:            "TTS_JOBS",
			TTSConsumerName:          "tts-workers",
			TextProcessedSubject:     "text.processed",
			AudioChunkCreatedSubject: "audio.chunk.created",
			AudioObjectStoreBucket:   "AUDIO_FILES",
			TextStreamName:           "TTS_JOBS",
			DeadLetterSubject:        "",
		},
		PNGToTextService: config.PNGToTextServiceConfig{
			Tesseract: config.Tesseract{
				Language:       "eng",
				OEM:            3,
				PSM:            3,
				DPI:            300,
				TimeoutSeconds: 60,
			},
			Gemini: config.Gemini{
				APIKeyVariable:    "TEST_GEMINI_API_KEY",
				Models:            []string{"gemini-pro"},
				MaxRetries:        3,
				RetryDelaySeconds: 5,
				TimeoutSeconds:    60,
				Temperature:       0.5,
				TopK:              40,
				TopP:              0.9,
				MaxTokens:         2048,
			},
			Logging: config.Logging{
				Level:                "info",
				Dir:                  t.TempDir(),
				EnableFileLogging:    true,
				EnableConsoleLogging: true,
			},
			Augmentation: config.Augmentation{
				Type:             "commentary",
				CustomPrompt:     "",
				UsePromptBuilder: true,
			},
			Prompts: config.Prompts{
				Augmentation: "Test prompt",
			},
		},
	}
}

func RunServerOnPort(t *testing.T, port int) (*server.Server, string) {
	t.Helper()

	opts := &server.Options{
		Port:      port,
		JetStream: true,
		StoreDir:  t.TempDir(),
	}
	natsServer, err := server.NewServer(opts)
	require.NoError(t, err)

	natsServer.Start()

	if !natsServer.ReadyForConnections(4 * time.Second) {
		t.Fatal("NATS server did not start")
	}

	return natsServer, natsServer.ClientURL()
}

func setupNatsTest(t *testing.T, cfg *config.Config) (*nats.Conn, nats.JetStreamContext) {
	t.Helper()

	natsServer, natsURL := RunServerOnPort(t, -1)
	t.Cleanup(natsServer.Shutdown)

	natsConn, err := nats.Connect(
		natsURL,
		nats.ReconnectWait(100*time.Millisecond),
		nats.MaxReconnects(10),
	)
	require.NoError(t, err)
	t.Cleanup(natsConn.Close)

	jetstream, err := natsConn.JetStream()
	require.NoError(t, err)

	_, err = jetstream.AddStream(&nats.StreamConfig{
		Name:      cfg.NATS.PNGStreamName,
		Subjects:  []string{cfg.NATS.PNGCreatedSubject, cfg.NATS.TextProcessedSubject, cfg.NATS.DeadLetterSubject},
		Retention: nats.LimitsPolicy,
		Storage:   nats.FileStorage,
	})
	require.NoError(t, err)

	_, err = jetstream.AddConsumer(cfg.NATS.PNGStreamName, &nats.ConsumerConfig{
		Durable:            cfg.NATS.PNGConsumerName,
		AckPolicy:          nats.AckExplicitPolicy,
		Name:               "",
		Description:        "",
		DeliverPolicy:      nats.DeliverAllPolicy,
		OptStartSeq:        0,
		OptStartTime:       nil,
		AckWait:            0,
		MaxDeliver:         0,
		BackOff:            nil,
		FilterSubject:      "",
		FilterSubjects:     nil,
		ReplayPolicy:       nats.ReplayInstantPolicy,
		RateLimit:          0,
		SampleFrequency:    "",
		MaxWaiting:         0,
		MaxAckPending:      0,
		FlowControl:        false,
		Heartbeat:          0,
		HeadersOnly:        false,
		MaxRequestBatch:    0,
		MaxRequestExpires:  0,
		MaxRequestMaxBytes: 0,
		DeliverSubject:     "",
		DeliverGroup:       "",
		InactiveThreshold:  0,
		Replicas:           0,
		MemoryStorage:      false,
		Metadata:           nil,
	})
	require.NoError(t, err)

	return natsConn, jetstream
}

func TestNatsWorker_Run_Success(t *testing.T) {
	t.Parallel()
	log := newTestLogger(t)
	cfg := newTestConfig(t)
	natsConn, jetstream := setupNatsTest(t, cfg)

	// Publish a message
	_, err := jetstream.Publish(cfg.NATS.PNGCreatedSubject, []byte("test data"))
	require.NoError(t, err)
	require.NoError(t, natsConn.Flush())

	mockPipeline := &mockPipeline{
		ProcessFunc: func(_ context.Context, _ string, _ []byte) (string, error) {
			return "processed text", nil
		},
	}

	natsWorker, err := worker.New(
		cfg.NATS.URL,
		cfg.NATS.PNGStreamName,
		cfg.NATS.PNGCreatedSubject,
		cfg.NATS.PNGConsumerName,
		cfg.NATS.TextProcessedSubject,
		cfg.NATS.DeadLetterSubject,
		mockPipeline,
		log,
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = natsWorker.Run(ctx)
	}()

	// Wait for the message to be processed
	time.Sleep(1 * time.Second)

	// Check that the output message was published
	sub, err := jetstream.SubscribeSync(cfg.NATS.TextProcessedSubject)
	require.NoError(t, err)

	msg, err := sub.NextMsg(1 * time.Second)
	require.NoError(t, err)
	require.Equal(t, "processed text", string(msg.Data))
}

func TestNatsWorker_Run_PipelineError(t *testing.T) {
	t.Parallel()
	log := newTestLogger(t)
	cfg := newTestConfig(t)
	natsConn, jetstream := setupNatsTest(t, cfg)

	// Publish a message
	_, err := jetstream.Publish(cfg.NATS.PNGCreatedSubject, []byte("test data"))
	require.NoError(t, err)
	require.NoError(t, natsConn.Flush())

	mockPipeline := &mockPipeline{
		ProcessFunc: func(_ context.Context, _ string, _ []byte) (string, error) {
			return "", fmt.Errorf("mock pipeline error: %w", errPipelineError)
		},
	}

	natsWorker, err := worker.New(
		cfg.NATS.URL,
		cfg.NATS.PNGStreamName,
		cfg.NATS.PNGCreatedSubject,
		cfg.NATS.PNGConsumerName,
		cfg.NATS.TextProcessedSubject,
		cfg.NATS.DeadLetterSubject,
		mockPipeline,
		log,
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = natsWorker.Run(ctx)
	}()

	// Wait for the message to be processed
	time.Sleep(1 * time.Second)

	// Check that the message was published to the dead-letter subject
	sub, err := jetstream.SubscribeSync(cfg.NATS.DeadLetterSubject)
	require.NoError(t, err)

	msg, err := sub.NextMsg(1 * time.Second)
	require.NoError(t, err)
	require.Equal(t, "test data", string(msg.Data))
}

func TestNatsWorker_Run_EmptyMessage(t *testing.T) {
	t.Parallel()
	log := newTestLogger(t)
	cfg := newTestConfig(t)
	natsConn, jetstream := setupNatsTest(t, cfg)

	// Publish an empty message
	_, err := jetstream.Publish(cfg.NATS.PNGCreatedSubject, []byte(""))
	require.NoError(t, err)
	require.NoError(t, natsConn.Flush())

	mockPipeline := &mockPipeline{ProcessFunc: nil}

	natsWorker, err := worker.New(
		cfg.NATS.URL,
		cfg.NATS.PNGStreamName,
		cfg.NATS.PNGCreatedSubject,
		cfg.NATS.PNGConsumerName,
		cfg.NATS.TextProcessedSubject,
		cfg.NATS.DeadLetterSubject,
		mockPipeline,
		log,
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = natsWorker.Run(ctx)
	}()

	// Wait for the message to be processed
	time.Sleep(1 * time.Second)

	// Check that no message was published to the output subject
	sub, err := jetstream.SubscribeSync(cfg.NATS.TextProcessedSubject)
	require.NoError(t, err)

	_, err = sub.NextMsg(1 * time.Second)
	require.Error(t, err)
	require.Equal(t, nats.ErrTimeout, err)
}

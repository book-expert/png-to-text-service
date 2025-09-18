package pipeline_test

import (
	"context"
	"errors"
	"fmt" // Added fmt
	"testing"

	"github.com/book-expert/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/book-expert/png-to-text-service/internal/augment"
	"github.com/book-expert/png-to-text-service/internal/pipeline"
)

var (
	errOcrError     = errors.New("ocr error")
	errAugmentError = errors.New("augment error")
)

// mockOCRProcessor is a mock implementation of the ocr.Processor for testing.
type mockOCRProcessor struct {
	ProcessPNGFunc func(ctx context.Context, filePath string) (string, error)
}

func (m *mockOCRProcessor) ProcessPNG(
	ctx context.Context,
	filePath string,
) (string, error) {
	return m.ProcessPNGFunc(ctx, filePath)
}

// mockAugmenter is a mock implementation of the augment.GeminiProcessor for testing.
type mockAugmenter struct {
	AugmentTextWithOptionsFunc func(
		ctx context.Context,
		text, imagePath string,
		opts *augment.AugmentationOptions,
	) (string, error)
}

func (m *mockAugmenter) AugmentTextWithOptions(
	ctx context.Context,
	text, imagePath string,
	opts *augment.AugmentationOptions,
) (string, error) {
	return m.AugmentTextWithOptionsFunc(ctx, text, imagePath, opts)
}

func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(t.TempDir(), "test.log")
	require.NoError(t, err)

	return log
}

func TestPipeline_Process_Success(t *testing.T) {
	t.Parallel()
	log := newTestLogger(t)
	ocr := &mockOCRProcessor{
		ProcessPNGFunc: func(_ context.Context, _ string) (string, error) {
			return "ocr text", nil
		},
	}
	augmenter := &mockAugmenter{
		AugmentTextWithOptionsFunc: func(
			_ context.Context,
			_ string,
			_ string,
			_ *augment.AugmentationOptions,
		) (string, error) {
			return "augmented text", nil
		},
	}

	testPipeline, err := pipeline.New(
		ocr,
		augmenter,
		log,
		false,
		5,
		&augment.AugmentationOptions{Parameters: nil, Type: "", CustomPrompt: ""},
	)
	require.NoError(t, err)

	result, err := testPipeline.Process(context.Background(), "test-id", []byte("png data"))

	require.NoError(t, err)
	assert.Equal(t, "augmented text", result)
}

func TestPipeline_Process_OCR_Error(t *testing.T) {
	t.Parallel()
	log := newTestLogger(t)
	ocr := &mockOCRProcessor{
		ProcessPNGFunc: func(_ context.Context, _ string) (string, error) {
			return "", fmt.Errorf("mock ocr error: %w", errOcrError)
		},
	}
	augmenter := &mockAugmenter{AugmentTextWithOptionsFunc: nil}

	testPipeline, err := pipeline.New(
		ocr,
		augmenter,
		log,
		false,
		10,
		&augment.AugmentationOptions{Parameters: nil, Type: "", CustomPrompt: ""},
	)
	require.NoError(t, err)

	_, err = testPipeline.Process(context.Background(), "test-id", []byte("png data"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "OCR processing: mock ocr error: ocr error")
}

func TestPipeline_Process_Augment_Error(t *testing.T) {
	t.Parallel()
	log := newTestLogger(t)
	ocr := &mockOCRProcessor{
		ProcessPNGFunc: func(_ context.Context, _ string) (string, error) {
			return "ocr text", nil
		},
	}
	augmenter := &mockAugmenter{
		AugmentTextWithOptionsFunc: func(
			_ context.Context,
			_ string,
			_ string,
			_ *augment.AugmentationOptions,
		) (string, error) {
			return "", fmt.Errorf("mock augment error: %w", errAugmentError)
		},
	}

	testPipeline, err := pipeline.New(
		ocr,
		augmenter,
		log,
		false,
		5,
		&augment.AugmentationOptions{Parameters: nil, Type: "", CustomPrompt: ""},
	)
	require.NoError(t, err)

	result, err := testPipeline.Process(context.Background(), "test-id", []byte("png data"))

	require.NoError(t, err)
	assert.Equal(t, "ocr text", result)
}

func TestPipeline_Process_ShortText(t *testing.T) {
	t.Parallel()
	log := newTestLogger(t)
	ocr := &mockOCRProcessor{
		ProcessPNGFunc: func(_ context.Context, _ string) (string, error) {
			return "short", nil
		},
	}
	augmenter := &mockAugmenter{AugmentTextWithOptionsFunc: nil}

	testPipeline, err := pipeline.New(
		ocr,
		augmenter,
		log,
		false,
		10,
		&augment.AugmentationOptions{Parameters: nil, Type: "", CustomPrompt: ""},
	)
	require.NoError(t, err)

	result, err := testPipeline.Process(context.Background(), "test-id", []byte("png data"))

	require.NoError(t, err)
	assert.Equal(t, "short", result)
}

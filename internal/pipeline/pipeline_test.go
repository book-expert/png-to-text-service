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

const testOcrText = "ocr text"

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
            return testOcrText, nil
        },
    }
	augmenter := &mockAugmenter{
		AugmentTextWithOptionsFunc: func(
			_ context.Context,
			_ string,
			_ string,
			opts *augment.AugmentationOptions,
		) (string, error) {
			if opts == nil || !opts.Commentary.Enabled {
				t.Fatalf("expected commentary augmentation to be enabled")
			}

			return "augmented text", nil
		},
	}
    defaultOptions := &augment.AugmentationOptions{
        Parameters: nil,
        Commentary: augment.AugmentationCommentaryOptions{Enabled: true, CustomAdditions: ""},
        Summary:    augment.AugmentationSummaryOptions{Enabled: false, Placement: "", CustomAdditions: ""},
    }

	testPipeline, err := pipeline.New(
		ocr,
		augmenter,
		log,
		false,
		5,
		defaultOptions,
	)
	require.NoError(t, err)

	result, err := testPipeline.Process(context.Background(), "test-id", []byte("png data"), nil)

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
    defaultOptions := &augment.AugmentationOptions{
        Parameters: nil,
        Commentary: augment.AugmentationCommentaryOptions{Enabled: true, CustomAdditions: ""},
        Summary:    augment.AugmentationSummaryOptions{Enabled: false, Placement: "", CustomAdditions: ""},
    }

	testPipeline, err := pipeline.New(
		ocr,
		augmenter,
		log,
		false,
		10,
		defaultOptions,
	)
	require.NoError(t, err)

	_, err = testPipeline.Process(context.Background(), "test-id", []byte("png data"), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "OCR processing: mock ocr error: ocr error")
}

func TestPipeline_Process_Augment_Error(t *testing.T) {
	t.Parallel()
	log := newTestLogger(t)
    ocr := &mockOCRProcessor{
        ProcessPNGFunc: func(_ context.Context, _ string) (string, error) {
            return testOcrText, nil
        },
    }
	augmenter := &mockAugmenter{
		AugmentTextWithOptionsFunc: func(
			_ context.Context,
			_ string,
			_ string,
			opts *augment.AugmentationOptions,
		) (string, error) {
			if opts == nil || !opts.Commentary.Enabled {
				t.Fatalf("expected commentary enabled for augment error test")
			}

			return "", fmt.Errorf("mock augment error: %w", errAugmentError)
		},
	}
    defaultOptions := &augment.AugmentationOptions{
        Parameters: nil,
        Commentary: augment.AugmentationCommentaryOptions{Enabled: true, CustomAdditions: ""},
        Summary:    augment.AugmentationSummaryOptions{Enabled: false, Placement: "", CustomAdditions: ""},
    }

	testPipeline, err := pipeline.New(
		ocr,
		augmenter,
		log,
		false,
		5,
		defaultOptions,
	)
	require.NoError(t, err)

	result, err := testPipeline.Process(context.Background(), "test-id", []byte("png data"), nil)

	require.NoError(t, err)
    assert.Equal(t, testOcrText, result)
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
    defaultOptions := &augment.AugmentationOptions{
        Parameters: nil,
        Commentary: augment.AugmentationCommentaryOptions{Enabled: true, CustomAdditions: ""},
        Summary:    augment.AugmentationSummaryOptions{Enabled: false, Placement: "", CustomAdditions: ""},
    }

	testPipeline, err := pipeline.New(
		ocr,
		augmenter,
		log,
		false,
		10,
		defaultOptions,
	)
	require.NoError(t, err)

	result, err := testPipeline.Process(context.Background(), "test-id", []byte("png data"), nil)

	require.NoError(t, err)
	assert.Equal(t, "short", result)
}

func TestPipeline_Process_DisabledOverrides(t *testing.T) {
	t.Parallel()
	log := newTestLogger(t)
    ocr := &mockOCRProcessor{
        ProcessPNGFunc: func(_ context.Context, _ string) (string, error) {
            return testOcrText, nil
        },
    }
	augmenter := &mockAugmenter{
		AugmentTextWithOptionsFunc: func(
			_ context.Context,
			_ string,
			_ string,
			_ *augment.AugmentationOptions,
		) (string, error) {
			t.Fatalf("augmentation should be skipped when overrides disable it")

			return "", nil
		},
	}
    defaultOptions := &augment.AugmentationOptions{
        Parameters: nil,
        Commentary: augment.AugmentationCommentaryOptions{Enabled: true, CustomAdditions: ""},
        Summary:    augment.AugmentationSummaryOptions{Enabled: false, Placement: "", CustomAdditions: ""},
    }

	testPipeline, err := pipeline.New(
		ocr,
		augmenter,
		log,
		false,
		5,
		defaultOptions,
	)
	require.NoError(t, err)

    overrides := &augment.AugmentationOptions{
        Parameters: nil,
        Commentary: augment.AugmentationCommentaryOptions{Enabled: false, CustomAdditions: ""},
        Summary:    augment.AugmentationSummaryOptions{Enabled: false, Placement: "", CustomAdditions: ""},
    }

	result, err := testPipeline.Process(context.Background(), "test-id", []byte("png data"), overrides)
	require.NoError(t, err)
    assert.Equal(t, testOcrText, result)
}

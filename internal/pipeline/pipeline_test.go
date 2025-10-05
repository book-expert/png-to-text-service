package pipeline_test

import (
	"context"
	"testing"

	"github.com/book-expert/logger"
	"github.com/book-expert/png-to-text-service/internal/pipeline"
	"github.com/book-expert/png-to-text-service/internal/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockOCRProcessor is a mock implementation of the OCRProcessor interface.
type mockOCRProcessor struct {
	Result string
	Err    error
}

func (m *mockOCRProcessor) ProcessPNG(ctx context.Context, filePath string) (string, error) {
	return m.Result, m.Err
}

// mockAugmenter is a mock implementation of the Augmenter interface.
type mockAugmenter struct {
	Result string
	Err    error
}

func (m *mockAugmenter) AugmentTextWithOptions(
	ctx context.Context,
	text string,
	pngData []byte,
	opts *shared.AugmentationOptions,
) (string, error) {
	return m.Result, m.Err
}

func TestPipeline_Process_SuccessfulProcessingWithAugmentation(t *testing.T) {
	t.Parallel()

	log, err := logger.New(t.TempDir(), "test.log")
	require.NoError(t, err)

	ocrProcessor := &mockOCRProcessor{
		Result: "This is the OCR text.",
	}

	augmenter := &mockAugmenter{
		Result: "This is the augmented text.",
	}

	p, err := pipeline.New(ocrProcessor, augmenter, log, false, 10, nil)
	require.NoError(t, err)

	overrides := &shared.AugmentationOptions{
		Commentary: shared.AugmentationCommentaryOptions{Enabled: true},
	}

	result, err := p.Process(context.Background(), "test-object", []byte("test-png-data"), overrides)

	assert.NoError(t, err)
	assert.Equal(t, "This is the augmented text.", result)
}
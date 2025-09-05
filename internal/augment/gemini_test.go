package augment_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nnikolov3/logger"
	"github.com/nnikolov3/png-to-text-service/internal/augment"
	"github.com/stretchr/testify/require"
)

func TestNewGeminiProcessor(t *testing.T) {
	t.Parallel()

	config := &augment.GeminiConfig{
		APIKey:            "test-api-key",
		PromptTemplate:    "test prompt",
		CustomPrompt:      "",
		AugmentationType:  augment.AugmentationCommentary,
		Models:            []string{"gemini-1.5-flash"},
		Temperature:       0.7,
		TimeoutSeconds:    120,
		TopK:              40,
		TopP:              0.95,
		MaxTokens:         8192,
		RetryDelaySeconds: 30,
		MaxRetries:        3,
		UsePromptBuilder:  false,
	}

	log, err := logger.New("/tmp", "gemini_test.log")
	require.NoError(t, err)

	processor := augment.NewGeminiProcessor(config, log)

	require.NotNil(t, processor)
}

func TestAugmentText_ValidationErrors(t *testing.T) {
	t.Parallel()

	config := &augment.GeminiConfig{
		APIKey:            "test-api-key",
		PromptTemplate:    "test prompt",
		CustomPrompt:      "",
		AugmentationType:  augment.AugmentationCommentary,
		Models:            []string{"gemini-1.5-flash"},
		Temperature:       0.7,
		TimeoutSeconds:    120,
		TopK:              40,
		TopP:              0.95,
		MaxTokens:         8192,
		RetryDelaySeconds: 30,
		MaxRetries:        3,
		UsePromptBuilder:  false,
	}

	log, err := logger.New("/tmp", "gemini_test.log")
	require.NoError(t, err)

	processor := augment.NewGeminiProcessor(config, log)
	ctx := context.Background()

	testCases := []struct {
		name           string
		setupImage     func(t *testing.T) string
		ocrText        string
		expectedErrMsg string
	}{
		{
			name: "empty image path rejected",
			setupImage: func(_ *testing.T) string {
				return ""
			},
			ocrText:        "test ocr text",
			expectedErrMsg: "image path is required",
		},
		{
			name: "nonexistent image path rejected",
			setupImage: func(_ *testing.T) string {
				return "/tmp/nonexistent-image.png"
			},
			ocrText:        "test ocr text",
			expectedErrMsg: "access image file",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			imagePath := testCase.setupImage(t)

			_, err := processor.AugmentText(ctx, testCase.ocrText, imagePath)
			require.Error(t, err)
			require.Contains(t, err.Error(), testCase.expectedErrMsg)
		})
	}
}

func TestAugmentText_WithValidImage(t *testing.T) {
	t.Parallel()

	config := &augment.GeminiConfig{
		APIKey:            "test-api-key",
		PromptTemplate:    "test prompt",
		CustomPrompt:      "",
		AugmentationType:  augment.AugmentationCommentary,
		Models:            []string{"gemini-1.5-flash"},
		Temperature:       0.7,
		TimeoutSeconds:    120,
		TopK:              40,
		TopP:              0.95,
		MaxTokens:         8192,
		RetryDelaySeconds: 30,
		MaxRetries:        3,
		UsePromptBuilder:  false,
	}

	log, err := logger.New("/tmp", "gemini_test.log")
	require.NoError(t, err)

	processor := augment.NewGeminiProcessor(config, log)
	ctx := context.Background()

	// Create a dummy PNG file
	imagePath := filepath.Join(t.TempDir(), "test.png")

	err = os.WriteFile(imagePath, []byte("dummy png content"), 0o600)
	require.NoError(t, err)

	// This will fail since we don't have a real API key and network,
	// but it tests that the validation passes
	_, err = processor.AugmentText(ctx, "test ocr text", imagePath)
	require.Error(t, err) // Expected to fail at HTTP request stage
	require.NotContains(t, err.Error(), "image path is required")
	require.NotContains(t, err.Error(), "access image file")
}

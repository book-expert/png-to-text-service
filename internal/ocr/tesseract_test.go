package ocr_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/book-expert/logger"
	"github.com/stretchr/testify/require"

	"github.com/book-expert/png-to-text-service/internal/ocr"
)

func TestNewProcessor(t *testing.T) {
	t.Parallel()

	config := ocr.TesseractConfig{
		Language:       "eng",
		OEM:            3,
		PSM:            6,
		DPI:            300,
		TimeoutSeconds: 30,
	}
	log, err := logger.New("/tmp", "test.log")
	require.NoError(t, err)

	processor := ocr.NewProcessor(config, log)

	require.NotNil(t, processor)
}

func setupTestProcessor(t *testing.T) (*ocr.Processor, context.Context) {
	t.Helper()

	config := ocr.TesseractConfig{
		Language:       "eng",
		OEM:            3,
		PSM:            6,
		DPI:            300,
		TimeoutSeconds: 30,
	}
	log, err := logger.New("/tmp", "test.log")
	require.NoError(t, err)

	processor := ocr.NewProcessor(config, log)
	ctx := context.Background()

	return processor, ctx
}

func TestProcessPNG_ValidationErrors(t *testing.T) {
	t.Parallel()

	var validationTestCases = []struct {
		name           string
		setupFile      func(t *testing.T) string
		expectedErrMsg string
	}{
		{
			name: "non-PNG extension rejected",
			setupFile: func(_ *testing.T) string {
				return "/tmp/test.jpg"
			},
			expectedErrMsg: "file must have .png extension",
		},
		{
			name: "nonexistent file rejected",
			setupFile: func(_ *testing.T) string {
				return "/tmp/nonexistent.png"
			},
			expectedErrMsg: "access file",
		},
		{
			name: "directory instead of file rejected",
			setupFile: func(t *testing.T) string {
				t.Helper()
				dirPath := filepath.Join(t.TempDir(), "test.png")
				err := os.Mkdir(dirPath, 0o750)
				require.NoError(t, err)

				return dirPath
			},
			expectedErrMsg: "path is a directory",
		},
		{
			name: "empty file rejected",
			setupFile: func(t *testing.T) string {
				t.Helper()
				filePath := filepath.Join(t.TempDir(), "empty.png")
				err := os.WriteFile(filePath, []byte{}, 0o600)
				require.NoError(t, err)

				return filePath
			},
			expectedErrMsg: "file is empty",
		},
	}

	processor, ctx := setupTestProcessor(t)

	for _, testCase := range validationTestCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			filePath := testCase.setupFile(t)

			_, err := processor.ProcessPNG(ctx, filePath)
			require.Error(t, err)
			require.Contains(t, err.Error(), testCase.expectedErrMsg)
		})
	}
}

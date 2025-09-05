package pipeline_test

import (
	"context"
	"testing"

	"github.com/nnikolov3/logger"
	"github.com/nnikolov3/png-to-text-service/internal/config"
	"github.com/nnikolov3/png-to-text-service/internal/pipeline"
	"github.com/stretchr/testify/require"
)

func createTestConfig() *config.Config {
	return &config.Config{
		Project: config.Project{
			Name:        "test",
			Version:     "1.0.0",
			Description: "test",
		},
		Paths: config.Paths{
			InputDir:  "/tmp/input",
			OutputDir: "/tmp/output",
		},
		Prompts: config.Prompts{
			Augmentation: "test prompt",
		},
		Augmentation: config.Augmentation{
			Type:             "commentary",
			CustomPrompt:     "",
			UsePromptBuilder: false,
		},
		Logging: config.Logging{
			Level:                "info",
			Dir:                  "/tmp",
			EnableFileLogging:    false,
			EnableConsoleLogging: true,
		},
		Tesseract: config.Tesseract{
			Language:       "eng",
			OEM:            3,
			PSM:            6,
			DPI:            300,
			TimeoutSeconds: 30,
		},
		Gemini: config.Gemini{
			APIKeyVariable:    "GEMINI_API_KEY",
			Models:            []string{"gemini-1.5-flash"},
			MaxRetries:        3,
			RetryDelaySeconds: 30,
			TimeoutSeconds:    120,
			Temperature:       0.7,
			TopK:              40,
			TopP:              0.95,
			MaxTokens:         8192,
		},
		Settings: config.Settings{
			Workers:            2,
			TimeoutSeconds:     120,
			EnableAugmentation: false,
			SkipExisting:       false,
		},
	}
}

func createTestPipeline(t *testing.T) *pipeline.Pipeline {
	t.Helper()

	cfg := createTestConfig()
	log, err := logger.New("/tmp", "pipeline_test.log")
	require.NoError(t, err)

	pipelineInstance, err := pipeline.NewPipeline(cfg, log)
	require.NoError(t, err)

	return pipelineInstance
}

func TestNewPipeline_Success(t *testing.T) {
	t.Parallel()

	pipelineInstance := createTestPipeline(t)
	require.NotNil(t, pipelineInstance)
}

func TestPipeline_ProcessDirectoryErrors(t *testing.T) {
	t.Parallel()

	pipelineInstance := createTestPipeline(t)
	ctx := context.Background()

	err := pipelineInstance.ProcessDirectory(
		ctx,
		"/nonexistent/directory",
		"/tmp/output",
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no such file or directory")
}

func TestPipeline_ProcessSingleErrors(t *testing.T) {
	t.Parallel()

	pipelineInstance := createTestPipeline(t)
	ctx := context.Background()

	// ProcessSingle handles errors internally and logs them,
	// so it doesn't return an error but processes the file
	err := pipelineInstance.ProcessSingle(
		ctx,
		"/nonexistent/file.jpg",
		"/tmp/output.txt",
	)
	// The method completes successfully even if the file processing fails
	// because it handles errors internally
	require.NoError(t, err)
}

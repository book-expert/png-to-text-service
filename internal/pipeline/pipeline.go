// ./internal/pipeline/pipeline.go
package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/book-expert/logger"

	"github.com/book-expert/png-to-text-service/internal/augment"
	"github.com/book-expert/png-to-text-service/internal/ocr"
)

// Pipeline orchestrates the multi-step process of converting a PNG to augmented text.
type Pipeline struct {
	// REMOVED: No more storage dependency.
	ocr              *ocr.Processor
	augmenter        *augment.GeminiProcessor
	logger           *logger.Logger
	localTmpDir      string
	outputDir        string
	keepTempFiles    bool
	minTextLength    int
	augmentationOpts *augment.AugmentationOptions
}

// New creates a new pipeline with all its dependencies.
func New(
	// REMOVED: storage argument is gone.
	ocr *ocr.Processor,
	augmenter *augment.GeminiProcessor,
	log *logger.Logger,
	outputDir string,
	keepTempFiles bool,
	minTextLength int,
	augOpts *augment.AugmentationOptions,
) (*Pipeline, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("could not get current working directory: %w", err)
	}
	localTmpDir := filepath.Join(cwd, "tmp")
	if err := os.MkdirAll(localTmpDir, 0o755); err != nil {
		return nil, fmt.Errorf(
			"could not create local temp directory '%s': %w",
			localTmpDir,
			err,
		)
	}

	return &Pipeline{
		ocr:              ocr,
		augmenter:        augmenter,
		logger:           log,
		localTmpDir:      localTmpDir,
		outputDir:        outputDir,
		keepTempFiles:    keepTempFiles,
		minTextLength:    minTextLength,
		augmentationOpts: augOpts,
	}, nil
}

// Process handles the full workflow for a single object.
// MODIFIED: It now accepts the raw pngData directly.
func (p *Pipeline) Process(ctx context.Context, objectID string, pngData []byte) error {
	p.logger.Info("Processing job for object: %s", objectID)

	// REMOVED: The call to storage.GetObject is no longer needed.

	tmpFile, err := p.createTempFile(pngData)
	if err != nil {
		return fmt.Errorf("create temp file for '%s': %w", objectID, err)
	}
	tmpFileName := tmpFile.Name()

	if !p.keepTempFiles {
		defer func() {
			if err := os.Remove(tmpFileName); err != nil {
				p.logger.Error(
					"Failed to remove temporary file %s: %v",
					tmpFileName,
					err,
				)
			}
		}()
	}

	p.logger.Info("Running OCR on temporary file: %s", tmpFileName)

	cleanedText, err := p.ocr.ProcessPNG(ctx, tmpFileName)
	if err != nil {
		return fmt.Errorf("OCR processing: %w", err)
	}

	if len(cleanedText) < p.minTextLength {
		p.logger.Warn(
			"OCR text for %s is too short (%d chars), skipping augmentation.",
			objectID,
			len(cleanedText),
		)
	} else {
		p.logger.Info("Augmenting text for %s", objectID)
		augmentedText, err := p.augmenter.AugmentTextWithOptions(ctx, cleanedText, tmpFileName, p.augmentationOpts)
		if err != nil {
			p.logger.Warn("Text augmentation failed for %s: %v. Using cleaned OCR text as fallback.", objectID, err)
		} else {
			cleanedText = augmentedText
		}
	}

	outputFileName := generateOutputFileName(objectID)
	outputFilePath := filepath.Join(p.outputDir, outputFileName)
	if err := os.WriteFile(outputFilePath, []byte(cleanedText), 0o644); err != nil {
		return fmt.Errorf("write output file %s: %w", outputFilePath, err)
	}

	p.logger.Info("Successfully wrote output for %s -> %s", objectID, outputFileName)
	return nil
}

func (p *Pipeline) createTempFile(data []byte) (*os.File, error) {
	tmpFile, err := os.CreateTemp(p.localTmpDir, "ocr-*.png")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			p.logger.Error(
				"failed to close temp file %s after write error: %v",
				tmpFile.Name(),
				closeErr,
			)
		}
		return nil, fmt.Errorf("write to temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("close temp file: %w", err)
	}
	return tmpFile, nil
}

func generateOutputFileName(objectID string) string {
	return objectID + ".txt"
}

// Package pipeline orchestrates the OCR and augmentation process.
package pipeline

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/book-expert/logger"

	"github.com/book-expert/png-to-text-service/internal/augment"
)

const (
	defaultDirPermissions = 0o750
)

// OCRProcessor defines the interface for an OCR processor.
type OCRProcessor interface {
	ProcessPNG(ctx context.Context, filePath string) (string, error)
}

// Augmenter defines the interface for a text augmenter.
type Augmenter interface {
	AugmentTextWithOptions(
		ctx context.Context,
		text, imagePath string,
		opts *augment.AugmentationOptions,
	) (string, error)
}

// Pipeline orchestrates the multi-step process of converting a PNG to augmented text.
type Pipeline struct {
	ocr                 OCRProcessor
	augmenter           Augmenter
	logger              *logger.Logger
	localTmpDir         string
	keepTempFiles       bool
	minTextLength       int
	defaultAugmentation *augment.AugmentationOptions
}

// New creates a new pipeline with all its dependencies.
func New(
	ocr OCRProcessor,
	augmenter Augmenter,
	log *logger.Logger,
	keepTempFiles bool,
	minTextLength int,
	augOpts *augment.AugmentationOptions,
) (*Pipeline, error) {
	localTmpDir := os.TempDir()

	err := os.MkdirAll(localTmpDir, defaultDirPermissions)
	if err != nil {
		return nil, fmt.Errorf(
			"could not create local temp directory '%s': %w",
			localTmpDir,
			err,
		)
	}

	return &Pipeline{
		ocr:                 ocr,
		augmenter:           augmenter,
		logger:              log,
		localTmpDir:         localTmpDir,
		keepTempFiles:       keepTempFiles,
		minTextLength:       minTextLength,
		defaultAugmentation: cloneAugmentationOptions(augOpts),
	}, nil
}

// Process handles the full workflow for a single object.
func (p *Pipeline) Process(
	ctx context.Context,
	objectID string,
	pngData []byte,
	overrides *augment.AugmentationOptions,
) (string, error) {
	p.logger.Info("Processing job for object: %s", objectID)

	// REMOVED: The call to storage.GetObject is no longer needed.

	tmpFile, err := p.createTempFile(pngData)
	if err != nil {
		return "", fmt.Errorf("create temp file for '%s': %w", objectID, err)
	}

	tmpFileName := tmpFile.Name()

	if !p.keepTempFiles {
		defer func() {
			err := os.Remove(tmpFileName)
			if err != nil {
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
		return "", fmt.Errorf("OCR processing: %w", err)
	}

	if len(cleanedText) < p.minTextLength {
		p.logger.Warn(
			"OCR text for %s is too short (%d chars), skipping augmentation.",
			objectID,
			len(cleanedText),
		)
	} else {
		effectiveOptions := p.mergeAugmentationOptions(overrides)

		if effectiveOptions.Commentary.Enabled || effectiveOptions.Summary.Enabled {
			p.logger.Info("Augmenting text for %s", objectID)

			augmentedText, err := p.augmenter.AugmentTextWithOptions(ctx, cleanedText, tmpFileName, effectiveOptions)
			if err != nil {
				p.logger.Warn("Text augmentation failed for %s: %v. Using cleaned OCR text as fallback.", objectID, err)
			} else {
				cleanedText = augmentedText
			}
		} else {
			p.logger.Info("Augmentation disabled for %s; returning OCR text.", objectID)
		}
	}

	p.logger.Info("Successfully processed object %s", objectID)

	return cleanedText, nil
}

func (p *Pipeline) mergeAugmentationOptions(
	overrides *augment.AugmentationOptions,
) *augment.AugmentationOptions {
	base := cloneAugmentationOptions(p.defaultAugmentation)
	if base == nil {
		base = &augment.AugmentationOptions{}
	}

	if overrides == nil {
		return base
	}

	if overrides.Parameters != nil {
		base.Parameters = mergeParameterMaps(base.Parameters, overrides.Parameters)
	}

	base.Commentary.Enabled = overrides.Commentary.Enabled
	if addition := strings.TrimSpace(overrides.Commentary.CustomAdditions); addition != "" {
		base.Commentary.CustomAdditions = appendInstruction(base.Commentary.CustomAdditions, addition)
	}

	base.Summary.Enabled = overrides.Summary.Enabled
	if overrides.Summary.Placement != "" {
		base.Summary.Placement = overrides.Summary.Placement
	}
	if addition := strings.TrimSpace(overrides.Summary.CustomAdditions); addition != "" {
		base.Summary.CustomAdditions = appendInstruction(base.Summary.CustomAdditions, addition)
	}

	return base
}

func cloneAugmentationOptions(src *augment.AugmentationOptions) *augment.AugmentationOptions {
	if src == nil {
		return nil
	}

	clone := &augment.AugmentationOptions{
		Parameters: cloneParameterMap(src.Parameters),
		Commentary: src.Commentary,
		Summary:    src.Summary,
	}

	return clone
}

func cloneParameterMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}

	clone := make(map[string]any, len(src))
	for key, value := range src {
		clone[key] = value
	}

	return clone
}

func mergeParameterMaps(
	defaults map[string]any,
	overrides map[string]any,
) map[string]any {
	result := cloneParameterMap(defaults)
	if result == nil {
		result = make(map[string]any, len(overrides))
	}

	for key, value := range overrides {
		result[key] = value
	}

	return result
}

func appendInstruction(base, addition string) string {
	base = strings.TrimSpace(base)
	addition = strings.TrimSpace(addition)

	switch {
	case base == "":
		return addition
	case addition == "":
		return base
	default:
		return base + "\n\n" + addition
	}
}

func (p *Pipeline) createTempFile(data []byte) (*os.File, error) {
	tmpFile, err := os.CreateTemp(p.localTmpDir, "ocr-*.png")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}

	_, err = tmpFile.Write(data)
	if err != nil {
		closeErr := tmpFile.Close()
		if closeErr != nil {
			p.logger.Error(
				"failed to close temp file %s after write error: %v",
				tmpFile.Name(),
				closeErr,
			)
		}

		return nil, fmt.Errorf("write to temp file: %w", err)
	}

	closeErr := tmpFile.Close()
	if closeErr != nil {
		return nil, fmt.Errorf("close temp file: %w", closeErr)
	}

	return tmpFile, nil
}

// ./internal/pipeline/pipeline.go
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/nnikolov3/logger"

	"github.com/nnikolov3/png-to-text-service/internal/augment"
	"github.com/nnikolov3/png-to-text-service/internal/config"
	"github.com/nnikolov3/png-to-text-service/internal/ocr"
)

const (
	defaultFilePermission = 0o600
	defaultDirPermission  = 0o750
)

var (
	ErrAPIKeyNotFound       = errors.New("API key not found in environment variable")
	ErrAugmentationDisabled = errors.New("text augmentation is disabled")
	pageNumberRegex         = regexp.MustCompile(`(?m)^\s*\d+\s*$`)
)

type OCRProcessor interface {
	ProcessPNG(ctx context.Context, pngPath string) (string, error)
}

type TextAugmenter interface {
	AugmentTextWithOptions(
		ctx context.Context,
		ocrText, imagePath string,
		opts *augment.AugmentationOptions,
	) (string, error)
}

type Pipeline struct {
	ocrProcessor  OCRProcessor
	textAugmenter TextAugmenter
	config        *config.Config
	logger        *logger.Logger
	enableAugment bool
	skipExisting  bool
	workers       int
}

type ProcessingResult struct {
	Error   error
	Success bool
}

func NewPipeline(cfg *config.Config, log *logger.Logger) (*Pipeline, error) {
	ocrProcessor := newOCRProcessorFromConfig(cfg, log)
	textAugmenter, err := newTextAugmenterFromConfig(cfg, log)
	if err != nil && !errors.Is(err, ErrAugmentationDisabled) {
		return nil, err
	}
	return &Pipeline{
		ocrProcessor:  ocrProcessor,
		textAugmenter: textAugmenter,
		config:        cfg,
		logger:        log,
		enableAugment: cfg.Settings.EnableAugmentation,
		skipExisting:  cfg.Settings.SkipExisting,
		workers:       cfg.Settings.Workers,
	}, nil
}

func newOCRProcessorFromConfig(cfg *config.Config, log *logger.Logger) *ocr.Processor {
	tesseractConfig := ocr.TesseractConfig{
		Language:       cfg.Tesseract.Language,
		OEM:            cfg.Tesseract.OEM,
		PSM:            cfg.Tesseract.PSM,
		DPI:            cfg.Tesseract.DPI,
		TimeoutSeconds: cfg.Tesseract.TimeoutSeconds,
	}
	return ocr.NewProcessor(tesseractConfig, log)
}

func newTextAugmenterFromConfig(
	cfg *config.Config,
	log *logger.Logger,
) (*augment.GeminiProcessor, error) {
	if !cfg.Settings.EnableAugmentation {
		return nil, ErrAugmentationDisabled
	}
	apiKey := cfg.GetAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf(
			"API key not found in environment variable %s: %w",
			cfg.Gemini.APIKeyVariable,
			ErrAPIKeyNotFound,
		)
	}
	geminiConfig := augment.GeminiConfig{
		APIKey:            apiKey,
		Models:            cfg.Gemini.Models,
		MaxRetries:        cfg.Gemini.MaxRetries,
		RetryDelaySeconds: cfg.Gemini.RetryDelaySeconds,
		TimeoutSeconds:    cfg.Gemini.TimeoutSeconds,
		Temperature:       cfg.Gemini.Temperature,
		TopK:              cfg.Gemini.TopK,
		TopP:              cfg.Gemini.TopP,
		MaxTokens:         cfg.Gemini.MaxTokens,
		PromptTemplate:    cfg.Prompts.Augmentation,
		UsePromptBuilder:  cfg.Augmentation.UsePromptBuilder,
	}
	return augment.NewGeminiProcessor(&geminiConfig, log), nil
}

func cleanPageNumbers(text string) string {
	return pageNumberRegex.ReplaceAllString(text, "")
}

func (p *Pipeline) ProcessSingle(
	ctx context.Context,
	pngPath, outputPath string,
	opts *augment.AugmentationOptions,
) error {
	startTime := time.Now()
	p.logger.Info("Processing single file: %s -> %s", pngPath, outputPath)
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, defaultDirPermission); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	result := p.processFile(ctx, pngPath, outputPath, opts)
	duration := time.Since(startTime)
	if result.Success {
		p.logger.Success("Processed %s in %v", filepath.Base(pngPath), duration)
	} else {
		p.logger.Error("Failed to process %s: %v", filepath.Base(pngPath), result.Error)
	}
	return result.Error
}

func (p *Pipeline) processFile(
	ctx context.Context,
	pngPath, outputPath string,
	opts *augment.AugmentationOptions,
) ProcessingResult {
	if p.skipExisting {
		if _, err := os.Stat(outputPath); err == nil {
			p.logger.Info("Skipping existing file: %s", filepath.Base(outputPath))
			return ProcessingResult{Success: true}
		}
	}

	p.logger.Info("Running OCR on %s", filepath.Base(pngPath))
	ocrText, err := p.ocrProcessor.ProcessPNG(ctx, pngPath)
	if err != nil {
		return ProcessingResult{Success: false, Error: fmt.Errorf("OCR processing: %w", err)}
	}

	cleanedText := cleanPageNumbers(ocrText)
	finalText := cleanedText

	if p.enableAugment && p.textAugmenter != nil {
		p.logger.Info("Augmenting text for %s", filepath.Base(pngPath))
		augmentedText, err := p.textAugmenter.AugmentTextWithOptions(
			ctx,
			cleanedText,
			pngPath,
			opts,
		)
		if err != nil {
			p.logger.Warn(
				"Text augmentation failed for %s: %v. Using cleaned OCR text as fallback.",
				filepath.Base(pngPath),
				err,
			)
		} else {
			finalText = augmentedText
		}
	}

	if err := os.WriteFile(outputPath, []byte(finalText), defaultFilePermission); err != nil {
		return ProcessingResult{Success: false, Error: fmt.Errorf("write output: %w", err)}
	}

	p.logger.Info(
		"Successfully processed %s -> %s",
		filepath.Base(pngPath),
		filepath.Base(outputPath),
	)
	return ProcessingResult{Success: true}
}

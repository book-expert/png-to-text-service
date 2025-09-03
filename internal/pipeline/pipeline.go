// Package pipeline orchestrates the complete PNG → OCR → Augmentation → Output flow.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nnikolov3/logger"

	"github.com/nnikolov3/png-to-text-service/internal/augment"
	"github.com/nnikolov3/png-to-text-service/internal/config"
	"github.com/nnikolov3/png-to-text-service/internal/ocr"
)

var ErrAPIKeyNotFound = errors.New("API key not found in environment variable")

// OCRProcessor defines the interface for OCR processing.
type OCRProcessor interface {
	ProcessPNG(ctx context.Context, pngPath string) (string, error)
}

// TextAugmenter defines the interface for text augmentation.
type TextAugmenter interface {
	AugmentText(ctx context.Context, ocrText, imagePath string) (string, error)
}

// Pipeline orchestrates the entire PNG processing workflow.
type Pipeline struct {
	ocrProcessor  OCRProcessor
	textAugmenter TextAugmenter
	config        *config.Config
	logger        *logger.Logger
	enableAugment bool
	skipExisting  bool
	workers       int
}

// ProcessingResult represents the result of processing a single PNG file.
type ProcessingResult struct {
	ProcessedAt   time.Time
	Error         error
	PNGPath       string
	OutputPath    string
	OCRText       string
	AugmentedText string
	Success       bool
}

// NewPipeline creates a new processing pipeline with the given configuration.
func NewPipeline(cfg *config.Config, logger *logger.Logger) (*Pipeline, error) {
	// Create OCR processor
	tesseractConfig := ocr.TesseractConfig{
		Language:       cfg.Tesseract.Language,
		OEM:            cfg.Tesseract.OEM,
		PSM:            cfg.Tesseract.PSM,
		DPI:            cfg.Tesseract.DPI,
		TimeoutSeconds: cfg.Tesseract.TimeoutSeconds,
	}
	ocrProcessor := ocr.NewProcessor(tesseractConfig, logger)

	// Create text augmenter if enabled
	var textAugmenter TextAugmenter

	if cfg.Settings.EnableAugmentation {
		apiKey := cfg.GetAPIKey()
		if apiKey == "" {
			return nil, fmt.Errorf(
				"API key not found in environment variable %s: %w",
				cfg.Gemini.APIKeyVariable,
				ErrAPIKeyNotFound,
			)
		}

		// Convert augmentation type string to enum
		var augmentationType augment.AugmentationType

		switch cfg.Augmentation.Type {
		case "commentary":
			augmentationType = augment.AugmentationCommentary
		case "summary":
			augmentationType = augment.AugmentationSummary
		default:
			augmentationType = augment.AugmentationCommentary // Default
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
			AugmentationType:  augmentationType,
			CustomPrompt:      cfg.Augmentation.CustomPrompt,
			UsePromptBuilder:  cfg.Augmentation.UsePromptBuilder,
		}

		textAugmenter = augment.NewGeminiProcessor(geminiConfig, logger)
	}

	return &Pipeline{
		ocrProcessor:  ocrProcessor,
		textAugmenter: textAugmenter,
		config:        cfg,
		logger:        logger,
		enableAugment: cfg.Settings.EnableAugmentation,
		skipExisting:  cfg.Settings.SkipExisting,
		workers:       cfg.Settings.Workers,
	}, nil
}

// ProcessDirectory processes all PNG files in a directory.
func (p *Pipeline) ProcessDirectory(
	ctx context.Context,
	inputDir, outputDir string,
) error {
	startTime := time.Now()

	p.logger.Info(
		"Starting directory processing: input=%s output=%s workers=%d",
		inputDir,
		outputDir,
		p.workers,
	)

	// Find all PNG files
	pngFiles, err := p.findPNGFiles(inputDir)
	if err != nil {
		return fmt.Errorf("find PNG files: %w", err)
	}

	if len(pngFiles) == 0 {
		p.logger.Info("No PNG files found in %s", inputDir)

		return nil
	}

	p.logger.Info("Found %d PNG files to process", len(pngFiles))

	// Ensure output directory exists
	mkdirErr := os.MkdirAll(outputDir, 0o755)
	if mkdirErr != nil {
		return fmt.Errorf("create output directory: %w", mkdirErr)
	}

	// Process files in parallel
	results, err := p.processFilesParallel(ctx, pngFiles, inputDir, outputDir)
	if err != nil {
		return fmt.Errorf("process files: %w", err)
	}

	// Report results
	p.reportResults(results, startTime)

	return nil
}

// ProcessSingle processes a single PNG file.
func (p *Pipeline) ProcessSingle(ctx context.Context, pngPath, outputPath string) error {
	startTime := time.Now()

	p.logger.Info("Processing single file: %s -> %s", pngPath, outputPath)

	// Ensure output directory exists
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Process the file
	result, err := p.processFile(ctx, pngPath, outputPath)
	if err != nil {
		return fmt.Errorf("process file: %w", err)
	}

	// Report result
	duration := time.Since(startTime)
	if result.Success {
		p.logger.Success("Processed %s in %v", filepath.Base(pngPath), duration)
	} else {
		p.logger.Error("Failed to process %s: %v", filepath.Base(pngPath), result.Error)
	}

	return nil
}

// findPNGFiles recursively finds all PNG files in a directory.
func (p *Pipeline) findPNGFiles(dir string) ([]string, error) {
	var pngFiles []string

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		if strings.HasSuffix(strings.ToLower(d.Name()), ".png") {
			pngFiles = append(pngFiles, path)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort files for consistent processing order
	sort.Strings(pngFiles)

	return pngFiles, nil
}

// processFilesParallel processes multiple files using a worker pool.
func (p *Pipeline) processFilesParallel(
	ctx context.Context,
	pngFiles []string,
	inputDir, outputDir string,
) ([]ProcessingResult, error) {
	jobs := make(chan string, len(pngFiles))
	results := make(chan ProcessingResult, len(pngFiles))

	// Start workers
	var wg sync.WaitGroup
	for range p.workers {
		wg.Add(1)

		go p.worker(ctx, &wg, jobs, results, inputDir, outputDir)
	}

	// Send jobs
	for _, pngFile := range pngFiles {
		jobs <- pngFile
	}

	close(jobs)

	// Wait for workers to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var allResults []ProcessingResult
	for result := range results {
		allResults = append(allResults, result)
	}

	return allResults, nil
}

// worker is a worker goroutine that processes PNG files.
func (p *Pipeline) worker(
	ctx context.Context,
	wg *sync.WaitGroup,
	jobs <-chan string,
	results chan<- ProcessingResult,
	inputDir, outputDir string,
) {
	defer wg.Done()

	for pngPath := range jobs {
		// Check for cancellation
		select {
		case <-ctx.Done():
			results <- ProcessingResult{
				ProcessedAt:   time.Time{},
				Error:         ctx.Err(),
				PNGPath:       pngPath,
				OutputPath:    "",
				OCRText:       "",
				AugmentedText: "",
				Success:       false,
			}

			return
		default:
		}

		// Generate output path
		relPath, err := filepath.Rel(inputDir, pngPath)
		if err != nil {
			results <- ProcessingResult{
				ProcessedAt:   time.Time{},
				Error:         fmt.Errorf("calculate relative path: %w", err),
				PNGPath:       pngPath,
				OutputPath:    "",
				OCRText:       "",
				AugmentedText: "",
				Success:       false,
			}

			continue
		}

		// Change extension from .png to .txt
		txtName := strings.TrimSuffix(filepath.Base(relPath), ".png") + ".txt"
		outputPath := filepath.Join(outputDir, filepath.Dir(relPath), txtName)

		// Process the file
		result, err := p.processFile(ctx, pngPath, outputPath)
		if err != nil {
			results <- ProcessingResult{
				ProcessedAt:   time.Time{},
				Error:         err,
				PNGPath:       pngPath,
				OutputPath:    "",
				OCRText:       "",
				AugmentedText: "",
				Success:       false,
			}

			continue
		}

		results <- result
	}
}

// processFile processes a single PNG file through the complete pipeline.
func (p *Pipeline) processFile(
	ctx context.Context,
	pngPath, outputPath string,
) (ProcessingResult, error) {
	result := ProcessingResult{
		ProcessedAt:   time.Now(),
		Error:         nil,
		PNGPath:       pngPath,
		OutputPath:    outputPath,
		OCRText:       "",
		AugmentedText: "",
		Success:       false,
	}

	// Check if output already exists and we should skip
	if p.skipExisting {
		if _, err := os.Stat(outputPath); err == nil {
			p.logger.Info(
				"Skipping existing file: %s",
				filepath.Base(outputPath),
			)

			result.Success = true

			return result, nil
		}
	}

	// Step 1: OCR Processing
	p.logger.Info("Running OCR on %s", filepath.Base(pngPath))

	ocrText, err := p.ocrProcessor.ProcessPNG(ctx, pngPath)
	if err != nil {
		result.Error = fmt.Errorf("OCR processing: %w", err)

		return result, nil
	}

	result.OCRText = ocrText

	// Step 2: Text Augmentation (if enabled)
	finalText := ocrText

	if p.enableAugment && p.textAugmenter != nil {
		p.logger.Info("Augmenting text for %s", filepath.Base(pngPath))

		augmentedText, err := p.textAugmenter.AugmentText(ctx, ocrText, pngPath)
		if err != nil {
			p.logger.Warn(
				"Text augmentation failed for %s: %v",
				filepath.Base(pngPath),
				err,
			)
			// Continue with OCR text only
		} else {
			finalText = augmentedText
			result.AugmentedText = augmentedText
		}
	}

	// Step 3: Write output
	if err := p.writeOutput(outputPath, finalText); err != nil {
		result.Error = fmt.Errorf("write output: %w", err)

		return result, nil
	}

	result.Success = true

	p.logger.Info(
		"Successfully processed %s -> %s",
		filepath.Base(pngPath),
		filepath.Base(outputPath),
	)

	return result, nil
}

// writeOutput writes the final text to the output file.
func (p *Pipeline) writeOutput(outputPath, text string) error {
	// Ensure the directory exists
	dir := filepath.Dir(outputPath)

	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Write the file
	err = os.WriteFile(
		outputPath,
		[]byte(text),
		0o644,
	)
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// reportResults logs summary statistics about the processing results.
func (p *Pipeline) reportResults(results []ProcessingResult, startTime time.Time) {
	duration := time.Since(startTime)
	successful := 0
	failed := 0

	for _, result := range results {
		if result.Success {
			successful++
		} else {
			failed++

			if result.Error != nil {
				p.logger.Error("Failed %s: %v", filepath.Base(result.PNGPath), result.Error)
			}
		}
	}

	total := len(results)
	p.logger.Success(
		"Processing complete: %d/%d successful, %d failed in %v",
		successful,
		total,
		failed,
		duration,
	)

	if successful > 0 {
		avgTime := duration / time.Duration(successful)
		p.logger.Info("Average time per successful file: %v", avgTime)
	}
}

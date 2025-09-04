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

const (
	// File permissions.
	defaultFilePermission = 0o600
	defaultDirPermission  = 0o750
)

var (
	// ErrAPIKeyNotFound indicates that the required API key was not found
	// in the environment variable.
	ErrAPIKeyNotFound = errors.New("API key not found in environment variable")
	// ErrAugmentationDisabled indicates that text augmentation is disabled.
	ErrAugmentationDisabled = errors.New("text augmentation is disabled")
)

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

// newOCRProcessorFromConfig creates an OCR processor from the given configuration.
// Returns a concrete struct pointer to the struct.
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

// It now returns a concrete *augment.GeminiProcessor to be more explicit,
// following the Go best practice "Accept interfaces, return structs.".
func newTextAugmenterFromConfig(
	cfg *config.Config,
	log *logger.Logger,
) (*augment.GeminiProcessor, error) {
	// Check if augmentation is enabled
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

	augmentationType := parseAugmentationType(cfg.Augmentation.Type)

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

	return augment.NewGeminiProcessor(&geminiConfig, log), nil
}

// parseAugmentationType converts augmentation type string to enum.
func parseAugmentationType(augmentType string) augment.AugmentationType {
	switch augmentType {
	case "commentary":
		return augment.AugmentationCommentary
	case "summary":
		return augment.AugmentationSummary
	default:
		return augment.AugmentationCommentary // Default
	}
}

// ProcessDirectory processes all PNG files in a directory.
func (p *Pipeline) ProcessDirectory(
	ctx context.Context,
	inputDir, outputDir string,
) error {
	// Start time
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
	mkdirErr := os.MkdirAll(outputDir, defaultDirPermission)
	if mkdirErr != nil {
		return fmt.Errorf("create output directory: %w", mkdirErr)
	}

	// Process files in parallel
	results := p.processFilesParallel(ctx, pngFiles, inputDir, outputDir)

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

	err := os.MkdirAll(outputDir, defaultDirPermission)
	if err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Process the file
	result := p.processFile(ctx, pngPath, outputPath)

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

	err := filepath.WalkDir(
		dir,
		func(path string, dirEntry os.DirEntry, err error) error {
			return p.handleWalkDirEntry(path, dirEntry, err, &pngFiles)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("walking directory %s: %w", dir, err)
	}

	sort.Strings(pngFiles)

	return pngFiles, nil
}

// handleWalkDirEntry processes a single directory entry during file walking.
func (p *Pipeline) handleWalkDirEntry(
	path string,
	dirEntry os.DirEntry,
	err error,
	pngFiles *[]string,
) error {
	// Handle error
	if err != nil {
		return err
	}

	if dirEntry.IsDir() {
		return nil
	}

	if p.isPNGFile(dirEntry.Name()) {
		*pngFiles = append(*pngFiles, path)
	}

	return nil
}

// isPNGFile checks if a filename is a PNG file.
func (p *Pipeline) isPNGFile(filename string) bool {
	return strings.HasSuffix(strings.ToLower(filename), ".png")
}

// processFilesParallel processes multiple files using a worker pool.
func (p *Pipeline) processFilesParallel(
	ctx context.Context,
	pngFiles []string,
	inputDir, outputDir string,
) []ProcessingResult {
	// Initialize channels
	jobs := make(chan string, len(pngFiles))
	results := make(chan ProcessingResult, len(pngFiles))

	// Start workers
	var waitGroup sync.WaitGroup
	for range p.workers {
		waitGroup.Add(1)

		go p.worker(ctx, &waitGroup, jobs, results, inputDir, outputDir)
	}

	// Send jobs
	for _, pngFile := range pngFiles {
		jobs <- pngFile
	}

	close(jobs)

	// Wait for workers to complete
	go func() {
		waitGroup.Wait()
		close(results)
	}()

	// Collect results
	allResults := make([]ProcessingResult, 0, len(pngFiles))
	for result := range results {
		allResults = append(allResults, result)
	}

	return allResults
}

// worker is a worker goroutine that processes PNG files.
func (p *Pipeline) worker(
	ctx context.Context,
	waitGroup *sync.WaitGroup,
	jobs <-chan string,
	results chan<- ProcessingResult,
	inputDir, outputDir string,
) {
	// Defer
	defer waitGroup.Done()

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
		result := p.processFile(ctx, pngPath, outputPath)

		results <- result
	}
}

// processFile processes a single PNG file through the complete pipeline.
func (p *Pipeline) processFile(
	ctx context.Context,
	pngPath, outputPath string,
) ProcessingResult {
	// Initialize result
	result := p.initProcessingResult(pngPath, outputPath)

	if p.shouldSkipExistingFile(outputPath) {
		result.Success = true

		return result
	}

	err := p.performOCR(ctx, pngPath, &result)
	if err != nil {
		return result
	}

	finalText := p.performAugmentation(ctx, pngPath, &result)

	err = p.writeOutput(outputPath, finalText)
	if err != nil {
		result.Error = fmt.Errorf("write output: %w", err)

		return result
	}

	result.Success = true

	p.logger.Info(
		"Successfully processed %s -> %s",
		filepath.Base(pngPath),
		filepath.Base(outputPath),
	)

	return result
}

// initProcessingResult creates and initializes a ProcessingResult.
func (p *Pipeline) initProcessingResult(pngPath, outputPath string) ProcessingResult {
	return ProcessingResult{
		ProcessedAt:   time.Now(),
		Error:         nil,
		PNGPath:       pngPath,
		OutputPath:    outputPath,
		OCRText:       "",
		AugmentedText: "",
		Success:       false,
	}
}

// shouldSkipExistingFile checks if we should skip processing an existing file.
func (p *Pipeline) shouldSkipExistingFile(outputPath string) bool {
	if !p.skipExisting {
		return false
	}

	_, err := os.Stat(outputPath)
	if err == nil {
		p.logger.Info("Skipping existing file: %s", filepath.Base(outputPath))

		return true
	}

	return false
}

// performOCR runs OCR processing and updates the result.
func (p *Pipeline) performOCR(
	ctx context.Context,
	pngPath string,
	result *ProcessingResult,
) error {
	// Update logger
	p.logger.Info("Running OCR on %s", filepath.Base(pngPath))

	ocrText, err := p.ocrProcessor.ProcessPNG(ctx, pngPath)
	if err != nil {
		result.Error = fmt.Errorf("OCR processing: %w", err)

		return fmt.Errorf("OCR processing failed for %s: %w", pngPath, err)
	}

	result.OCRText = ocrText

	return nil
}

// performAugmentation runs text augmentation if enabled and returns final text.
func (p *Pipeline) performAugmentation(
	ctx context.Context,
	pngPath string,
	result *ProcessingResult,
) string {
	// Check if augmentation is enabled
	if !p.enableAugment || p.textAugmenter == nil {
		return result.OCRText
	}

	p.logger.Info("Augmenting text for %s", filepath.Base(pngPath))

	augmentedText, err := p.textAugmenter.AugmentText(ctx, result.OCRText, pngPath)
	if err != nil {
		p.logger.Warn(
			"Text augmentation failed for %s: %v",
			filepath.Base(pngPath),
			err,
		)

		return result.OCRText
	}

	result.AugmentedText = augmentedText

	return augmentedText
}

// writeOutput writes the final text to the output file.
func (p *Pipeline) writeOutput(outputPath, text string) error {
	// Ensure the directory exists
	dir := filepath.Dir(outputPath)

	err := os.MkdirAll(dir, defaultDirPermission)
	if err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Write the file
	err = os.WriteFile(
		outputPath,
		[]byte(text),
		defaultFilePermission,
	)
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// reportResults logs summary statistics about the processing results.
func (p *Pipeline) reportResults(results []ProcessingResult, startTime time.Time) {
	duration := time.Since(startTime)
	successful, failed := p.countResults(results)

	p.logSummary(successful, len(results), failed, duration)
	p.logAverageTime(successful, duration)
}

// countResults counts successful and failed processing results.
func (p *Pipeline) countResults(results []ProcessingResult) (successful, failed int) {
	for i := range results {
		result := &results[i]
		if result.Success {
			successful++
		} else {
			failed++

			p.logFailedResult(result)
		}
	}

	return successful, failed
}

// logFailedResult logs details about a failed processing result.
func (p *Pipeline) logFailedResult(result *ProcessingResult) {
	if result.Error != nil {
		p.logger.Error(
			"Failed %s: %v",
			filepath.Base(result.PNGPath),
			result.Error,
		)
	}
}

// logSummary logs the overall processing summary.
func (p *Pipeline) logSummary(successful, total, failed int, duration time.Duration) {
	p.logger.Success(
		"Processing complete: %d/%d successful, %d failed in %v",
		successful,
		total,
		failed,
		duration,
	)
}

// logAverageTime logs the average processing time per successful file.
func (p *Pipeline) logAverageTime(successful int, duration time.Duration) {
	if successful > 0 {
		avgTime := duration / time.Duration(successful)
		p.logger.Info("Average time per successful file: %v", avgTime)
	}
}

// Package ocr provides OCR functionality using Tesseract.
package ocr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/book-expert/logger"
)

var (
	// ErrInvalidExtension indicates that the file does not have a .png extension.
	ErrInvalidExtension = errors.New("file must have .png extension")
	// ErrPathIsDirectory indicates that the provided path is a directory, not a file.
	ErrPathIsDirectory = errors.New("path is a directory")
	// ErrFileEmpty indicates that the file is empty.
	ErrFileEmpty = errors.New("file is empty")
	// ErrOCRResultEmpty indicates that the OCR processing returned an empty result.
	ErrOCRResultEmpty = errors.New("empty OCR result")
)

// TesseractConfig holds configuration parameters for Tesseract OCR engine.
// These parameters directly control Tesseract's behavior and output quality.
type TesseractConfig struct {
	// Language specifies the OCR language model to use (e.g., "eng", "fra", "deu").
	// Multiple languages can be specified with "+" separator (e.g., "eng+fra").
	Language string

	// OEM (OCR Engine Mode) controls which OCR engine to use:
	// 0 = Legacy engine only
	// 1 = Neural nets LSTM engine only
	// 2 = Legacy + LSTM engines
	// 3 = Default (based on what is available)
	OEM int

	// PSM (Page Segmentation Mode) determines how Tesseract segments the page:
	// 0 = Orientation and script detection only
	// 1 = Automatic page segmentation with OSD
	// 3 = Fully automatic page segmentation (default)
	// 6 = Uniform block of text
	// 13 = Raw line. Treat the image as a single text line
	PSM int

	// DPI specifies the dots per inch of the input image.
	// Higher DPI generally improves OCR accuracy but increases processing time.
	// Common values: 150 (fast), 300 (standard), 600 (high quality)
	DPI int

	// TimeoutSeconds sets the maximum time allowed for OCR processing per image.
	// Prevents hung processes from blocking the pipeline indefinitely.
	TimeoutSeconds int
}

// Processor implements OCR processing using the Tesseract OCR engine.
// It provides a high-level interface for converting PNG images to text
// with comprehensive error handling, validation, and text cleaning.
//
// The processor handles the complete OCR workflow:
// - PNG file validation and preprocessing
// - Tesseract process execution with configured parameters
// - Output text extraction and basic normalization
// - Error recovery and retry logic for transient failures
//
// All processing is performed using external Tesseract binaries,
// requiring Tesseract to be installed and accessible via PATH.
type Processor struct {
	logger *logger.Logger
	config TesseractConfig
}

// NewProcessor creates a new Tesseract OCR processor with the specified configuration.
// The processor is immediately ready for use and will validate configuration
// parameters during the first OCR operation.
//
// The logger is used for detailed processing information, performance metrics,
// and error reporting. All OCR operations will be logged for troubleshooting
// and monitoring purposes.
func NewProcessor(config TesseractConfig, log *logger.Logger) *Processor {
	return &Processor{
		config: config,
		logger: log,
	}
}

// ProcessPNG performs OCR on a PNG file and returns cleaned text.
func (p *Processor) ProcessPNG(ctx context.Context, pngPath string) (string, error) {
	err := p.validateFile(pngPath)
	if err != nil {
		return "", fmt.Errorf("validate PNG file: %w", err)
	}

	text, err := p.runTesseract(ctx, pngPath)
	if err != nil {
		return "", fmt.Errorf("run tesseract: %w", err)
	}

	cleanedText := p.cleanOCRText(text)
	if strings.TrimSpace(cleanedText) == "" {
		return "", fmt.Errorf(
			"empty OCR result for %s: %w",
			pngPath,
			ErrOCRResultEmpty,
		)
	}

	return cleanedText, nil
}

// validateFile checks if the PNG file exists and is readable.
func (p *Processor) validateFile(pngPath string) error {
	if !strings.HasSuffix(strings.ToLower(pngPath), ".png") {
		return fmt.Errorf(
			"file must have .png extension %s: %w",
			pngPath,
			ErrInvalidExtension,
		)
	}

	info, err := os.Stat(pngPath)
	if err != nil {
		return fmt.Errorf("access file: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf(
			"path is a directory %s: %w",
			pngPath,
			ErrPathIsDirectory,
		)
	}

	if info.Size() == 0 {
		return fmt.Errorf("file is empty %s: %w", pngPath, ErrFileEmpty)
	}

	return nil
}

// runTesseract executes Tesseract OCR on the specified PNG file.
func (p *Processor) runTesseract(ctx context.Context, pngPath string) (string, error) {
	timeout := time.Duration(p.config.TimeoutSeconds) * time.Second

	tesseractCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cleanedPngPath := filepath.Clean(pngPath)

	cmd := exec.CommandContext(tesseractCtx, "tesseract")
	cmd.Args = append(cmd.Args, cleanedPngPath)
	cmd.Args = append(cmd.Args, "stdout")
	cmd.Args = append(cmd.Args, "-l", p.config.Language)
	cmd.Args = append(cmd.Args, "--dpi", strconv.Itoa(p.config.DPI))
	cmd.Args = append(cmd.Args, "--oem", strconv.Itoa(p.config.OEM))
	cmd.Args = append(cmd.Args, "--psm", strconv.Itoa(p.config.PSM))

	// Set environment variables to limit threading for parallel processing
	cmd.Env = append(os.Environ(),
		"OMP_NUM_THREADS=1",
		"OPENBLAS_NUM_THREADS=1",
		"MKL_NUM_THREADS=1",
		"NUMEXPR_NUM_THREADS=1",
	)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Try retry with more permissive PSM on timeout
		if errors.Is(tesseractCtx.Err(), context.DeadlineExceeded) {
			p.logger.Warn(
				"Tesseract timeout for %s, retrying with PSM=6",
				filepath.Base(pngPath),
			)

			return p.retryTesseract(ctx, pngPath)
		}

		return "", fmt.Errorf(
			"tesseract execution failed: %w (stderr: %s)",
			err,
			stderr.String(),
		)
	}

	return stdout.String(), nil
}

// retryTesseract retries OCR with a more permissive PSM setting.
func (p *Processor) retryTesseract(ctx context.Context, pngPath string) (string, error) {
	timeout := time.Duration(p.config.TimeoutSeconds) * time.Second

	retryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cleanedPngPath := filepath.Clean(pngPath)

	cmd := exec.CommandContext(retryCtx, "tesseract")
	cmd.Args = append(cmd.Args, cleanedPngPath)
	cmd.Args = append(cmd.Args, "stdout")
	cmd.Args = append(cmd.Args, "-l", p.config.Language)
	cmd.Args = append(cmd.Args, "--dpi", strconv.Itoa(p.config.DPI))
	cmd.Args = append(cmd.Args, "--oem", strconv.Itoa(p.config.OEM))
	cmd.Args = append(cmd.Args, "--psm", "6")

	cmd.Env = append(os.Environ(),
		"OMP_NUM_THREADS=1",
		"OPENBLAS_NUM_THREADS=1",
		"MKL_NUM_THREADS=1",
		"NUMEXPR_NUM_THREADS=1",
	)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf(
			"tesseract retry failed: %w (stderr: %s)",
			err,
			stderr.String(),
		)
	}

	return stdout.String(), nil
}

// cleanOCRText applies various cleaning operations to improve OCR text quality.
func (p *Processor) cleanOCRText(text string) string {
	if text == "" {
		return text
	}

	cleaner := NewCleaner()

	return cleaner.Clean(text)
}

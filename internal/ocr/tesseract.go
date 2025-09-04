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

	"github.com/nnikolov3/logger"
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

// TesseractConfig holds configuration for Tesseract OCR.
type TesseractConfig struct {
	Language       string
	OEM            int
	PSM            int
	DPI            int
	TimeoutSeconds int
}

// Processor implements OCR processing using Tesseract.
type Processor struct {
	logger *logger.Logger
	config TesseractConfig
}

// NewProcessor creates a new Tesseract OCR processor.
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

	args := []string{
		pngPath,
		"stdout",
		"-l", p.config.Language,
		"--dpi", strconv.Itoa(p.config.DPI),
		"--oem", strconv.Itoa(p.config.OEM),
		"--psm", strconv.Itoa(p.config.PSM),
	}

	cmd := exec.CommandContext(tesseractCtx, "tesseract", args...)

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

	args := []string{
		pngPath,
		"stdout",
		"-l", p.config.Language,
		"--dpi", strconv.Itoa(p.config.DPI),
		"--oem", strconv.Itoa(p.config.OEM),
		"--psm", "6", // More permissive PSM
	}

	cmd := exec.CommandContext(retryCtx, "tesseract", args...)

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

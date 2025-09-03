// PNG-to-text-service: Unified PNG → OCR → AI Augmentation pipeline
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nnikolov3/logger"

	"github.com/nnikolov3/png-to-text-service/internal/config"
	"github.com/nnikolov3/png-to-text-service/internal/pipeline"
)

func main() {
	// Parse command line flags
	var (
		configPathFlag = flag.String(
			"config",
			"",
			"Path to configuration file (default: find project.toml)",
		)
		inputDirFlag = flag.String(
			"input",
			"",
			"Input directory containing PNG files (overrides config)",
		)
		outputDirFlag = flag.String(
			"output",
			"",
			"Output directory for text files (overrides config)",
		)
		singleFileFlag = flag.String(
			"file",
			"",
			"Process single PNG file instead of directory",
		)
		workersFlag = flag.Int(
			"workers",
			0,
			"Number of worker goroutines (overrides config)",
		)
		noAugmentFlag = flag.Bool(
			"no-augment",
			false,
			"Disable AI text augmentation",
		)
		versionFlag = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()

	// Handle version flag
	if *versionFlag {
		fmt.Println("png-to-text-service version 1.0.0")
		os.Exit(0)
	}

	// Load configuration
	cfg, err := loadConfiguration(*configPathFlag)
	if err != nil {
		fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	loggerInstance, err := initializeLogger(cfg)
	if err != nil {
		fatalf("Failed to initialize logger: %v", err)
	}

	defer func() {
		closeErr := loggerInstance.Close()
		if closeErr != nil {
			fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", closeErr)
		}
	}()

	loggerInstance.Info("PNG-to-text-service starting up - version 1.0.0")

	// Apply command line overrides
	applyCommandLineOverrides(
		cfg,
		*inputDirFlag,
		*outputDirFlag,
		*workersFlag,
		*noAugmentFlag,
	)

	// Ensure directories exist
	if err := cfg.EnsureDirectories(); err != nil {
		fatalf("Failed to ensure directories: %v", err)
	}

	// Create pipeline
	processingPipeline, err := pipeline.NewPipeline(cfg, loggerInstance)
	if err != nil {
		fatalf("Failed to create pipeline: %v", err)
	}

	// Set up context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	setupSignalHandling(cancel, loggerInstance)

	// Process files
	if *singleFileFlag != "" {
		err = processSingleFile(
			ctx,
			processingPipeline,
			*singleFileFlag,
			cfg.Paths.OutputDir,
			loggerInstance,
		)
	} else {
		err = processDirectory(ctx, processingPipeline, cfg.Paths.InputDir, cfg.Paths.OutputDir, loggerInstance)
	}

	if err != nil {
		loggerInstance.Error("Processing failed: %v", err)
		os.Exit(1)
	}

	loggerInstance.Success("PNG-to-text-service completed successfully")
}

// loadConfiguration loads the configuration from the specified path or finds it
// automatically.
func loadConfiguration(configPath string) (*config.Config, error) {
	if configPath == "" {
		// Find configuration automatically
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}

		_, configPath, err = config.FindProjectRoot(wd)
		if err != nil {
			return nil, fmt.Errorf("find project configuration: %w", err)
		}
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("load configuration from %s: %w", configPath, err)
	}

	return cfg, nil
}

// initializeLogger creates and configures the logger instance.
func initializeLogger(cfg *config.Config) (*logger.Logger, error) {
	logFileName := fmt.Sprintf(
		"png-to-text-service_%s.log",
		time.Now().Format("2006-01-02_15-04-05"),
	)

	loggerInstance, err := logger.New(cfg.Logging.Dir, logFileName)
	if err != nil {
		return nil, fmt.Errorf("create logger: %w", err)
	}

	return loggerInstance, nil
}

// applyCommandLineOverrides applies command line flag overrides to the configuration.
func applyCommandLineOverrides(
	cfg *config.Config,
	inputDir, outputDir string,
	workers int,
	noAugment bool,
) {
	if inputDir != "" {
		cfg.Paths.InputDir = inputDir
	}

	if outputDir != "" {
		cfg.Paths.OutputDir = outputDir
	}

	if workers > 0 {
		cfg.Settings.Workers = workers
	}

	if noAugment {
		cfg.Settings.EnableAugmentation = false
	}
}

// setupSignalHandling configures graceful shutdown on SIGINT and SIGTERM.
func setupSignalHandling(cancel context.CancelFunc, logger *logger.Logger) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("Received signal %v, initiating graceful shutdown...", sig)
		cancel()
	}()
}

// processSingleFile processes a single PNG file.
func processSingleFile(
	ctx context.Context,
	pipeline *pipeline.Pipeline,
	pngFile, outputDir string,
	logger *logger.Logger,
) error {
	if !filepath.IsAbs(pngFile) {
		abs, err := filepath.Abs(pngFile)
		if err != nil {
			return fmt.Errorf(
				"resolve absolute path for %s: %w",
				pngFile,
				err,
			)
		}

		pngFile = abs
	}

	// Generate output path
	baseName := filepath.Base(pngFile)
	txtName := baseName[:len(baseName)-len(filepath.Ext(baseName))] + ".txt"
	outputPath := filepath.Join(outputDir, txtName)

	logger.Info("Processing single file: %s -> %s", pngFile, outputPath)

	return pipeline.ProcessSingle(ctx, pngFile, outputPath)
}

// processDirectory processes all PNG files in a directory.
func processDirectory(
	ctx context.Context,
	pipeline *pipeline.Pipeline,
	inputDir, outputDir string,
	logger *logger.Logger,
) error {
	if inputDir == "" || outputDir == "" {
		return errors.New("input and output directories must be specified")
	}

	// Validate input directory exists
	if _, err := os.Stat(inputDir); err != nil {
		return fmt.Errorf("input directory %s: %w", inputDir, err)
	}

	logger.Info("Processing directory: %s -> %s", inputDir, outputDir)

	return pipeline.ProcessDirectory(ctx, inputDir, outputDir)
}

// fatalf prints an error message and exits with code 1.
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}

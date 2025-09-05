// ./cmd/png-to-text-service/main.go
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

var (
	ErrDirectoriesRequired = errors.New(
		"input and output directories must be specified",
	)
	// osGetwd is a package-level variable to allow mocking os.Getwd in tests.
	//nolint:gochecknoglobals // This global variable is necessary for mocking
	// OS-level functions in tests.
	osGetwd = os.Getwd
	// fatalf is a package-level variable to allow mocking os.Exit calls in tests.
	//nolint:gochecknoglobals // This global variable is necessary for mocking
	// process termination in tests.
	fatalf = func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
		os.Exit(1)
	}
)

// pipelineProcessor defines the interface for the processing pipeline,
// allowing for mock implementations in tests.
type pipelineProcessor interface {
	ProcessSingle(ctx context.Context, pngFile, outputPath string) error
	ProcessDirectory(ctx context.Context, inputDir, outputDir string) error
}

// logProvider defines the interface for the logger, allowing for mock
// implementations in tests.
type logProvider interface {
	Info(format string, args ...any)
	Error(format string, args ...any)
	Success(format string, args ...any)
	Close() error
}

func main() {
	flags := parseCommandLineFlags()

	if flags.versionFlag {
		printVersionAndExit()
	}

	cfg, loggerImpl := initializeApplication(flags.configPathFlag)

	var loggerInstance logProvider = loggerImpl
	defer closeLogger(loggerInstance)

	applyCommandLineOverrides(
		cfg,
		flags.inputDirFlag,
		flags.outputDirFlag,
		flags.workersFlag,
		flags.noAugmentFlag,
		flags.promptFlag,
		flags.augmentationTypeFlag,
	)

	ctx := setupApplicationContext(loggerInstance)

	var processingPipeline pipelineProcessor = createPipeline(cfg, loggerImpl)

	runProcessing(ctx, processingPipeline, cfg, flags.singleFileFlag, loggerInstance)
}

// commandLineFlags holds all command line flag values.
type commandLineFlags struct {
	configPathFlag       string
	inputDirFlag         string
	outputDirFlag        string
	singleFileFlag       string
	promptFlag           string
	augmentationTypeFlag string
	workersFlag          int
	noAugmentFlag        bool
	versionFlag          bool
}

// parseCommandLineFlags parses and returns command line flags.
func parseCommandLineFlags() commandLineFlags {
	var flags commandLineFlags

	flag.StringVar(
		&flags.configPathFlag,
		"config",
		"",
		"Path to configuration file (default: find project.toml)",
	)
	flag.StringVar(
		&flags.inputDirFlag,
		"input",
		"",
		"Input directory containing PNG files (overrides config)",
	)
	flag.StringVar(
		&flags.outputDirFlag,
		"output",
		"",
		"Output directory for text files (overrides config)",
	)
	flag.StringVar(
		&flags.singleFileFlag,
		"file",
		"",
		"Process single PNG file instead of directory",
	)
	flag.IntVar(
		&flags.workersFlag,
		"workers",
		0,
		"Number of worker goroutines (overrides config)",
	)
	flag.BoolVar(
		&flags.noAugmentFlag,
		"no-augment",
		false,
		"Disable AI text augmentation",
	)
	flag.StringVar(
		&flags.promptFlag,
		"prompt",
		"",
		"Custom prompt for AI text augmentation (overrides config)",
	)
	flag.StringVar(
		&flags.augmentationTypeFlag,
		"augmentation-type",
		"",
		"Augmentation type: 'commentary' or 'summary' (overrides config)",
	)
	flag.BoolVar(&flags.versionFlag, "version", false, "Print version and exit")

	flag.Parse()

	return flags
}

// printVersionAndExit prints version information and exits.
func printVersionAndExit() {
	_, _ = fmt.Fprintln(os.Stdout, "png-to-text-service version 1.0.0")
	os.Exit(0)
}

// initializeApplication loads config and initializes logger.
func initializeApplication(configPath string) (*config.Config, *logger.Logger) {
	cfg, err := loadConfiguration(configPath)
	if err != nil {
		fatalf("Failed to load configuration: %v", err)
	}

	loggerInstance, err := initializeLogger(cfg)
	if err != nil {
		fatalf("Failed to initialize logger: %v", err)
	}

	loggerInstance.Info("PNG-to-text-service starting up - version 1.0.0")

	return cfg, loggerInstance
}

// closeLogger safely closes the logger instance.
func closeLogger(loggerInstance logProvider) {
	closeErr := loggerInstance.Close()
	if closeErr != nil {
		fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", closeErr)
	}
}

// setupApplicationContext creates context and signal handling.
func setupApplicationContext(loggerInstance logProvider) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	setupSignalHandling(cancel, loggerInstance)

	return ctx
}

// createPipeline creates and configures the processing pipeline.
func createPipeline(
	cfg *config.Config,
	loggerInstance *logger.Logger,
) *pipeline.Pipeline {
	dirErr := cfg.EnsureDirectories()
	if dirErr != nil {
		fatalf("Failed to ensure directories: %v", dirErr)
	}

	processingPipeline, err := pipeline.NewPipeline(cfg, loggerInstance)
	if err != nil {
		fatalf("Failed to create pipeline: %v", err)
	}

	return processingPipeline
}

// runProcessing executes the main processing logic.
func runProcessing(
	ctx context.Context,
	processingPipeline pipelineProcessor,
	cfg *config.Config,
	singleFile string,
	loggerInstance logProvider,
) {
	var err error

	if singleFile != "" {
		err = processSingleFile(
			ctx,
			processingPipeline,
			singleFile,
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
	finalConfigPath, err := resolveConfigPath(configPath)
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(finalConfigPath)
	if err != nil {
		return nil, fmt.Errorf(
			"load configuration from %s: %w",
			finalConfigPath,
			err,
		)
	}

	return cfg, nil
}

// resolveConfigPath determines the final configuration file path.
func resolveConfigPath(configPath string) (string, error) {
	if configPath != "" {
		return configPath, nil
	}

	wd, err := osGetwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	_, foundConfigPath, err := config.FindProjectRoot(wd)
	if err != nil {
		return "", fmt.Errorf("find project configuration: %w", err)
	}

	return foundConfigPath, nil
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
	customPrompt, augmentationType string,
) {
	applyPathOverrides(cfg, inputDir, outputDir)
	applySettingsOverrides(cfg, workers, noAugment)
	applyAugmentationOverrides(cfg, customPrompt, augmentationType)
}

// applyPathOverrides applies path-related command line overrides.
func applyPathOverrides(cfg *config.Config, inputDir, outputDir string) {
	if inputDir != "" {
		cfg.Paths.InputDir = inputDir
	}

	if outputDir != "" {
		cfg.Paths.OutputDir = outputDir
	}
}

// applySettingsOverrides applies settings-related command line overrides.
func applySettingsOverrides(cfg *config.Config, workers int, noAugment bool) {
	if workers > 0 {
		cfg.Settings.Workers = workers
	}

	if noAugment {
		cfg.Settings.EnableAugmentation = false
	}
}

// applyAugmentationOverrides applies augmentation-related command line overrides.
func applyAugmentationOverrides(
	cfg *config.Config,
	customPrompt, augmentationType string,
) {
	if customPrompt != "" {
		cfg.Augmentation.CustomPrompt = customPrompt
	}

	if augmentationType != "" {
		validateAndSetAugmentationType(cfg, augmentationType)
	}
}

// validateAndSetAugmentationType validates and sets the augmentation type.
func validateAndSetAugmentationType(cfg *config.Config, augmentationType string) {
	if augmentationType == "commentary" || augmentationType == "summary" {
		cfg.Augmentation.Type = augmentationType
	} else {
		fatalf("Invalid augmentation type: %s. Must be 'commentary' or 'summary'", augmentationType)
	}
}

// setupSignalHandling configures graceful shutdown on SIGINT and SIGTERM.
func setupSignalHandling(cancel context.CancelFunc, log logProvider) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Info("Received signal %v, initiating graceful shutdown...", sig)
		cancel()
	}()
}

// processSingleFile processes a single PNG file.
func processSingleFile(
	ctx context.Context,
	processingPipeline pipelineProcessor,
	pngFile, outputDir string,
	log logProvider,
) error {
	var err error

	if !filepath.IsAbs(pngFile) {
		var abs string

		abs, err = filepath.Abs(pngFile)
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

	log.Info("Processing single file: %s -> %s", pngFile, outputPath)

	err = processingPipeline.ProcessSingle(ctx, pngFile, outputPath)
	if err != nil {
		return fmt.Errorf("processing single file %s: %w", pngFile, err)
	}

	return nil
}

// processDirectory processes all PNG files in a directory.
func processDirectory(
	ctx context.Context,
	processingPipeline pipelineProcessor,
	inputDir, outputDir string,
	log logProvider,
) error {
	if inputDir == "" || outputDir == "" {
		return ErrDirectoriesRequired
	}

	// Validate input directory exists
	_, err := os.Stat(inputDir)
	if err != nil {
		return fmt.Errorf("input directory %s: %w", inputDir, err)
	}

	log.Info("Processing directory: %s -> %s", inputDir, outputDir)

	err = processingPipeline.ProcessDirectory(ctx, inputDir, outputDir)
	if err != nil {
		return fmt.Errorf("processing directory %s: %w", inputDir, err)
	}

	return nil
}

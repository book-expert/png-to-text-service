# PNG-to-Text Service

A unified PNG-to-text processing service that combines Tesseract OCR with AI-powered text augmentation for high-quality text extraction and enhancement.

## Features

- **Tesseract OCR Processing**: High-performance parallel PNG-to-text extraction
- **AI Text Augmentation**: Enhances OCR text using Gemini AI models with image context
- **Unified Pipeline**: Single application for complete PNG → OCR → Enhanced Text flow
- **Parallel Processing**: Configurable worker pools for optimal performance
- **Comprehensive Configuration**: TOML-based configuration with sensible defaults
- **Robust Error Handling**: Graceful degradation and detailed logging

## Architecture

The service is built with clean architecture principles:

```
cmd/png-to-text-service/    - Main application entry point
internal/
├── config/                 - Configuration management
├── ocr/                    - Tesseract OCR processing
├── augment/                - AI text augmentation (Gemini)
├── pipeline/               - Unified processing pipeline
```

## Installation

### Prerequisites

- Go 1.25+
- Tesseract OCR installed and available in PATH
- Google Gemini API key (if using AI augmentation)

### Build

```bash
git clone <repository-url>
cd png-to-text-service
make build
```

## Configuration

The service uses `project.toml` for configuration. Key settings:

- **Paths**: Input/output directories
- **Tesseract**: OCR engine settings (language, OEM, PSM, DPI)
- **Gemini**: AI augmentation settings (models, retries, temperature)
- **Settings**: Workers, timeouts, feature toggles

See `project.toml` for complete configuration options.

## Usage

### Basic Usage

Process all PNG files in a directory:
```bash
./bin/png-to-text-service -input ./images -output ./text
```

### Single File Processing

Process a single PNG file:
```bash
./bin/png-to-text-service -file ./image.png -output ./output
```

### Disable AI Augmentation

Use OCR only (faster, no API key required):
```bash
./bin/png-to-text-service -input ./images -output ./text -no-augment
```

### Custom Configuration

Use a specific configuration file:
```bash
./bin/png-to-text-service -config ./custom-config.toml
```

## Command Line Options

- `-config string`: Path to configuration file
- `-input string`: Input directory (overrides config)
- `-output string`: Output directory (overrides config)  
- `-file string`: Process single PNG file
- `-workers int`: Number of worker goroutines (overrides config)
- `-no-augment`: Disable AI text augmentation
- `-version`: Print version and exit

## Environment Variables

- `GEMINI_API_KEY`: Your Google Gemini API key (required for AI augmentation)

## Performance

The service is optimized for high-throughput processing:

- Parallel OCR processing with configurable worker pools
- Efficient memory usage with streaming text processing
- Comprehensive text cleaning and normalization
- Graceful timeout handling and retry logic

## Development

### Requirements

- golangci-lint for comprehensive linting
- Standard Go testing tools

### Commands

```bash
make help              # Show all available commands
make check             # Run comprehensive quality checks
make test-coverage     # Generate test coverage report
make build             # Build the application
make clean             # Clean build artifacts
```

## License

See LICENSE file for details.
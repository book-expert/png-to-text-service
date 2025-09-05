# PNG-to-Text Service

A high-performance OCR service that extracts text from PNG images using Tesseract OCR, with optional AI-powered text enhancement through Google's Gemini models.

## What It Does

This service processes PNG images to extract readable text through optical character recognition (OCR). It provides:

- **Tesseract OCR Integration**: Reliable text extraction from PNG images
- **AI Text Enhancement**: Optional post-processing using Google Gemini models for improved accuracy and formatting
- **Batch Processing**: Efficient processing of multiple images in parallel
- **Configurable Pipeline**: Flexible configuration for different use cases and quality requirements

## Why Use This Service

- **Production Ready**: Built with comprehensive error handling, logging, and configuration management
- **Scalable**: Parallel processing architecture handles large image batches efficiently
- **Quality Focused**: AI enhancement capabilities improve OCR accuracy for challenging images
- **Maintainable**: Clean architecture with well-defined component boundaries and comprehensive test coverage

## How It Works

The service follows a three-stage pipeline architecture:

1. **OCR Stage**: Tesseract processes PNG images to extract raw text
2. **Cleaning Stage**: Text undergoes normalization and cleanup (removes OCR artifacts, fixes spacing, handles ligatures)
3. **Enhancement Stage** (Optional): Gemini AI models improve text quality and add contextual understanding

## How To Use

### Installation

```bash
# Clone the repository
git clone <repository-url>
cd png-to-text-service

# Build the service
make build
```

### Configuration

Create a `project.toml` configuration file:

```toml
[project]
name = "PNG OCR Service"
version = "1.0.0"
description = "OCR service for PNG images"

[paths]
input_dir = "/path/to/input/images"
output_dir = "/path/to/output/text"

[tesseract]
language = "eng"
dpi = 300
timeout_seconds = 120

[settings]
workers = 4
enable_augmentation = false

# Optional: AI Enhancement
[augmentation]
type = "commentary"
use_prompt_builder = false

[gemini]
api_key_variable = "GEMINI_API_KEY"
models = ["gemini-1.5-flash"]
temperature = 0.7
max_tokens = 8192
```

### Basic Usage

```bash
# Process all PNG files in a directory
./bin/png-to-text-service -input ./images -output ./text

# Process a single PNG file
./bin/png-to-text-service -file ./image.png -output ./output

# Disable AI augmentation (faster, no API key required)
./bin/png-to-text-service -input ./images -output ./text -no-augment

# Configure parallel processing
./bin/png-to-text-service -input ./images -output ./text -workers 8
```

### Environment Variables

```bash
# Required for AI enhancement
export GEMINI_API_KEY="your-gemini-api-key"

# Optional: Custom configuration path
export CONFIG_PATH="/path/to/project.toml"
```

## Command Line Options

- `-config string`: Path to configuration file
- `-input string`: Input directory (overrides config)
- `-output string`: Output directory (overrides config)  
- `-file string`: Process single PNG file
- `-workers int`: Number of worker goroutines (overrides config)
- `-no-augment`: Disable AI text augmentation
- `-version`: Print version and exit

## Architecture Overview

The service is organized into focused, single-responsibility modules:

- **`cmd/`**: Application entry points and command-line interface
- **`internal/config/`**: Configuration management and validation
- **`internal/ocr/`**: Tesseract integration and text cleaning
- **`internal/augment/`**: AI-powered text enhancement
- **`internal/pipeline/`**: Processing orchestration and workflow management

Each module maintains clear boundaries with well-defined interfaces, ensuring the system remains maintainable and testable as it evolves.

## Quality Assurance

- **Comprehensive Test Coverage**: Unit tests for all core functionality
- **Static Analysis**: Automated linting and code quality enforcement
- **Error Handling**: Robust error management with detailed logging
- **Configuration Validation**: Runtime validation of all configuration parameters

## Development

### Prerequisites

- Go 1.25+
- Tesseract OCR installed and accessible via PATH
- Google Cloud API credentials (for AI enhancement features)

### Running Tests

```bash
# Run all tests
make test

# Run linting
make lint

# Check test coverage
go test -cover ./...
```

### Project Structure

```
png-to-text-service/
├── cmd/png-to-text-service/     # Main application
├── internal/
│   ├── config/                  # Configuration management
│   ├── ocr/                     # OCR processing and cleaning
│   ├── augment/                 # AI text enhancement
│   └── pipeline/                # Processing orchestration
├── project.toml                 # Configuration file
└── README.md                    # This file
```

This service prioritizes simplicity, reliability, and maintainability while providing the flexibility needed for diverse OCR processing requirements.
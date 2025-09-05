# PNG-to-Text Service

OCR service that extracts text from PNG images using Tesseract, with optional AI enhancement. Uses Google Gemini as reference implementation but can be adapted for other AI providers (Groq, OpenAI, etc.).

## Overview

Processes PNG files to extract text, add AI-generated commentary/summary, and provide final merged output:

1. **Extract**: Tesseract OCR extracts raw text, cleaning removes artifacts
2. **Augment**: Multimodal AI models add commentary or summary using both text and image
3. **Merge**: Combines original OCR text with AI augmentation for final output

## Installation

### Prerequisites
- Go 1.25+
- Tesseract OCR in PATH
- AI provider API key (if using enhancement)

### Build
```bash
git clone <repository-url>
cd png-to-text-service
make build
```

## Configuration

Create `project.toml`:

```toml
[paths]
input_dir = "/path/to/input"
output_dir = "/path/to/output"

[tesseract]
language = "eng"
dpi = 300
timeout_seconds = 120

[settings]
workers = 4
enable_augmentation = false

[augmentation]
type = "commentary"  # or "summary"

[gemini]
api_key_variable = "GEMINI_API_KEY"
models = ["gemini-1.5-flash"]
# Modify internal/augment/ for other providers (Groq, OpenAI, etc.)
```

## Usage

```bash
# Process directory
./bin/png-to-text-service -input ./images -output ./text

# Single file
./bin/png-to-text-service -file image.png -output output.txt

# Without AI enhancement
./bin/png-to-text-service -input ./images -output ./text -no-augment

# Custom workers
./bin/png-to-text-service -input ./images -output ./text -workers 8
```

## Command Line Options

- `-config string`: Configuration file path
- `-input string`: Input directory
- `-output string`: Output directory
- `-file string`: Single PNG file
- `-workers int`: Number of parallel workers
- `-no-augment`: Disable AI enhancement
- `-version`: Show version

## Environment Variables

- `GEMINI_API_KEY`: API key for Gemini (or adapt for other providers)
- `CONFIG_PATH`: Override config file location

## Project Structure

```
png-to-text-service/
├── cmd/png-to-text-service/     # Main executable
├── internal/
│   ├── config/                  # Configuration loading and validation
│   ├── ocr/                     # Tesseract integration and text cleaning
│   ├── augment/                 # AI enhancement (Gemini template)
│   └── pipeline/                # Processing orchestration
└── project.toml                 # Configuration file
```

## Development

```bash
make test      # Run tests
make lint      # Run linting
make build     # Build binary
```

## Configuration Reference

### Tesseract Settings
- `language`: OCR language model ("eng", "fra", etc.)
- `oem`: OCR Engine Mode (0-3, default: 3)
- `psm`: Page Segmentation Mode (0-13, default: 3)
- `dpi`: Image resolution (default: 300)
- `timeout_seconds`: Processing timeout per image

### Augmentation Settings
- `type`: Enhancement type ("commentary" or "summary")

### AI Provider Settings (Gemini)
- `models`: Model names to use
- `temperature`: Creativity level (0.0-1.0)
- `max_tokens`: Response length limit
- `max_retries`: Retry attempts on failure

### Processing Settings
- `workers`: Parallel processing threads
- `skip_existing`: Skip files with existing output
- `enable_augmentation`: Enable AI augmentation and merging
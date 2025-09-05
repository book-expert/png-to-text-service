# OCR Package

Provides optical character recognition (OCR) capabilities using Tesseract OCR engine, with comprehensive text cleaning and normalization for high-quality text extraction from PNG images.

## What It Does

This package handles the complete OCR workflow:

- **Tesseract Integration**: Executes Tesseract OCR engine with configurable parameters
- **Image Validation**: Ensures PNG files meet processing requirements
- **Text Cleaning**: Removes OCR artifacts and normalizes extracted text
- **Error Recovery**: Implements retry logic for transient processing failures
- **Performance Optimization**: Configurable timeouts and processing parameters

## Why This Package Exists

OCR processing requires specialized handling because:

- **Quality Assurance**: Raw OCR output contains artifacts that need systematic cleaning
- **Reliability**: External process execution needs robust error handling and retry mechanisms
- **Performance**: Processing parameters must be tuned for optimal speed/quality tradeoffs
- **Consistency**: Text normalization ensures uniform output across different image types

## How It Works

The package provides two main components:

### Tesseract Processor

Manages the complete OCR workflow:

1. **File Validation**: Checks PNG file existence, format, and size
2. **Process Execution**: Runs Tesseract with configured parameters (language, OEM, PSM, DPI)
3. **Output Processing**: Captures and processes Tesseract's text output
4. **Error Handling**: Manages process failures, timeouts, and retry logic
5. **Text Cleaning**: Applies comprehensive cleaning to extracted text

### Text Cleaner

Performs systematic text normalization:

1. **Artifact Removal**: Eliminates common OCR artifacts (preprint tokens, detected diacritics warnings)
2. **Spacing Normalization**: Fixes multiple spaces, handles hyphenation across lines
3. **Character Replacement**: Converts ligatures and special characters to standard forms
4. **Line Processing**: Removes punctuation-only lines and empty lines
5. **Final Cleanup**: Applies whitespace normalization and removes carriage returns

## How To Use

### Creating a Processor

```go
config := ocr.TesseractConfig{
    Language:       "eng",
    OEM:            3,    // LSTM OCR Engine Mode
    PSM:            6,    // Uniform block of text
    DPI:            300,  // Image resolution
    TimeoutSeconds: 120, // Process timeout
}

processor := ocr.NewProcessor(config, logger)
```

### Processing Images

```go
// Process a single PNG file
text, err := processor.ProcessPNG(context.Background(), "/path/to/image.png")
if err != nil {
    log.Printf("OCR processing failed: %v", err)
    return
}

// Text is automatically cleaned and normalized
fmt.Println(text)
```

### Standalone Text Cleaning

```go
// Clean raw OCR text
cleaner := ocr.NewCleaner()
cleanText := cleaner.Clean(rawOCRText)
```

## Configuration Options

### Tesseract Parameters

- **`Language`**: OCR language model (e.g., "eng", "fra", "deu")
- **`OEM`**: OCR Engine Mode (0-3, where 3 is LSTM-only)
- **`PSM`**: Page Segmentation Mode (0-13, where 6 assumes uniform text block)
- **`DPI`**: Image resolution for processing (typically 300)
- **`TimeoutSeconds`**: Maximum processing time per image

### Validation Rules

The processor enforces these requirements:

- **File Extension**: Must be `.png` (case-insensitive)
- **File Existence**: File must exist and be readable
- **File Type**: Must be a regular file, not a directory
- **File Size**: Must not be empty

## Architecture Design

### Dependencies

- **Configuration**: Uses `internal/config` for Tesseract settings
- **Logging**: Uses `github.com/nnikolov3/logger` for structured logging
- **Standard Library**: Uses `os/exec`, `context`, `bufio` for process management

### Error Handling

The package provides specific error types for different failure modes:

- **Validation Errors**: File format, existence, and permission issues
- **Process Errors**: Tesseract execution failures with detailed context
- **Timeout Errors**: Processing timeouts with retry information
- **System Errors**: Resource constraints and system-level failures

### Performance Considerations

- **Timeouts**: Configurable timeouts prevent hung processes
- **Resource Management**: Proper cleanup of subprocess resources
- **Memory Efficiency**: Streaming text processing for large outputs
- **Retry Logic**: Intelligent retry for transient failures

### Testing Coverage

The package includes comprehensive tests for:

- **Processor Creation**: Configuration validation and initialization
- **File Validation**: All validation rules and error conditions
- **Text Cleaning**: All cleaning operations and edge cases
- **Error Scenarios**: Process failures, timeouts, and invalid inputs

This package provides the core OCR functionality with the reliability and quality required for production text extraction workflows.
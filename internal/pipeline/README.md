# Pipeline Package

Orchestrates the complete PNG-to-text processing workflow, coordinating OCR extraction and AI enhancement with parallel processing, error handling, and comprehensive logging.

## What It Does

This package provides the high-level processing orchestration:

- **Workflow Coordination**: Manages the complete pipeline from PNG input to enhanced text output
- **Parallel Processing**: Coordinates multiple worker goroutines for efficient batch processing
- **Component Integration**: Connects OCR, text cleaning, and AI enhancement components
- **Error Management**: Handles failures gracefully with detailed logging and recovery options
- **Progress Reporting**: Provides comprehensive processing statistics and timing information

## Why This Package Exists

Processing workflows require orchestration because:

- **Component Coordination**: OCR, cleaning, and AI enhancement must work together seamlessly
- **Performance Optimization**: Parallel processing maximizes throughput for batch operations
- **Error Resilience**: Individual file failures shouldn't stop batch processing
- **Resource Management**: CPU-intensive operations need proper coordination and throttling
- **User Experience**: Progress reporting and clear error messages improve usability

## How It Works

The pipeline implements a multi-stage processing workflow:

### Single File Processing

1. **Validation**: Ensures PNG file exists and meets requirements
2. **OCR Processing**: Extracts text using Tesseract with configured parameters
3. **Text Cleaning**: Applies comprehensive cleaning and normalization
4. **AI Enhancement** (Optional): Enhances text using Gemini models with image context
5. **Output Writing**: Saves processed text to the specified output file
6. **Result Reporting**: Logs processing outcome with timing information

### Batch Processing

1. **Directory Scanning**: Discovers all PNG files in input directory
2. **Worker Pool Creation**: Spawns configured number of worker goroutines
3. **Task Distribution**: Distributes files across workers for parallel processing
4. **Progress Monitoring**: Tracks completion status for all files
5. **Result Aggregation**: Collects processing statistics and error information
6. **Summary Reporting**: Provides comprehensive batch processing summary

## How To Use

### Creating a Pipeline

```go
// Configuration includes all component settings
config := &config.Config{
    Tesseract: config.Tesseract{
        Language:       "eng",
        TimeoutSeconds: 120,
    },
    Settings: config.Settings{
        Workers:            4,
        EnableAugmentation: true,
        SkipExisting:       false,
    },
    // ... other configuration
}

pipeline, err := pipeline.NewPipeline(config, logger)
if err != nil {
    log.Fatal(err)
}
```

### Processing Files

```go
ctx := context.Background()

// Process a single file
err := pipeline.ProcessSingle(ctx, "input.png", "output.txt")
if err != nil {
    log.Printf("Processing failed: %v", err)
}

// Process a directory
err = pipeline.ProcessDirectory(ctx, "/input/dir", "/output/dir")
if err != nil {
    log.Printf("Batch processing failed: %v", err)
}
```

## Architecture Design

### Component Integration

The pipeline coordinates these internal components:

- **OCR Processor**: Handles Tesseract integration and text extraction
- **Text Cleaner**: Normalizes and cleans extracted text
- **AI Processor**: Enhances text using Gemini models (when enabled)
- **Configuration**: Provides validated settings for all components

### Processing Workflow

```
PNG Files → File Discovery → Worker Pool → OCR Processing → Text Cleaning → AI Enhancement → Output Files
    ↓              ↓             ↓            ↓               ↓                ↓              ↓
Input Dir → PNG Filtering → Task Queue → Tesseract → Text Cleaner → Gemini API → Output Dir
```

### Error Handling Strategy

The pipeline implements comprehensive error handling:

- **Graceful Degradation**: Individual file failures don't stop batch processing
- **Error Classification**: Distinguishes between recoverable and permanent failures
- **Context Preservation**: Maintains detailed error context for debugging
- **User Communication**: Provides clear, actionable error messages

### Performance Optimization

- **Worker Pools**: Configurable parallelism for optimal resource utilization
- **Skip Logic**: Avoids reprocessing existing output files when configured
- **Memory Management**: Efficient resource usage for large batch operations
- **Timeout Handling**: Prevents hung operations from blocking other work

## Dependencies

- **Configuration**: Uses `internal/config` for pipeline settings
- **OCR**: Uses `internal/ocr` for Tesseract processing and text cleaning
- **Augment**: Uses `internal/augment` for AI-powered text enhancement
- **Logging**: Uses `github.com/nnikolov3/logger` for structured logging

## Configuration Options

The pipeline respects these configuration settings:

### Processing Settings
- **`Workers`**: Number of parallel processing goroutines
- **`TimeoutSeconds`**: Maximum processing time per file
- **`EnableAugmentation`**: Whether to use AI enhancement
- **`SkipExisting`**: Whether to skip files with existing output

### Component Settings
- **Tesseract Configuration**: Language, OCR modes, DPI settings
- **Gemini Configuration**: AI model parameters, retry settings
- **Logging Configuration**: Log levels and output destinations

## Testing Coverage

The package includes comprehensive tests for:

- **Pipeline Creation**: Configuration validation and component initialization
- **Single File Processing**: Success scenarios and error conditions
- **Directory Processing**: Batch operations with various file sets
- **Error Handling**: All error conditions and recovery scenarios
- **Worker Management**: Parallel processing behavior and resource cleanup

## Performance Characteristics

- **Throughput**: Scales linearly with worker count up to system limits
- **Memory Usage**: Constant memory usage regardless of batch size
- **Error Recovery**: Individual failures don't impact overall batch processing
- **Resource Cleanup**: Proper cleanup of system resources and goroutines

This package provides the orchestration layer that transforms individual components into a cohesive, production-ready text processing service.
# Pipeline Package

Processing orchestration and workflow coordination.

## Components

### Pipeline
- `NewPipeline(config, logger)`: Create processing pipeline
- `ProcessSingle(ctx, pngPath, outputPath)`: Process single PNG file
- `ProcessDirectory(ctx, inputDir, outputDir)`: Process directory of PNGs

## Usage

```go
pipeline, err := pipeline.NewPipeline(config, logger)

// Single file
err = pipeline.ProcessSingle(ctx, "input.png", "output.txt")

// Directory
err = pipeline.ProcessDirectory(ctx, "/input", "/output")
```

## Processing Flow

1. **Extract**: OCR text extraction from PNG
2. **Clean**: Remove OCR artifacts and normalize text
3. **Augment** (optional): AI enhancement with image context
4. **Merge**: Combine original + enhanced text
5. **Output**: Write final text to file

## Configuration

Pipeline uses these config settings:
- `Settings.Workers`: Parallel processing threads
- `Settings.EnableAugmentation`: Enable AI enhancement
- `Settings.SkipExisting`: Skip files with existing output
- `Settings.TimeoutSeconds`: Processing timeout per file

## Error Handling

- Individual file failures don't stop batch processing
- Detailed error logging with file context
- Graceful fallback to OCR-only when AI enhancement fails
- Proper resource cleanup for worker goroutines
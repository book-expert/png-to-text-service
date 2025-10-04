# Pipeline Package

Processing orchestration for PNG to text with optional augmentation.

## Components

### Pipeline
- `New(ocr OCRProcessor, augmenter Augmenter, log *logger.Logger, keepTempFiles bool, minTextLength int, defaults *augment.AugmentationOptions) (*Pipeline, error)`
- `Process(ctx context.Context, objectID string, pngData []byte, overrides *augment.AugmentationOptions) (string, error)`

## Usage

```go
pl, err := pipeline.New(ocrProc, augmenter, log, false, 10, defaultOpts)
if err != nil { /* handle */ }

text, err := pl.Process(ctx, "object-id", pngBytes, nil)
```

## Processing Flow

1. Write PNG bytes to a temporary file
2. OCR + clean text via Tesseract
3. Skip augmentation for short text or when disabled
4. Otherwise, augment with Gemini using merged defaults + overrides
5. Return final text (augmented or OCR‑only on failure)

## Configuration

Defaults come from `png_to_text_service.augmentation.defaults` in the project TOML
and can be overridden per message via the incoming augmentation preferences.

## Error Handling

- Failures in augmentation fall back to OCR‑only text
- Detailed, contextual logging
- Temporary files are removed unless `keepTempFiles` is true

# OCR Package

Tesseract OCR integration and text cleaning for PNG files.

## Components

### Processor
- `NewProcessor(config, logger)`: Create OCR processor
- `ProcessPNG(ctx, pngPath)`: Extract and clean text from PNG

### Cleaner
- `NewCleaner()`: Create text cleaner
- `Clean(input)`: Clean OCR artifacts from text

## Usage

```go
// OCR processing
processor := ocr.NewProcessor(config.Tesseract, logger)
text, err := processor.ProcessPNG(ctx, "image.png")

// Standalone cleaning
cleaner := ocr.NewCleaner()
cleaned := cleaner.Clean(rawOCRText)
```

## Tesseract Configuration

```go
TesseractConfig{
    Language:       "eng",     // OCR language (eng, fra, etc.)
    OEM:            3,         // OCR Engine Mode (0-3)
    PSM:            3,         // Page Segmentation Mode (0-13)
    DPI:            300,       // Image resolution
    TimeoutSeconds: 120,       // Process timeout
}
```

## Text Cleaning

Removes OCR artifacts:
- Tesseract error messages ("preprint", "detected diacritics")
- Ligature replacements (ﬁ→fi, ﬂ→fl)
- Hyphenation fixes across line breaks
- Punctuation-only lines
- Multiple spaces normalization

## File Validation

- Must have `.png` extension
- Must exist and be readable
- Must be regular file (not directory)
- Must not be empty
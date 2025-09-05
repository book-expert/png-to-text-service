# Internal Packages

Core business logic and implementation packages.

## Contents

- `config/`: Configuration loading and validation
- `ocr/`: Tesseract OCR integration and text cleaning
- `augment/`: AI text enhancement (Gemini template)
- `pipeline/`: Processing orchestration and workflow

## Pipeline Flow

```
config → ocr → augment → output
   ↓       ↓       ↓
   └── pipeline coordinates all ──┘
```

## Dependencies

- `config/`: Foundation (no internal dependencies)
- `ocr/`: Uses `config/` for Tesseract settings
- `augment/`: Uses `config/` for AI settings
- `pipeline/`: Coordinates all packages

## Usage

```go
import (
    "github.com/nnikolov3/png-to-text-service/internal/config"
    "github.com/nnikolov3/png-to-text-service/internal/pipeline"
)
```
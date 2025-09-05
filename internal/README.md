# Internal Packages

This directory contains the core business logic and implementation details for the PNG-to-Text Service. These packages are designed to be imported only by applications within this project.

## What It Contains

- **`config/`**: Configuration management, validation, and environment setup
- **`ocr/`**: Tesseract OCR integration and text cleaning operations  
- **`augment/`**: AI-powered text enhancement using Google Gemini models
- **`pipeline/`**: Processing orchestration and workflow management

## Why This Architecture

The internal package structure follows domain-driven design principles, organizing code by business capability rather than technical layers:

- **Domain Separation**: Each package represents a distinct business domain with clear responsibilities
- **Dependency Management**: Acyclic dependency graph prevents circular imports and coupling issues
- **Interface-Driven**: Packages interact through well-defined interfaces, not concrete implementations
- **Testability**: Each package can be unit tested in isolation with minimal external dependencies

## How It Works

The packages work together to form a processing pipeline:

```
Configuration → OCR Processing → Text Cleaning → AI Enhancement → Output
     ↓              ↓              ↓              ↓
   config/         ocr/           ocr/         augment/
                                               
               Pipeline Orchestration
                      ↓
                   pipeline/
```

### Package Interactions

1. **`config/`** provides validated configuration to all other packages
2. **`ocr/`** processes images and cleans extracted text
3. **`augment/`** enhances cleaned text using AI models (optional)
4. **`pipeline/`** coordinates the entire workflow and handles parallel processing

## How To Use

### Importing Packages

```go
import (
    "github.com/nnikolov3/png-to-text-service/internal/config"
    "github.com/nnikolov3/png-to-text-service/internal/pipeline"
)
```

### Package Dependencies

The dependency flow follows a strict hierarchy:

- **`config/`**: No internal dependencies (foundation layer)
- **`ocr/`**: Depends on `config/` for Tesseract configuration
- **`augment/`**: Depends on `config/` for Gemini configuration
- **`pipeline/`**: Coordinates all other packages, depends on `config/`, `ocr/`, and `augment/`

## Architecture Principles

Each package adheres to these design principles:

- **Single Responsibility**: Each package has one primary business concern
- **Explicit Dependencies**: All dependencies are explicit and injected, never hidden
- **Error Handling**: Comprehensive error handling with context preservation
- **Configuration-Driven**: Behavior controlled through configuration, not hardcoded values
- **Interface Contracts**: Public APIs defined through interfaces for testability and flexibility

### Quality Standards

- **Comprehensive Testing**: All public functions have corresponding unit tests
- **Documentation**: Package-level documentation explains purpose, usage, and architectural decisions
- **Static Analysis**: Code passes all linting and static analysis checks
- **Performance**: Designed for high-throughput processing with proper resource management

This structure ensures that the service remains maintainable and extensible while providing clear boundaries between different business concerns.
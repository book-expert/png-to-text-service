# Configuration Package

Provides centralized configuration management for the PNG-to-Text Service, handling TOML file parsing, environment variable integration, validation, and default value application.

## What It Does

This package manages all configuration aspects of the service:

- **TOML Configuration Loading**: Parses `project.toml` configuration files
- **Environment Variable Integration**: Merges environment variables with file-based configuration
- **Validation**: Ensures all required configuration values are present and valid
- **Default Application**: Applies sensible defaults for optional configuration values
- **Path Management**: Handles directory creation and path resolution

## Why This Package Exists

Configuration management is centralized to ensure:

- **Consistency**: All components use the same configuration values and validation rules
- **Security**: Sensitive values (API keys) are managed through environment variables
- **Flexibility**: Configuration can be overridden at multiple levels (file, environment, command-line)
- **Reliability**: Invalid configurations are caught early with clear error messages

## How It Works

Configuration loading follows a structured process:

1. **File Discovery**: Automatically locates `project.toml` files starting from the current directory
2. **Parsing**: Uses the external configurator library to parse TOML into Go structures
3. **Validation**: Checks for required fields and validates value ranges
4. **Default Application**: Fills in missing optional values with sensible defaults
5. **Environment Integration**: Overlays environment variables on top of file configuration

### Configuration Structure

```go
type Config struct {
    Project      Project      // Project metadata
    Paths        Paths        // Input/output directories
    Prompts      Prompts      // AI prompt templates
    Augmentation Augmentation // AI enhancement settings
    Logging      Logging      // Log configuration
    Tesseract    Tesseract    // OCR engine settings
    Gemini       Gemini       // AI model settings
    Settings     Settings     // General processing settings
}
```

## How To Use

### Loading Configuration

```go
// Automatic discovery
config, err := config.Load("")
if err != nil {
    log.Fatal(err)
}

// Explicit path
config, err := config.Load("/path/to/project.toml")
if err != nil {
    log.Fatal(err)
}
```

### Accessing Configuration Values

```go
// Tesseract OCR settings
ocrProcessor := ocr.NewProcessor(config.Tesseract, logger)

// AI enhancement settings
aiProcessor := augment.NewGeminiProcessor(&config.Gemini, logger)

// Directory setup
err := config.EnsureDirectories()
```

### Environment Variables

The package supports these environment variables:

- **`GEMINI_API_KEY`**: Google Gemini API key for text enhancement
- **`CONFIG_PATH`**: Override default configuration file location

### Validation Rules

The package enforces these validation requirements:

- **Required Paths**: `input_dir` and `output_dir` must be specified
- **API Key Validation**: When augmentation is enabled, `api_key_variable` must reference a valid environment variable
- **Numeric Ranges**: Timeout values, worker counts, and model parameters must be within acceptable ranges
- **File Extensions**: Configuration ensures compatibility between settings

## Architecture Design

### Dependencies

- **External**: Uses `github.com/nnikolov3/configurator` for TOML parsing
- **Standard Library**: Relies on `os`, `path/filepath` for file operations
- **No Internal Dependencies**: Serves as foundation layer for other packages

### Error Handling

The package provides detailed error messages for common configuration issues:

- Missing required fields with field names
- Invalid file paths with specific path information
- Environment variable resolution errors
- TOML syntax errors with line numbers

### Testing Strategy

The package includes comprehensive tests covering:

- **Success Scenarios**: Valid configuration loading and parsing
- **Error Conditions**: Missing files, invalid values, environment variable issues
- **Default Application**: Verification that defaults are applied correctly
- **Validation Logic**: All validation rules are tested with invalid input

This package serves as the foundation for all other service components, ensuring they receive validated, consistent configuration data.
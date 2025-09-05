# Configuration Package

TOML configuration loading, validation, and management.

## Functions

- `Load(configPath)`: Load and validate configuration from TOML file
- `FindProjectRoot(startDir)`: Locate project.toml file
- `Config.GetAPIKey()`: Retrieve API key from environment
- `Config.EnsureDirectories()`: Create required directories
- `Config.GetLogFilePath()`: Build log file paths

## Configuration Structure

```go
type Config struct {
    Project      Project      // name, version, description
    Paths        Paths        // input_dir, output_dir
    Augmentation Augmentation // type, custom_prompt, use_prompt_builder
    Logging      Logging      // level, dir, enable_*_logging
    Tesseract    Tesseract    // language, oem, psm, dpi, timeout_seconds
    Gemini       Gemini       // api_key_variable, models, retries, timeout
    Settings     Settings     // workers, timeout, enable_augmentation
}
```

## Usage

```go
config, err := config.Load("/path/to/project.toml")
ocrProcessor := ocr.NewProcessor(config.Tesseract, logger)
err = config.EnsureDirectories()
```

## Environment Variables

- `GEMINI_API_KEY`: API key for Gemini
- `CONFIG_PATH`: Override config file location

## Validation

- Required: `input_dir`, `output_dir`
- If augmentation enabled: `api_key_variable` required
- Applies defaults for missing optional values
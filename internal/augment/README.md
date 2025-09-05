# Augment Package

Provides AI-powered text enhancement using Google's Gemini models to improve OCR accuracy, add contextual understanding, and format text for specific use cases.

## What It Does

This package enhances OCR-extracted text through AI processing:

- **Gemini Integration**: Connects to Google's Gemini AI models via REST API
- **Contextual Enhancement**: Uses both extracted text and original image for improved accuracy
- **Flexible Prompting**: Supports multiple enhancement types (commentary, summary, custom)
- **Robust Error Handling**: Implements retry logic, timeout management, and graceful degradation
- **Configurable Processing**: Adjustable model parameters for different quality/speed requirements

## Why This Package Exists

OCR text often requires enhancement because:

- **OCR Limitations**: Character recognition errors, spacing issues, and context misunderstandings
- **Quality Improvement**: AI models can correct errors and improve readability using visual context
- **Use Case Adaptation**: Different applications need different text formatting and enhancement styles
- **Business Value**: Enhanced text provides more value for downstream processing and analysis

## How It Works

The enhancement process follows this workflow:

1. **Input Validation**: Verifies text content and image file accessibility
2. **Image Processing**: Loads and encodes the original PNG image for context
3. **Prompt Construction**: Builds appropriate prompts based on enhancement type
4. **API Communication**: Sends requests to Gemini API with text and image data
5. **Response Processing**: Extracts and validates enhanced text from API responses
6. **Error Recovery**: Implements intelligent retry logic for transient failures

### Enhancement Types

- **Commentary**: Adds contextual explanations and improves readability
- **Summary**: Creates concise summaries of longer text content
- **Custom**: Uses user-defined prompts for specialized processing

## How To Use

### Creating a Processor

```go
config := &augment.GeminiConfig{
    APIKey:            "your-gemini-api-key",
    PromptTemplate:    "Improve this OCR text for clarity",
    AugmentationType:  augment.AugmentationCommentary,
    Models:            []string{"gemini-1.5-flash"},
    Temperature:       0.7,
    TimeoutSeconds:    120,
    MaxRetries:        3,
    RetryDelaySeconds: 30,
}

processor := augment.NewGeminiProcessor(config, logger)
```

### Enhancing Text

```go
// Enhance OCR text with image context
enhancedText, err := processor.AugmentText(
    context.Background(),
    ocrText,
    "/path/to/original/image.png",
)
if err != nil {
    log.Printf("Enhancement failed: %v", err)
    // Fall back to original OCR text
    return ocrText
}

return enhancedText
```

### Configuration Options

```go
type GeminiConfig struct {
    APIKey            string              // Gemini API authentication key
    PromptTemplate    string              // Base prompt for enhancement
    CustomPrompt      string              // Override prompt for custom processing
    AugmentationType  AugmentationType    // Enhancement style (commentary/summary/custom)
    Models            []string            // Gemini model names to try
    Temperature       float64             // Model creativity (0.0-1.0)
    TopK              int                 // Token selection parameter
    TopP              float64             // Nucleus sampling parameter
    MaxTokens         int                 // Maximum response length
    TimeoutSeconds    int                 // Request timeout
    MaxRetries        int                 // Retry attempts for failures
    RetryDelaySeconds int                 // Delay between retries
    UsePromptBuilder  bool                // Enable external prompt builder (future)
}
```

## Architecture Design

### API Integration

The package implements a complete HTTP client for Gemini API:

- **Authentication**: API key-based authentication
- **Request Construction**: JSON payload with text and image data
- **Response Parsing**: Robust JSON parsing with error handling
- **Rate Limiting**: Built-in retry logic with exponential backoff

### Error Handling Strategy

The package handles various failure modes:

- **Network Errors**: Connection failures, timeouts, DNS issues
- **API Errors**: Authentication failures, quota limits, invalid requests
- **Response Errors**: Malformed responses, empty results, parsing failures
- **System Errors**: File access issues, memory constraints

### Retry Logic

Implements intelligent retry behavior:

- **Transient Errors**: Automatically retries network and API errors
- **Exponential Backoff**: Increases delay between retry attempts
- **Maximum Attempts**: Configurable retry limits prevent infinite loops
- **Error Classification**: Distinguishes between retryable and permanent failures

### Performance Optimization

- **Timeout Management**: Configurable timeouts prevent hung requests
- **Connection Reuse**: HTTP client reuses connections for efficiency
- **Memory Management**: Streams large responses to minimize memory usage
- **Concurrent Safety**: Thread-safe for parallel processing workflows

## Dependencies

- **Configuration**: Uses `internal/config` for Gemini settings
- **Logging**: Uses `github.com/nnikolov3/logger` for structured logging
- **Standard Library**: Uses `net/http`, `encoding/json`, `context` for API communication

## Testing Coverage

The package includes comprehensive tests for:

- **Processor Creation**: Configuration validation and initialization
- **Input Validation**: Text and image validation with various error conditions
- **API Interaction**: Mocked API responses for different scenarios
- **Error Handling**: All error conditions and retry logic
- **Response Processing**: JSON parsing and text extraction

This package provides production-ready AI enhancement capabilities with the reliability and configurability needed for diverse text processing workflows.
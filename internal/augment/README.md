# Augment Package

AI text enhancement using Gemini models (template for other providers).

## Components

### GeminiProcessor
- `NewGeminiProcessor(config, logger)`: Create AI processor
- `AugmentText(ctx, ocrText, imagePath)`: Enhance text using image context
- `AugmentTextWithOptions(ctx, ocrText, imagePath, opts)`: Enhanced with options

## Usage

```go
config := &augment.GeminiConfig{
    APIKey:            os.Getenv("GEMINI_API_KEY"),
    AugmentationType:  augment.AugmentationCommentary,
    Models:            []string{"gemini-1.5-flash"},
    Temperature:       0.7,
    MaxRetries:        3,
    TimeoutSeconds:    120,
}

processor := augment.NewGeminiProcessor(config, logger)
enhanced, err := processor.AugmentText(ctx, ocrText, "image.png")
```

## Augmentation Types

- `AugmentationCommentary`: Add descriptive commentary like accessibility features
- `AugmentationSummary`: Add summary at end of content
- Custom prompts via `AugmentationOptions.CustomPrompt`

## Configuration

```go
GeminiConfig{
    APIKey:            string    // Gemini API key
    AugmentationType:  string    // "commentary" or "summary"
    Models:            []string  // Model names to try
    Temperature:       float64   // Creativity (0.0-1.0)
    MaxRetries:        int       // Retry attempts
    TimeoutSeconds:    int       // Request timeout
    MaxTokens:         int       // Response length limit
}
```

## Adapting for Other Providers

This package uses Gemini as a template. To adapt for other AI providers (Groq, OpenAI, etc.):
1. Modify API URL and request format in HTTP calls
2. Update authentication method
3. Adjust response parsing for provider's JSON format
4. Keep the same interface for easy swapping
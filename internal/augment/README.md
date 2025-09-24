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
    APIKey:               os.Getenv("GEMINI_API_KEY"),
    CommentaryBasePrompt: "...",
    SummaryBasePrompt:    "...",
    Models:               []string{"gemini-1.5-flash"},
    Temperature:          0.7,
    MaxRetries:           3,
    TimeoutSeconds:       120,
}

processor := augment.NewGeminiProcessor(config, logger)
options := &augment.AugmentationOptions{
    Commentary: augment.AugmentationCommentaryOptions{Enabled: true},
    Summary:    augment.AugmentationSummaryOptions{Enabled: true},
}
enhanced, err := processor.AugmentTextWithOptions(ctx, ocrText, "image.png", options)
```

## Configuration Highlights

- `CommentaryBasePrompt` / `SummaryBasePrompt`: Base instructions for each augmentation mode.
- `AugmentationOptions.Commentary` / `.Summary`: Enablement flags, placement (top/bottom), and extra instructions supplied at runtime.

## Adapting for Other Providers

This package uses Gemini as a template. To adapt for other AI providers (Groq, OpenAI, etc.):
1. Modify API URL and request format in HTTP calls
2. Update authentication method
3. Adjust response parsing for provider's JSON format
4. Keep the same interface for easy swapping

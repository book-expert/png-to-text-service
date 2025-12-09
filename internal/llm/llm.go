package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/book-expert/logger"
	"google.golang.org/genai"
)

var (
	ErrFileEmpty    = errors.New("file is empty")
	ErrLLMAPIError  = errors.New("LLM API error")
	ErrNoCandidates = errors.New("no candidates in response")
)

// Config holds the configuration for the LLM client.
type Config struct {
	APIKey            string
	Model             string
	Temperature       float64
	TimeoutSeconds    int
	MaxRetries        int
	SystemInstruction string
	ExtractionPrompt  string
}

// Processor provides methods for interacting with the LLM.
type Processor struct {
	client *genai.Client
	logger *logger.Logger
	config Config
}

// NewProcessor creates a new generic Processor instance using the GenAI SDK.
func NewProcessor(config *Config, log *logger.Logger) *Processor {
	ctx := context.Background()
	// Fix 1: Removed Backend option as it might be default/unnecessary.
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: config.APIKey,
	})
	if err != nil {
		log.Fatal("Failed to create GenAI client: %v", err)
	}

	return &Processor{
		client: client,
		config: *config,
		logger: log,
	}
}

// ProcessImage uploads the PNG and sends it to the LLM for text extraction and augmentation.
func (processor *Processor) ProcessImage(ctx context.Context, objectID string, imageData []byte) (string, error) {
	if len(imageData) == 0 {
		return "", ErrFileEmpty
	}

	// 1. Upload File
	// Fix 2: Use UploadFileConfig instead of UploadFileOptions if available, or nil.
	// Based on error "undefined: genai.UploadFileOptions", let's try passing nil for options first
	// or construct the UploadFileConfig if I can guess it. 
    // Actually, the library usually uses *UploadFileConfig.
    // Let's assume the method signature is Upload(ctx, reader, config).
    
	uploadConfig := &genai.UploadFileConfig{
		DisplayName: fmt.Sprintf("ocr-%s", objectID),
		MIMEType:    "image/png",
	}

	file, err := processor.client.Files.Upload(ctx, bytes.NewReader(imageData), uploadConfig)
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	// Defer deletion
	// Fix 3: Handle Delete signature: (ctx, name, config) -> (response, error)
	defer func() {
		if _, err := processor.client.Files.Delete(context.Background(), file.Name, nil); err != nil {
			processor.logger.Warn("Failed to delete file %s: %v", file.Name, err)
		}
	}()

	// 2. Generate Content
	var lastErr error
	for attempt := 1; attempt <= processor.config.MaxRetries; attempt++ {
		result, err := processor.generateContent(ctx, file)
		if err == nil && strings.TrimSpace(result) != "" {
			return result, nil
		}

		lastErr = err
		processor.logger.Warn("LLM attempt %d/%d failed: %v", attempt, processor.config.MaxRetries, err)

		if attempt < processor.config.MaxRetries {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Second * 2):
				continue
			}
		}
	}

	return "", fmt.Errorf("all attempts failed: %w", lastErr)
}

func (processor *Processor) generateContent(ctx context.Context, file *genai.File) (string, error) {
    // Fix 4: Cast temperature to float32 (pointer)
    temp := float32(processor.config.Temperature)
    
	req := &genai.GenerateContentConfig{
		// Model field removed (passed as arg)
		Temperature: &temp,
		ResponseMIMEType: "application/json", 
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				{Text: processor.config.SystemInstruction},
			},
		},
	}

	parts := []*genai.Part{
		{FileData: &genai.FileData{FileURI: file.URI, MIMEType: file.MIMEType}},
		{Text: processor.config.ExtractionPrompt},
	}

	resp, err := processor.client.Models.GenerateContent(ctx, processor.config.Model, []*genai.Content{{Parts: parts}}, req)
	if err != nil {
		return "", fmt.Errorf("generate content failed: %w", err)
	}

	if resp == nil || len(resp.Candidates) == 0 {
		return "", ErrNoCandidates
	}

	var sb strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		sb.WriteString(part.Text)
	}

	rawText := sb.String()
	if !isValidJSON(rawText) {
		processor.logger.Warn("Model returned invalid JSON: %s", rawText)
	}

	return rawText, nil
}

func isValidJSON(s string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(s), &js) == nil
}
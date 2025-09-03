// Package augment provides AI-powered text augmentation using Gemini models.
package augment

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nnikolov3/logger"
)

// AugmentationType defines the type of augmentation to perform.
type AugmentationType string

const (
	// AugmentationCommentary describes people, environment, etc. like movie
	// accessibility features (default).
	AugmentationCommentary AugmentationType = "commentary"
	// AugmentationSummary adds a summary at the end of the page.
	AugmentationSummary AugmentationType = "summary"
)

// AugmentationOptions contains options for text augmentation.
type AugmentationOptions struct {
	Parameters   map[string]any
	Type         AugmentationType
	CustomPrompt string
}

// GeminiConfig holds configuration for Gemini API interaction.
type GeminiConfig struct {
	APIKey            string
	PromptTemplate    string
	CustomPrompt      string
	AugmentationType  AugmentationType
	Models            []string
	Temperature       float64
	TimeoutSeconds    int
	TopK              int
	TopP              float64
	MaxTokens         int
	RetryDelaySeconds int
	MaxRetries        int
	UsePromptBuilder  bool
}

// GeminiProcessor implements text augmentation using Google's Gemini API.
type GeminiProcessor struct {
	httpClient *http.Client
	logger     *logger.Logger
	config     GeminiConfig
}

// Gemini API types for JSON marshaling.
type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiPart struct {
	InlineData *geminiInlineData `json:"inlineData,omitempty"`
	Text       string            `json:"text,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	Temperature     float64 `json:"temperature"`
	TopK            int     `json:"topK"`
	TopP            float64 `json:"topP"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

type geminiRequest struct {
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig"`
}

type geminiCandidateContentPart struct {
	Text string `json:"text"`
}

type geminiCandidateContent struct {
	Parts []geminiCandidateContentPart `json:"parts"`
}

type geminiCandidate struct {
	Content geminiCandidateContent `json:"content"`
}

type geminiError struct {
	Message string `json:"message"`
}

type geminiResponse struct {
	Error      *geminiError      `json:"error,omitempty"`
	Candidates []geminiCandidate `json:"candidates"`
}

// NewGeminiProcessor creates a new Gemini-based text augmentation processor.
func NewGeminiProcessor(config GeminiConfig, logger *logger.Logger) *GeminiProcessor {
	return &GeminiProcessor{
		config: config,
		httpClient: &http.Client{
			Transport:     nil,
			CheckRedirect: nil,
			Jar:           nil,
			Timeout:       time.Duration(config.TimeoutSeconds) * time.Second,
		},
		logger: logger,
	}
}

// AugmentText enhances OCR text using image context and AI models.
// This method maintains backward compatibility by using default configuration options.
func (g *GeminiProcessor) AugmentText(
	ctx context.Context,
	ocrText, imagePath string,
) (string, error) {
	// Use the new method with default options for backward compatibility
	opts := &AugmentationOptions{
		Type:         g.config.AugmentationType,
		CustomPrompt: g.config.CustomPrompt,
		Parameters:   nil,
	}

	return g.AugmentTextWithOptions(ctx, ocrText, imagePath, opts)
}

// validateInputs checks that the required inputs are valid.
func (g *GeminiProcessor) validateInputs(ocrText, imagePath string) error {
	if imagePath == "" {
		return errors.New("image path is required")
	}

	if _, err := os.Stat(imagePath); err != nil {
		return fmt.Errorf("access image file: %w", err)
	}

	return nil
}

// readAndEncodeImage reads an image file and returns base64 encoded data with MIME type.
func (g *GeminiProcessor) readAndEncodeImage(imagePath string) (string, string, error) {
	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return "", "", fmt.Errorf("read image file: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(imageBytes)
	mimeType := g.detectImageMimeType(imagePath)

	return encoded, mimeType, nil
}

// detectImageMimeType returns appropriate MIME type based on file extension.
func (g *GeminiProcessor) detectImageMimeType(imagePath string) string {
	ext := strings.ToLower(filepath.Ext(imagePath))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "image/png"
	}
}


// buildPromptWithOptions constructs a prompt based on augmentation options.
func (g *GeminiProcessor) buildPromptWithOptions(
	ocrText string,
	opts *AugmentationOptions,
) (string, error) {
	// If custom prompt is provided, use it directly
	if opts != nil && opts.CustomPrompt != "" {
		return g.buildCustomPrompt(ocrText, opts.CustomPrompt), nil
	}

	// If prompt builder is enabled, use enhanced prompt building
	if g.config.UsePromptBuilder {
		return g.buildPromptWithBuilder(ocrText, opts)
	}

	// Fall back to original prompt building logic with type-specific defaults
	augmentationType := g.config.AugmentationType
	if opts != nil && opts.Type != "" {
		augmentationType = opts.Type
	}

	return g.buildPromptForType(ocrText, augmentationType), nil
}

// buildCustomPrompt constructs a prompt using a custom template.
func (g *GeminiProcessor) buildCustomPrompt(ocrText, customPrompt string) string {
	var builder strings.Builder

	builder.WriteString(customPrompt)
	builder.WriteString("\n\nOCR TEXT FOLLOWS:\n\n")

	if strings.TrimSpace(ocrText) == "" {
		builder.WriteString(
			"[This page contains little or no OCR text. Describe relevant content from the image in clear narration.]",
		)
	} else {
		builder.WriteString(ocrText)
	}

	return builder.String()
}

// buildPromptWithBuilder constructs a prompt using a simple built-in template system.
// Note: This is a simplified implementation. In the future, this could be enhanced
// with a proper external prompt-builder library when it has a stable public API.
func (g *GeminiProcessor) buildPromptWithBuilder(
	ocrText string,
	opts *AugmentationOptions,
) (string, error) {
	// Determine the augmentation type
	augmentationType := g.config.AugmentationType
	if opts != nil && opts.Type != "" {
		augmentationType = opts.Type
	}

	// Use the simpler prompt building approach
	return g.buildPromptForType(ocrText, augmentationType), nil
}

// buildPromptForType constructs a prompt for a specific augmentation type.
func (g *GeminiProcessor) buildPromptForType(
	ocrText string,
	augmentationType AugmentationType,
) string {
	var (
		builder        strings.Builder
		promptTemplate string
	)

	// Select prompt template based on type

	switch augmentationType {
	case AugmentationCommentary:
		promptTemplate = getCommentaryPrompt()
	case AugmentationSummary:
		promptTemplate = getSummaryPrompt()
	default:
		// Use configured template or fall back to commentary
		if g.config.PromptTemplate != "" {
			promptTemplate = g.config.PromptTemplate
		} else {
			promptTemplate = getCommentaryPrompt()
		}
	}

	builder.WriteString(promptTemplate)
	builder.WriteString("\n\nOCR TEXT FOLLOWS:\n\n")
	builder.WriteString(g.formatOCRText(ocrText))

	return builder.String()
}

// formatOCRText formats OCR text for prompt inclusion.
func (g *GeminiProcessor) formatOCRText(ocrText string) string {
	if strings.TrimSpace(ocrText) == "" {
		return "[This page contains little or no OCR text. Describe relevant content from the image in clear narration.]"
	}

	return ocrText
}

// tryModelWithRetries attempts to get a response from a specific model with retry logic.
func (g *GeminiProcessor) tryModelWithRetries(
	ctx context.Context,
	model, prompt, imageData, mimeType string,
) (string, error) {
	var lastErr error

	for attempt := 1; attempt <= g.config.MaxRetries; attempt++ {
		result, err := g.callGeminiAPI(ctx, model, prompt, imageData, mimeType)
		if err == nil && strings.TrimSpace(result) != "" {
			return result, nil
		}

		if err == nil {
			err = errors.New("empty response")
		}

		lastErr = fmt.Errorf(
			"model %s attempt %d/%d: %w",
			model,
			attempt,
			g.config.MaxRetries,
			err,
		)

		// Wait before retry (except on last attempt)
		if attempt < g.config.MaxRetries {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(g.config.RetryDelaySeconds) * time.Second):
			}
		}
	}

	return "", lastErr
}

// callGeminiAPI makes a single API call to Gemini.
func (g *GeminiProcessor) callGeminiAPI(
	ctx context.Context,
	model, prompt, imageData, mimeType string,
) (string, error) {
	// Build API URL
	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model,
		g.config.APIKey,
	)

	// Build request body
	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Role: "user",
				Parts: []geminiPart{
					{
						InlineData: nil,
						Text:       prompt,
					},
					{
						InlineData: &geminiInlineData{
							MimeType: mimeType,
							Data:     imageData,
						},
						Text: "",
					},
				},
			},
		},
		GenerationConfig: geminiGenerationConfig{
			Temperature:     g.config.Temperature,
			TopK:            g.config.TopK,
			TopP:            g.config.TopP,
			MaxOutputTokens: g.config.MaxTokens,
		},
	}

	// Marshal request to JSON
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		url,
		bytes.NewReader(jsonData),
	)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			g.logger.Warn("Failed to close response body: %v", closeErr)
		}
	}()

	// Read response
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	// Handle HTTP errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErrResp geminiResponse
		if json.Unmarshal(respBytes, &apiErrResp) == nil &&
			apiErrResp.Error != nil {
			return "", fmt.Errorf(
				"Gemini API error (HTTP %d): %s",
				resp.StatusCode,
				apiErrResp.Error.Message,
			)
		}

		return "", fmt.Errorf(
			"Gemini API error (HTTP %d): %s",
			resp.StatusCode,
			strings.TrimSpace(string(respBytes)),
		)
	}

	// Parse response
	var geminiResp geminiResponse
	if err := json.Unmarshal(respBytes, &geminiResp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	// Check for API errors in response
	if geminiResp.Error != nil && geminiResp.Error.Message != "" {
		return "", fmt.Errorf("Gemini API error: %s", geminiResp.Error.Message)
	}

	// Extract text from candidates
	if len(geminiResp.Candidates) == 0 {
		return "", errors.New("no candidates in response")
	}

	var textBuilder strings.Builder
	for _, part := range geminiResp.Candidates[0].Content.Parts {
		if strings.TrimSpace(part.Text) == "" {
			continue
		}

		if textBuilder.Len() > 0 {
			textBuilder.WriteByte('\n')
		}

		textBuilder.WriteString(part.Text)
	}

	return textBuilder.String(), nil
}

// getCommentaryPrompt returns the default commentary prompt.
func getCommentaryPrompt() string {
	return "You are a PhD-level technical narrator specializing in accessibility commentary. Integrate descriptive commentary derived from the page image into the OCR text, producing a coherent narration for text-to-speech. Focus on describing people, environment, figures, diagrams, and visual elements like movie accessibility features. Maintain technical accuracy, describe visual content as natural prose inserted near relevant context, avoid markdown formatting, and output continuous paragraphs."
}

// getSummaryPrompt returns the default summary prompt.
func getSummaryPrompt() string {
	return "You are a PhD-level technical summarizer. Enhance the OCR text by adding a comprehensive summary at the end of the content. The summary should capture key points, main concepts, and important details from both the OCR text and visual elements in the image. Maintain technical accuracy, avoid markdown formatting, and structure the output as: [original OCR text] followed by [SUMMARY: comprehensive summary paragraph]."
}

// AugmentTextWithOptions enhances OCR text using image context and AI models with
// specific options.
func (g *GeminiProcessor) AugmentTextWithOptions(
	ctx context.Context,
	ocrText, imagePath string,
	opts *AugmentationOptions,
) (string, error) {
	if err := g.validateInputs(ocrText, imagePath); err != nil {
		return "", fmt.Errorf("validate inputs: %w", err)
	}

	// Read and encode the image
	imageData, mimeType, err := g.readAndEncodeImage(imagePath)
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}

	// Build the prompt based on options
	prompt, err := g.buildPromptWithOptions(ocrText, opts)
	if err != nil {
		return "", fmt.Errorf("build prompt: %w", err)
	}

	// Try models in order with retries
	var lastErr error

	for _, model := range g.config.Models {
		result, err := g.tryModelWithRetries(
			ctx,
			model,
			prompt,
			imageData,
			mimeType,
		)
		if err == nil && strings.TrimSpace(result) != "" {
			return result, nil
		}

		lastErr = err
		g.logger.Warn("Model %s failed: %v", model, err)
	}

	return "", fmt.Errorf("all models failed, last error: %w", lastErr)
}

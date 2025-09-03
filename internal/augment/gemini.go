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

// GeminiConfig holds configuration for Gemini API interaction.
type GeminiConfig struct {
	APIKey            string
	PromptTemplate    string
	Models            []string
	MaxRetries        int
	RetryDelaySeconds int
	TimeoutSeconds    int
	Temperature       float64
	TopK              int
	TopP              float64
	MaxTokens         int
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
			Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		},
		logger: logger,
	}
}

// AugmentText enhances OCR text using image context and AI models.
func (g *GeminiProcessor) AugmentText(
	ctx context.Context,
	ocrText, imagePath string,
) (string, error) {
	if err := g.validateInputs(ocrText, imagePath); err != nil {
		return "", fmt.Errorf("validate inputs: %w", err)
	}

	// Read and encode the image
	imageData, mimeType, err := g.readAndEncodeImage(imagePath)
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}

	// Build the prompt
	prompt := g.buildPrompt(ocrText)

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

// buildPrompt constructs the full prompt including template and OCR text.
func (g *GeminiProcessor) buildPrompt(ocrText string) string {
	var builder strings.Builder

	// Use configured prompt template or default
	promptTemplate := g.config.PromptTemplate
	if promptTemplate == "" {
		promptTemplate = "You are a PhD-level technical narrator. Integrate commentary derived from the page image into the OCR text, producing a coherent narration for text-to-speech. Maintain technical accuracy, describe figures/code/tables as natural prose inserted near the relevant context, avoid markdown, and output continuous paragraphs."
	}

	builder.WriteString(promptTemplate)
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
					{Text: prompt},
					{
						InlineData: &geminiInlineData{
							MimeType: mimeType,
							Data:     imageData,
						},
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
	defer resp.Body.Close()

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

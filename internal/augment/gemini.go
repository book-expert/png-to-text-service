// Package augment provides functionality for augmenting OCR text using the Gemini API.
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

	"github.com/book-expert/logger"
	"github.com/book-expert/prompt-builder/promptbuilder"
)

var (
	// ErrImagePathRequired is returned when the image path is empty.
	ErrImagePathRequired = errors.New("image path is required")
	// ErrEmptyResponse is returned when the Gemini API returns an empty response.
	ErrEmptyResponse = errors.New("empty response")
	// ErrNoCandidates is returned when the Gemini API response contains no candidates.
	ErrNoCandidates = errors.New("no candidates in response")
	// ErrGeminiAPIError is returned when the Gemini API returns an error.
	ErrGeminiAPIError = errors.New("gemini API error")
	// ErrMaxRetries is returned when the maximum number of retries is exceeded.
	ErrMaxRetries = errors.New("max retries exceeded")
)

// AugmentationType defines the type of augmentation to perform.
type AugmentationType string

const (
	// AugmentationCommentary indicates that the augmentation should be a commentary.
	AugmentationCommentary AugmentationType = "commentary"
	// AugmentationSummary indicates that the augmentation should be a summary.
	AugmentationSummary AugmentationType = "summary"

	// MaxFileSize1MB defines the maximum file size allowed for processing in bytes (1MB).
	MaxFileSize1MB = 1024 * 1024
)

// AugmentationOptions holds options for text augmentation.
type AugmentationOptions struct {
	Parameters   map[string]any   `json:"parameters"`
	Type         AugmentationType `json:"mode"`
	CustomPrompt string           `json:"customPrompt"`
}

// GeminiConfig holds the configuration for the Gemini API client.
type GeminiConfig struct {
	APIKey            string
	PromptTemplate    string // No longer used by the new builder, but kept for the simple path
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

// GeminiProcessor provides methods for interacting with the Gemini API.
type GeminiProcessor struct {
	httpClient *http.Client
	logger     *logger.Logger
	config     GeminiConfig
}

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

// NewGeminiProcessor creates a new GeminiProcessor instance.
func NewGeminiProcessor(config *GeminiConfig, log *logger.Logger) *GeminiProcessor {
	return &GeminiProcessor{
		config: *config,
		httpClient: &http.Client{
			Timeout:       time.Duration(config.TimeoutSeconds) * time.Second,
			Transport:     nil, // Added missing field
			CheckRedirect: nil, // Added missing field
			Jar:           nil, // Added missing field
		},
		logger: log,
	}
}

// AugmentTextWithOptions augments the given OCR text with additional information
// using the Gemini API, based on the provided image and augmentation options.
func (g *GeminiProcessor) AugmentTextWithOptions(
	ctx context.Context,
	ocrText, imagePath string,
	opts *AugmentationOptions,
) (string, error) {
	err := g.validateInputs(ocrText, imagePath)
	if err != nil {
		return "", fmt.Errorf("validate inputs: %w", err)
	}

	imageData, mimeType, err := g.prepareImageData(imagePath)
	if err != nil {
		return "", err
	}

	prompt, err := g.buildPromptWithOptions(ocrText, imagePath, opts)
	if err != nil {
		return "", fmt.Errorf("build prompt: %w", err)
	}

	return g.tryAllModels(ctx, prompt, imageData, mimeType)
}

func (g *GeminiProcessor) buildPromptWithOptions(
	ocrText, imagePath string,
	opts *AugmentationOptions,
) (string, error) {
	if g.config.UsePromptBuilder {
		return g.buildPromptWithBuilder(ocrText, imagePath, opts)
	}
	// Fallback to simple string replacement if not using the builder
	var finalPrompt string
	if opts != nil && opts.CustomPrompt != "" {
		finalPrompt = opts.CustomPrompt
	} else {
		finalPrompt = g.config.PromptTemplate
	}

	return strings.ReplaceAll(finalPrompt, "{{.OCRText}}", ocrText), nil
}

// CORRECTED: This function now correctly uses the new, fully-featured prompt builder.
func (g *GeminiProcessor) buildPromptWithBuilder(
	ocrText, imagePath string,
	opts *AugmentationOptions,
) (string, error) {
	// 1. Create a FileProcessor with default settings.
	allowedExtensions := []string{
		".png",
	}
	fileProcessor := promptbuilder.NewFileProcessor(
		MaxFileSize1MB,
		allowedExtensions,
	) // 1MB max file size

	// 2. Create the builder by passing the FileProcessor.
	builder := promptbuilder.New(fileProcessor)

	// Add a preset for commentary, as an example.
	_ = builder.AddSystemPreset(
		"commentary",
		"You are an expert code analyst providing commentary.",
	)

	// 3. Create a BuildRequest struct with all the necessary information.
	req := &promptbuilder.BuildRequest{
		Prompt:        ocrText,   // The main text goes into the prompt field.
		File:          imagePath, // The image path is treated as the file to be included.
		Task:          "",        // Added missing field
		SystemMessage: "",        // Added missing field
		Guidelines:    "",        // Added missing field
		Image:         "",        // Added missing field
		OutputFormat:  "",        // Added missing field
	}

	if opts != nil {
		req.Task = string(opts.Type) // e.g., "commentary"
		if opts.CustomPrompt != "" {
			req.SystemMessage = opts.CustomPrompt
		}
	}

	// 4. Call the new BuildPrompt method.
	result, err := builder.BuildPrompt(req)
	if err != nil {
		return "", fmt.Errorf("failed to build prompt: %w", err)
	}

	// 5. Return the final prompt string from the result.
	return result.Prompt.String(), nil
}

func (g *GeminiProcessor) validateInputs(_, imagePath string) error {
	if imagePath == "" {
		return ErrImagePathRequired
	}

	_, err := os.Stat(imagePath)
	if err != nil {
		return fmt.Errorf("access image file: %w", err)
	}

	return nil
}

func (g *GeminiProcessor) prepareImageData(
	imagePath string,
) (imageData, mimeType string, err error) {
	imageData, mimeType, err = g.readAndEncodeImage(imagePath)
	if err != nil {
		return "", "", fmt.Errorf("read image: %w", err)
	}

	return imageData, mimeType, nil
}

func (g *GeminiProcessor) readAndEncodeImage(
	imagePath string,
) (encodedData, mimeType string, err error) {
	cleanedImagePath := filepath.Clean(imagePath)

	imageBytes, err := os.ReadFile(cleanedImagePath)
	if err != nil {
		return "", "", fmt.Errorf("read image file: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(imageBytes)
	detectedMimeType := g.detectImageMimeType(imagePath)

	return encoded, detectedMimeType, nil
}

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

func (g *GeminiProcessor) tryAllModels(
	ctx context.Context,
	prompt, imageData, mimeType string,
) (string, error) {
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

		lastErr = err

		if attempt < g.config.MaxRetries {
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("context done: %w", ctx.Err())
			case <-time.After(time.Duration(g.config.RetryDelaySeconds) * time.Second):
				// continue
			}
		}
	}

	return "", fmt.Errorf(
		"model %s failed after %d attempts: %w",
		model,
		g.config.MaxRetries,
		lastErr,
	)
}

func (g *GeminiProcessor) createGeminiRequest(
	prompt, imageData, mimeType string,
) ([]byte, error) {
	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Role: "user",
				Parts: []geminiPart{
					{Text: prompt, InlineData: nil},
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

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	return jsonData, nil
}

func (g *GeminiProcessor) executeHTTPRequest(
	ctx context.Context,
	url string,
	jsonData []byte,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		url,
		bytes.NewReader(jsonData),
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}

	return resp, nil
}

func (g *GeminiProcessor) processGeminiResponse(
	resp *http.Response,
) (string, error) {
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: HTTP %d: %s",
			ErrGeminiAPIError,
			resp.StatusCode,
			strings.TrimSpace(string(respBytes)),
		)
	}

	var geminiResp geminiResponse

	unmarshalErr := json.Unmarshal(respBytes, &geminiResp)
	if unmarshalErr != nil {
		return "", fmt.Errorf("unmarshal response: %w", unmarshalErr)
	}

	if len(geminiResp.Candidates) == 0 ||
		len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", ErrNoCandidates
	}

	var textBuilder strings.Builder
	for _, part := range geminiResp.Candidates[0].Content.Parts {
		textBuilder.WriteString(part.Text)
	}

	return textBuilder.String(), nil
}

func (g *GeminiProcessor) callGeminiAPI(
	ctx context.Context,
	model, prompt, imageData, mimeType string,
) (string, error) {
	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model,
		g.config.APIKey,
	)

	jsonData, err := g.createGeminiRequest(prompt, imageData, mimeType)
	if err != nil {
		return "", err
	}

	resp, err := g.executeHTTPRequest(ctx, url, jsonData)
	if err != nil {
		return "", err
	}

	defer func() {
		closeErr := resp.Body.Close()
		if closeErr != nil {
			g.logger.Error("failed to close response body: %v", closeErr)
		}
	}()

	return g.processGeminiResponse(resp)
}

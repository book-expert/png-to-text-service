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

var (
	// ErrImagePathRequired indicates that an image path is required for processing.
	ErrImagePathRequired = errors.New("image path is required")
	// ErrEmptyResponse indicates that the Gemini API returned an empty response.
	ErrEmptyResponse = errors.New("empty response")
	// ErrNoCandidates indicates that no candidates were found in the API response.
	ErrNoCandidates = errors.New("no candidates in response")
	// ErrGeminiAPIError indicates a generic Gemini API error.
	ErrGeminiAPIError = errors.New("gemini API error")
	// ErrMaxRetries indicates that the maximum number of retries has been exceeded.
	ErrMaxRetries = errors.New("max retries exceeded")
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
func NewGeminiProcessor(config *GeminiConfig, log *logger.Logger) *GeminiProcessor {
	return &GeminiProcessor{
		config: *config,
		httpClient: &http.Client{
			Transport:     nil,
			CheckRedirect: nil,
			Jar:           nil,
			Timeout:       time.Duration(config.TimeoutSeconds) * time.Second,
		},
		logger: log,
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

// AugmentTextWithOptions enhances OCR text using image context and AI models with
// specific options.
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

	prompt, err := g.buildPromptWithOptions(ocrText, opts)
	if err != nil {
		return "", fmt.Errorf("build prompt: %w", err)
	}

	return g.tryAllModels(ctx, prompt, imageData, mimeType)
}

// validateInputs checks that the required inputs are valid.
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

// readAndEncodeImage reads an image file and returns base64 encoded data with MIME type.
func (g *GeminiProcessor) readAndEncodeImage(
	imagePath string,
) (encodedData, mimeType string, err error) {
	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return "", "", fmt.Errorf("read image file: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(imageBytes)
	detectedMimeType := g.detectImageMimeType(imagePath)

	return encoded, detectedMimeType, nil
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
	augmentationType := g.determineAugmentationType(opts)

	return g.buildPromptForType(ocrText, augmentationType), nil
}

// determineAugmentationType resolves the augmentation type from options or config
// defaults.
func (g *GeminiProcessor) determineAugmentationType(
	opts *AugmentationOptions,
) AugmentationType {
	if opts != nil && opts.Type != "" {
		return opts.Type
	}

	return g.config.AugmentationType
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
	for attempt := 1; attempt <= g.config.MaxRetries; attempt++ {
		result, err := g.attemptAPICall(
			ctx,
			model,
			prompt,
			imageData,
			mimeType,
			attempt,
		)
		if result != "" {
			return result, nil
		}

		if err != nil {
			return "", err
		}
	}

	return "", ErrMaxRetries
}

// attemptAPICall makes a single API attempt and handles retry logic.
func (g *GeminiProcessor) attemptAPICall(
	ctx context.Context,
	model, prompt, imageData, mimeType string,
	attempt int,
) (string, error) {
	result, err := g.callGeminiAPI(ctx, model, prompt, imageData, mimeType)

	if g.isSuccessfulResult(result, err) {
		return result, nil
	}

	if attempt == g.config.MaxRetries {
		return "", g.buildRetryError(model, attempt, err)
	}

	waitErr := g.waitBeforeRetry(ctx)
	if waitErr != nil {
		return "", waitErr
	}

	return "", nil
}

// isSuccessfulResult checks if the API call was successful and returned valid content.
func (g *GeminiProcessor) isSuccessfulResult(result string, err error) bool {
	return err == nil && strings.TrimSpace(result) != ""
}

// buildRetryError creates an error message for retry attempts.
func (g *GeminiProcessor) buildRetryError(model string, attempt int, err error) error {
	if err == nil {
		err = ErrEmptyResponse
	}

	return fmt.Errorf(
		"model %s attempt %d/%d: %w",
		model,
		attempt,
		g.config.MaxRetries,
		err,
	)
}

// waitBeforeRetry waits for the configured delay before the next retry.
func (g *GeminiProcessor) waitBeforeRetry(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("context canceled during retry wait: %w", ctx.Err())
	case <-time.After(time.Duration(g.config.RetryDelaySeconds) * time.Second):
		return nil
	}
}

// callGeminiAPI makes a single API call to Gemini.
func (g *GeminiProcessor) callGeminiAPI(
	ctx context.Context,
	model, prompt, imageData, mimeType string,
) (string, error) {
	url := g.buildAPIURL(model)
	reqBody := g.buildRequestBody(prompt, imageData, mimeType)

	respBytes, err := g.performHTTPRequest(ctx, url, reqBody)
	if err != nil {
		return "", err
	}

	return g.processAPIResponse(respBytes)
}

// performHTTPRequest handles the HTTP request and response reading.
func (g *GeminiProcessor) performHTTPRequest(
	ctx context.Context,
	url string,
	reqBody geminiRequest,
) ([]byte, error) {
	req, err := g.createHTTPRequest(ctx, url, reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := g.executeRequest(req)
	if err != nil {
		return nil, err
	}
	defer g.closeResponse(resp)

	respBytes, err := g.readResponseBody(resp)
	if err != nil {
		return nil, err
	}

	httpErr := g.checkHTTPError(resp.StatusCode, respBytes)
	if httpErr != nil {
		return nil, httpErr
	}

	return respBytes, nil
}

// processAPIResponse handles parsing and extracting text from the API response.
func (g *GeminiProcessor) processAPIResponse(respBytes []byte) (string, error) {
	geminiResp, err := g.parseResponse(respBytes)
	if err != nil {
		return "", err
	}

	return g.extractTextFromResponse(geminiResp)
}

// buildAPIURL constructs the Gemini API URL.
func (g *GeminiProcessor) buildAPIURL(model string) string {
	return fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model,
		g.config.APIKey,
	)
}

// buildRequestBody creates the request body for the Gemini API.
func (g *GeminiProcessor) buildRequestBody(
	prompt, imageData, mimeType string,
) geminiRequest {
	return geminiRequest{
		Contents: []geminiContent{
			{
				Role: "user",
				Parts: []geminiPart{
					{
						Text:       prompt,
						InlineData: nil,
					},
					{
						Text: "",
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
}

// createHTTPRequest creates and configures the HTTP request.
func (g *GeminiProcessor) createHTTPRequest(
	ctx context.Context,
	url string,
	reqBody geminiRequest,
) (*http.Request, error) {
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

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

	return req, nil
}

// executeRequest performs the HTTP request.
func (g *GeminiProcessor) executeRequest(req *http.Request) (*http.Response, error) {
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}

	return resp, nil
}

// closeResponse safely closes the response body.
func (g *GeminiProcessor) closeResponse(resp *http.Response) {
	closeErr := resp.Body.Close()
	if closeErr != nil {
		g.logger.Warn("Failed to close response body: %v", closeErr)
	}
}

// readResponseBody reads the response body content.
func (g *GeminiProcessor) readResponseBody(resp *http.Response) ([]byte, error) {
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return respBytes, nil
}

// checkHTTPError handles HTTP error responses.
func (g *GeminiProcessor) checkHTTPError(statusCode int, respBytes []byte) error {
	if statusCode >= 200 && statusCode < 300 {
		return nil
	}

	var apiErrResp geminiResponse
	if json.Unmarshal(respBytes, &apiErrResp) == nil && apiErrResp.Error != nil {
		return fmt.Errorf(
			"HTTP %d: %s: %w",
			statusCode,
			apiErrResp.Error.Message,
			ErrGeminiAPIError,
		)
	}

	return fmt.Errorf(
		"HTTP %d: %s: %w",
		statusCode,
		strings.TrimSpace(string(respBytes)),
		ErrGeminiAPIError,
	)
}

// parseResponse parses the JSON response from Gemini API.
func (g *GeminiProcessor) parseResponse(respBytes []byte) (geminiResponse, error) {
	var geminiResp geminiResponse

	err := json.Unmarshal(respBytes, &geminiResp)
	if err != nil {
		return geminiResp, fmt.Errorf("unmarshal response: %w", err)
	}

	if geminiResp.Error != nil && geminiResp.Error.Message != "" {
		return geminiResp, fmt.Errorf(
			"%s: %w",
			geminiResp.Error.Message,
			ErrGeminiAPIError,
		)
	}

	return geminiResp, nil
}

// extractTextFromResponse extracts text content from the API response.
func (g *GeminiProcessor) extractTextFromResponse(
	geminiResp geminiResponse,
) (string, error) {
	if len(geminiResp.Candidates) == 0 {
		return "", ErrNoCandidates
	}

	return g.buildTextFromParts(geminiResp.Candidates[0].Content.Parts), nil
}

// buildTextFromParts combines text parts into a single string.
func (g *GeminiProcessor) buildTextFromParts(parts []geminiCandidateContentPart) string {
	var textBuilder strings.Builder

	for _, part := range parts {
		if g.isValidTextPart(part) {
			g.appendTextPart(&textBuilder, part.Text)
		}
	}

	return textBuilder.String()
}

// isValidTextPart checks if a text part contains valid content.
func (g *GeminiProcessor) isValidTextPart(part geminiCandidateContentPart) bool {
	return strings.TrimSpace(part.Text) != ""
}

// appendTextPart adds a text part to the builder with proper formatting.
func (g *GeminiProcessor) appendTextPart(builder *strings.Builder, text string) {
	if builder.Len() > 0 {
		builder.WriteByte('\n')
	}

	builder.WriteString(text)
}

// prepareImageData reads and encodes the image for API consumption.
func (g *GeminiProcessor) prepareImageData(
	imagePath string,
) (imageData, mimeType string, err error) {
	imageData, mimeType, err = g.readAndEncodeImage(imagePath)
	if err != nil {
		return "", "", fmt.Errorf("read image: %w", err)
	}

	return imageData, mimeType, nil
}

// tryAllModels attempts to get a response from all configured models in order.
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

		if g.isSuccessfulResult(result, err) {
			return result, nil
		}

		lastErr = err
		g.logger.Warn("Model %s failed: %v", model, err)
	}

	return "", fmt.Errorf("all models failed, last error: %w", lastErr)
}

// getCommentaryPrompt returns the default commentary prompt.
func getCommentaryPrompt() string {
	return "You are a PhD-level technical narrator specializing in accessibility commentary. " +
		"Integrate descriptive commentary derived from the page image into the OCR text, " +
		"producing a coherent narration for text-to-speech. Focus on describing people, " +
		"environment, figures, diagrams, and visual elements like movie accessibility features. " +
		"Maintain technical accuracy, describe visual content as natural prose inserted " +
		"near relevant context, avoid markdown formatting, and output continuous paragraphs."
}

// getSummaryPrompt returns the default summary prompt.
func getSummaryPrompt() string {
	return "You are a PhD-level technical summarizer. Enhance the OCR text by adding " +
		"a comprehensive summary at the end of the content. The summary should capture " +
		"key points, main concepts, and important details from both the OCR text and " +
		"visual elements in the image. Maintain technical accuracy, avoid markdown " +
		"formatting, and structure the output as: [original OCR text] followed by " +
		"[SUMMARY: comprehensive summary paragraph]."
}

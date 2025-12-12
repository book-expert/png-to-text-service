package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/book-expert/logger"
	"github.com/book-expert/png-to-text-service/internal/events"
	"github.com/book-expert/prompt-builder/promptbuilder"
	"google.golang.org/genai"
)

const (
	MimeTypePNG = "image/png"
	RetryDelay  = 2 * time.Second
)

var (
	ErrFileEmpty    = errors.New("input file data is empty")
	ErrNoCandidates = errors.New("no candidates found in LLM response")
)

type Config struct {
	APIKey            string
	Model             string
	Temperature       float64
	TimeoutSeconds    int
	MaxRetries        int
	SystemInstruction string
	ExtractionPrompt  string
}

type Processor struct {
	client  *genai.Client
	logger  *logger.Logger
	config  Config
	builder *promptbuilder.Builder
}

// NewProcessor initializes the client.
func NewProcessor(ctx context.Context, cfg *Config, log *logger.Logger) (*Processor, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: cfg.APIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create GenAI client: %w", err)
	}

	// Initialize prompt builder with a dummy file processor as we deal with raw bytes
	builder := promptbuilder.New(promptbuilder.NewFileProcessor(0, nil))

	// Register basic presets
	// Note: These could be moved to config or external files later
	builder.AddSystemPreset("default", cfg.SystemInstruction)
	builder.AddSystemPreset("academic", "You are an academic researcher. Extract text precisely. Maintain formal tone. Annotate pauses with [short pause] or [medium pause] where commas or periods appear.")
	builder.AddSystemPreset("storyteller", "You are a dramatic storyteller. Extract text. Add [sigh], [laughing], [whispering] tags where the visual context suggests emotion.")
	builder.AddSystemPreset("news_anchor", "You are a professional news anchor. Speak clearly and authoritatively. Use [medium pause] between headlines.")

	return &Processor{
		client:  client,
		config:  *cfg,
		logger:  log,
		builder: builder,
	}, nil
}

// ProcessImage uploads, generates, and cleans up.
func (p *Processor) ProcessImage(ctx context.Context, objectID string, imageData []byte, settings events.JobSettings) (string, error) {
	if len(imageData) == 0 {
		return "", ErrFileEmpty
	}

	// 1. Upload
	uploadedFile, err := p.uploadFile(ctx, objectID, imageData)
	if err != nil {
		return "", err
	}

	// 2. Cleanup Deferral
	defer p.cleanupFile(uploadedFile.Name)

	// 3. Build Dynamic Prompt
	prompt, err := p.buildDynamicPrompt(settings, imageData)
	if err != nil {
		return "", err
	}

	// 4. Generate with Retries
	return p.generateWithRetries(ctx, uploadedFile, prompt)
}

func (p *Processor) buildDynamicPrompt(settings events.JobSettings, imageData []byte) (*promptbuilder.Prompt, error) {
	task := "default"
	if settings.StyleProfile != "" {
		task = settings.StyleProfile
	}

	// Construct User Prompt
	userPrompt := p.config.ExtractionPrompt
	if settings.CustomInstructions != "" {
		userPrompt += "\n\nCustom Instructions:\n" + settings.CustomInstructions
	}

	// Construct Guidelines from exclusions
	var guidelines []string
	if len(settings.Exclusions) > 0 {
		guidelines = append(guidelines, "STRICTLY EXCLUDE the following elements:")
		for _, ex := range settings.Exclusions {
			guidelines = append(guidelines, "- "+ex)
		}
	}

	req := &promptbuilder.BuildRequest{
		Prompt:     userPrompt,
		Task:       task,
		Guidelines: strings.Join(guidelines, "\n"),
		// Image:      imageData, // We send image via File API, not base64 in prompt for now
	}

	result, err := p.builder.BuildPrompt(req)
	if err != nil {
		return nil, err
	}
	return result.Prompt, nil
}

func (p *Processor) uploadFile(ctx context.Context, objectID string, data []byte) (*genai.File, error) {
	uploadConfig := &genai.UploadFileConfig{
		DisplayName: fmt.Sprintf("ocr-%s", objectID),
		MIMEType:    MimeTypePNG,
	}

	file, err := p.client.Files.Upload(ctx, bytes.NewReader(data), uploadConfig)
	if err != nil {
		return nil, fmt.Errorf("upload failed: %w", err)
	}
	return file, nil
}

func (p *Processor) cleanupFile(fileName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := p.client.Files.Delete(ctx, fileName, nil); err != nil {
		p.logger.Warnf(fmt.Sprintf("Failed to delete remote file %s: %v", fileName, err))
	}
}

func (p *Processor) generateWithRetries(ctx context.Context, file *genai.File, prompt *promptbuilder.Prompt) (string, error) {
	var lastError error

	for attempt := 1; attempt <= p.config.MaxRetries; attempt++ {
		result, err := p.callGenAIModel(ctx, file, prompt)

		if err == nil {
			return result, nil
		}

		lastError = err
		p.logger.Warnf(fmt.Sprintf("LLM attempt %d/%d failed: %v", attempt, p.config.MaxRetries, err))

		if attempt < p.config.MaxRetries {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(RetryDelay):
				continue
			}
		}
	}

	return "", fmt.Errorf("all %d attempts failed: %w", p.config.MaxRetries, lastError)
}

func (p *Processor) callGenAIModel(parentCtx context.Context, file *genai.File, prompt *promptbuilder.Prompt) (string, error) {
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(p.config.TimeoutSeconds)*time.Second)
	defer cancel()

	temperature := float32(p.config.Temperature)

	// Combine UserPrompt and Guidelines
	finalUserPrompt := prompt.UserPrompt
	if prompt.Guidelines != "" {
		finalUserPrompt += "\n\n" + prompt.Guidelines
	}

	resp, err := p.client.Models.GenerateContent(
		ctx,
		p.config.Model,
		[]*genai.Content{
			{
				Parts: []*genai.Part{
					{FileData: &genai.FileData{FileURI: file.URI, MIMEType: file.MIMEType}},
					{Text: finalUserPrompt},
				},
			},
		},
		&genai.GenerateContentConfig{
			Temperature: &temperature,
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: prompt.SystemMessage}},
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf("generation failed: %w", err)
	}

	if resp == nil || len(resp.Candidates) == 0 {
		return "", ErrNoCandidates
	}

	var sb strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		sb.WriteString(part.Text)
	}

	return sb.String(), nil
}

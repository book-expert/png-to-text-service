/*
GOLDEN RULES & DEVELOPER MANIFESTO (THE NORTH STAR)
--------------------------------------------------------------------------------
"Work is love made visible. And if you cannot work with love but only with
distaste, it is better that you should leave your work and sit at the gate of
the temple and take alms of those who work with joy." — Kahlil Gibran

1.  LOVE AND CARE (Primary Driver)
    - This is a craft. Build with pride, honesty, and kindness.
    - If you put love in your work, you build something deserving of love.
    - Be helpful: Code is read more than written; optimize for the reader.

2.  WRITE WHAT YOU MEAN (Explicit > Implicit)
    - Use WHOLE WORDS: `RequestIdentifier` not `ReqID`.
    - No magic numbers: Move application settings to `project.toml`.
    - Secure by design: Keep API keys and secrets strictly in `.env`.
    - No ambiguity: If you assume something, document it.

3.  SIMPLE IS EFFICIENT (Minimal Viable Elegance)
    - Avoid over-engineering. Small interfaces, clear structs.
    - If a design requires a hack, stop. Redesign it with elegance.
    - Lean, Clean, Mean: Delete dead code immediately.

4.  NO BASELESS ASSUMPTIONS (Scientific Rigor)
    - Do not guess. Base decisions on documentation and proven patterns.
    - If you do not know, ask or verify.

5.  NON-BLOCKING & ROBUST
    - Never block the main goroutine. Use Context for cancellation.
    - Handle errors explicitly: Don't just return them, wrap them with context.

--------------------------------------------------------------------------------
EXAMPLES OF "LOVE AND CARE" IN THIS CONTEXT:
--------------------------------------------------------------------------------
(A) NAMING
    Indifferent:  func Gen(t string, v string)
    With Love:    func GenerateSoundscape(ctx context.Context, textPrompt string, voiceID string)
    *Why: The Agent reading this next year will know exactly what it does and that it is cancellable.*

(B) CONFIGURATION
    Indifferent:  const Timeout = 30 // Hardcoded
    With Love:    config.App.TimeoutSeconds // Loaded from project.toml
    *Why: Allows behavior tuning without recompiling or touching the codebase.*

(C) ERROR HANDLING
    Indifferent:  if err != nil { return err }
    With Love:    if err != nil { return fmt.Errorf("failed to initialize vox engine: %w", err) }
    *Why: Wrapping the error gives the user the 'trace of breadcrumbs' they need to fix it. That is kindness.*
--------------------------------------------------------------------------------
*/

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
	client *genai.Client
	logger *logger.Logger
	config Config
}

// NewProcessor initializes the client.
func NewProcessor(ctx context.Context, cfg *Config, log *logger.Logger) (*Processor, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: cfg.APIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create GenAI client: %w", err)
	}

	return &Processor{
		client: client,
		config: *cfg,
		logger: log,
	}, nil
}

// ProcessImage uploads, generates, and cleans up.
func (p *Processor) ProcessImage(ctx context.Context, objectID string, imageData []byte, settings *events.JobSettings) (string, error) {
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

	// 3. Build Vision Prompt (System Instruction)
	// We combine:
	// - Base instruction from config (TOML)
	// - User-defined exclusions (Settings.Exclusions)
	// - User-defined augmentation (Settings.AugmentationPrompt)
	// - AI-generated Text Directives (Settings.AudioSessionConfig.TextDirective)
	var exclusions, augmentation string
	if settings != nil {
		exclusions = settings.Exclusions
		augmentation = settings.AugmentationPrompt
	}

	var textDirective string
	if settings != nil && settings.AudioSessionConfig != nil {
		textDirective = settings.AudioSessionConfig.TextDirective
	}

	systemInstruction := p.buildVisionSystemInstruction(exclusions, augmentation, textDirective)

	// User prompt: Simple directive to execute the system instruction.
	// We can use the ExtractionPrompt from TOML if available.
	userPrompt := p.config.ExtractionPrompt
	if userPrompt == "" {
		userPrompt = "Extract the text from this image."
	}

	// 4. Generate with Retries (Extract Text)
	extractedText, err := p.generateWithRetries(ctx, uploadedFile, systemInstruction, userPrompt)
	if err != nil {
		return "", err
	}

	// 5. Return Clean Text (No Headers)
	return extractedText, nil
}

func (p *Processor) buildVisionSystemInstruction(exclusions string, augmentation string, textDirective string) string {
	// Start with the base instruction from TOML
	instruction := p.config.SystemInstruction

	// If TOML was empty, fall back to a reasonable default (though TOML should be source of truth)
	if instruction == "" {
		instruction = `You are an expert narrator. Extract text cleanly.`
	}

	// Append dynamic exclusions
	if exclusions != "" {
		instruction += "\n\nCRITICAL EXCLUSIONS (Do NOT Read):\n"
		instruction += exclusions
	}

	// Append AI-generated structural directives
	if textDirective != "" {
		instruction += "\n\nSTRUCTURAL CLEANUP RULES:\n"
		instruction += textDirective
	}

	// Append User Augmentation (Descriptive capabilities)
	if augmentation != "" {
		instruction += "\n\nNARRATIVE AUGMENTATION REQUEST:\n"
		instruction += augmentation
		instruction += "\n\n(Note: You are permitted to insert descriptive text for visuals or explanations IF requested above. Integrate these naturally into the narrative flow, without using brackets, labels, or special tags like [DESCRIPTION]. The goal is a seamless audio book experience.)"
	} else {
		// Strict pure transcript mode if no augmentation requested
		instruction += "\n\nCRITICAL: Output ONLY the spoken text. Do NOT output metadata, headers, scene descriptions, or music cues. The output must be pure transcript."
	}

	instruction += "\n\nIf the page consists PRIMARILY of excluded content (like a full References page), output ONLY the string \"[NO_SPEECH]\"."

	return instruction
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

func (p *Processor) generateWithRetries(ctx context.Context, file *genai.File, systemInstruction string, userPrompt string) (string, error) {
	var lastError error

	for attempt := 1; attempt <= p.config.MaxRetries; attempt++ {
		result, err := p.callGenAIModel(ctx, file, systemInstruction, userPrompt)

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

func (p *Processor) callGenAIModel(parentCtx context.Context, file *genai.File, systemInstruction string, userPrompt string) (string, error) {
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(p.config.TimeoutSeconds)*time.Second)
	defer cancel()

	temperature := float32(p.config.Temperature)

	// We rely on the System Instruction to enforce the format.
	resp, err := p.client.Models.GenerateContent(
		ctx,
		p.config.Model,
		[]*genai.Content{
			{
				Parts: []*genai.Part{
					{FileData: &genai.FileData{FileURI: file.URI, MIMEType: file.MIMEType}},
					{Text: userPrompt},
				},
			},
		},
		&genai.GenerateContentConfig{
			Temperature: &temperature,
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: systemInstruction}},
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

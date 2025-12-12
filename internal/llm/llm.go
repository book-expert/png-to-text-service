package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/book-expert/logger"
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
func (p *Processor) ProcessImage(ctx context.Context, objectID string, imageData []byte) (string, error) {
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

	// 3. Generate with Retries
	return p.generateWithRetries(ctx, uploadedFile)
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

func (p *Processor) generateWithRetries(ctx context.Context, file *genai.File) (string, error) {
	var lastError error

	for attempt := 1; attempt <= p.config.MaxRetries; attempt++ {
		result, err := p.callGenAIModel(ctx, file)

		if err == nil {
			return result, nil
		}

		lastError = err
		// TODO: Remove sprintf stuff
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

func (p *Processor) callGenAIModel(parentCtx context.Context, file *genai.File) (string, error) {
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(p.config.TimeoutSeconds)*time.Second)
	defer cancel()

	temperature := float32(p.config.Temperature)

	resp, err := p.client.Models.GenerateContent(
		ctx,
		p.config.Model,
		[]*genai.Content{
			{
				Parts: []*genai.Part{
					{FileData: &genai.FileData{FileURI: file.URI, MIMEType: file.MIMEType}},
					{Text: p.config.ExtractionPrompt},
				},
			},
		},
		&genai.GenerateContentConfig{
			Temperature: &temperature,
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: p.config.SystemInstruction}},
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

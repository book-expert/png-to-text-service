/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */

package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/book-expert/common-events"
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

func NewProcessor(parentContext context.Context, configuration *Config, serviceLogger *logger.Logger) (*Processor, error) {
	client, clientError := genai.NewClient(parentContext, &genai.ClientConfig{
		APIKey: configuration.APIKey,
	})
	if clientError != nil {
		return nil, fmt.Errorf("failed to create GenAI client: %w", clientError)
	}

	return &Processor{
		client: client,
		config: *configuration,
		logger: serviceLogger,
	}, nil
}

func (processor *Processor) ProcessImage(parentContext context.Context, objectID string, imageData []byte, settings *events.JobSettings) (string, error) {
	if len(imageData) == 0 {
		return "", ErrFileEmpty
	}

	uploadedFile, uploadError := processor.uploadFile(parentContext, objectID, imageData)
	if uploadError != nil {
		return "", uploadError
	}
	defer processor.cleanupFile(uploadedFile.Name)

	var exclusions, augmentation string
	if settings != nil {
		exclusions = settings.Exclusions
		augmentation = settings.AugmentationPrompt
	}

	var textDirective string
	if settings != nil && settings.AudioSessionConfig != nil {
		textDirective = settings.AudioSessionConfig.TextDirective
	}

	systemInstruction := processor.buildVisionSystemInstruction(exclusions, augmentation, textDirective)

	userPrompt := processor.config.ExtractionPrompt
	if userPrompt == "" {
		userPrompt = "Extract the text from this image."
	}

	extractedText, generationError := processor.generateWithRetries(parentContext, uploadedFile, systemInstruction, userPrompt)
	if generationError != nil {
		return "", generationError
	}

	return extractedText, nil
}

func (processor *Processor) buildVisionSystemInstruction(exclusions string, augmentation string, textDirective string) string {
	instruction := processor.config.SystemInstruction
	if instruction == "" {
		instruction = "You are an expert narrator. Extract text cleanly."
	}

	if exclusions != "" {
		instruction += "\n\nCRITICAL EXCLUSIONS (Do NOT Read):\n" + exclusions
	}

	if textDirective != "" {
		instruction += "\n\nSTRUCTURAL CLEANUP RULES:\n" + textDirective
	}

	if augmentation != "" {
		instruction += "\n\nNARRATIVE AUGMENTATION REQUEST:\n" + augmentation + "\n\n(Note: You are permitted to insert descriptive text for visuals or explanations IF requested above. Integrate these naturally into the narrative flow, without using brackets, labels, or special tags like [DESCRIPTION]. The goal is a seamless audio book experience.)"
	} else {
		instruction += "\n\nCRITICAL: Output ONLY the spoken text. Do NOT output metadata, headers, scene descriptions, or music cues. The output must be pure transcript."
	}

	instruction += "\n\nIf the page consists PRIMARILY of excluded content (like a full References page), output ONLY the string \"[NO_SPEECH]\"."

	return instruction
}

func (processor *Processor) uploadFile(parentContext context.Context, objectID string, data []byte) (*genai.File, error) {
	uploadConfig := &genai.UploadFileConfig{
		DisplayName: fmt.Sprintf("ocr-%s", objectID),
		MIMEType:    MimeTypePNG,
	}

	file, uploadError := processor.client.Files.Upload(parentContext, bytes.NewReader(data), uploadConfig)
	if uploadError != nil {
		return nil, fmt.Errorf("upload failed: %w", uploadError)
	}
	return file, nil
}

func (processor *Processor) cleanupFile(fileName string) {
	cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCleanup()

	if _, deletionError := processor.client.Files.Delete(cleanupContext, fileName, nil); deletionError != nil {
		processor.logger.Warnf("Failed to delete remote file %s: %v", fileName, deletionError)
	}
}

func (processor *Processor) generateWithRetries(parentContext context.Context, file *genai.File, systemInstruction string, userPrompt string) (string, error) {
	var lastError error

	for attempt := 1; attempt <= processor.config.MaxRetries; attempt++ {
		result, callError := processor.callGenAIModel(parentContext, file, systemInstruction, userPrompt)
		if callError == nil {
			return result, nil
		}

		lastError = callError
		processor.logger.Warnf("LLM attempt %d/%d failed: %v", attempt, processor.config.MaxRetries, callError)

		if attempt < processor.config.MaxRetries {
			select {
			case <-parentContext.Done():
				return "", parentContext.Err()
			case <-time.After(RetryDelay):
				continue
			}
		}
	}

	return "", fmt.Errorf("all %d attempts failed: %w", processor.config.MaxRetries, lastError)
}

func (processor *Processor) callGenAIModel(parentContext context.Context, file *genai.File, systemInstruction string, userPrompt string) (string, error) {
	generationContext, cancelGeneration := context.WithTimeout(parentContext, time.Duration(processor.config.TimeoutSeconds)*time.Second)
	defer cancelGeneration()

	temperature := float32(processor.config.Temperature)

	response, generationError := processor.client.Models.GenerateContent(
		generationContext,
		processor.config.Model,
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
	if generationError != nil {
		return "", fmt.Errorf("generation failed: %w", generationError)
	}

	if response == nil || len(response.Candidates) == 0 {
		return "", ErrNoCandidates
	}

	var stringBuilder strings.Builder
	for _, part := range response.Candidates[0].Content.Parts {
		stringBuilder.WriteString(part.Text)
	}

	return stringBuilder.String(), nil
}

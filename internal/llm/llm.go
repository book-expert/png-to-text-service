/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */

package llm

/*
#cgo LDFLAGS: -lzigllm
#include <llm.h>
#include <stdlib.h>
*/
import "C"
import (
	"context"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/book-expert/common-events"
	"github.com/book-expert/logger"
)

const (
	RetryDelay = 2 * time.Second
)

var (
	ErrFileEmpty    = errors.New("input file data is empty")
	ErrNoCandidates = errors.New("no candidates found in LLM response")
)

type Config struct {
	APIKey            string
	Model             string
	Temperature       float64
	MaxOutputTokens   int
	TimeoutSeconds    int
	MaxRetries        int
	SystemInstruction string
	ExtractionPrompt  string
}

type Processor struct {
	handle C.zig_llm_handle_t
	logger *logger.Logger
	config Config
}

func NewProcessor(_ context.Context, configuration *Config, serviceLogger *logger.Logger) (*Processor, error) {
	apiKey := C.CString(configuration.APIKey)
	defer C.free(unsafe.Pointer(apiKey))
	model := C.CString(configuration.Model)
	defer C.free(unsafe.Pointer(model))

	zigConfig := C.zig_llm_config_t{
		api_key:           apiKey,
		model:             model,
		max_output_tokens: C.int32_t(configuration.MaxOutputTokens),
		temperature:       C.float(configuration.Temperature),
	}

	handle := C.zig_llm_init(zigConfig)
	if handle == nil {
		return nil, errors.New("failed to initialize Zig LLM client")
	}

	return &Processor{
		handle: handle,
		config: *configuration,
		logger: serviceLogger,
	}, nil
}

func (processor *Processor) Close() {
	if processor.handle != nil {
		C.zig_llm_deinit(processor.handle)
		processor.handle = nil
	}
}

func (processor *Processor) ProcessImage(parentContext context.Context, _ string, imageData []byte, settings *events.JobSettings) (string, error) {
	if len(imageData) == 0 {
		return "", ErrFileEmpty
	}

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

	extractedText, generationError := processor.generateWithRetries(parentContext, imageData, systemInstruction, userPrompt)
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

func (processor *Processor) generateWithRetries(parentContext context.Context, imageData []byte, systemInstruction string, userPrompt string) (string, error) {
	var lastError error

	for attempt := 1; attempt <= processor.config.MaxRetries; attempt++ {
		result, callError := processor.callZigLLM(parentContext, imageData, systemInstruction, userPrompt)
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

func (processor *Processor) callZigLLM(_ context.Context, imageData []byte, systemInstruction string, userPrompt string) (string, error) {
	cSystemInstruction := C.CString(systemInstruction)
	defer C.free(unsafe.Pointer(cSystemInstruction))
	cUserPrompt := C.CString(userPrompt)
	defer C.free(unsafe.Pointer(cUserPrompt))
	cMimeType := C.CString("image/png")
	defer C.free(unsafe.Pointer(cMimeType))

	cImageData := (*C.char)(unsafe.Pointer(&imageData[0]))
	cImageLen := C.size_t(len(imageData))

	resultPtr := C.zig_llm_prompt_inline(
		processor.handle,
		cSystemInstruction,
		cUserPrompt,
		cImageData,
		cImageLen,
		cMimeType,
	)

	if resultPtr == nil {
		return "", errors.New("zig llm prompt failed")
	}
	defer C.zig_llm_free_string(resultPtr)

	return C.GoString(resultPtr), nil
}

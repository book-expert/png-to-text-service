/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */

package llm

/*
#cgo LDFLAGS: -lcppclient
#include <llm.h>
#include <stdlib.h>
#include <string.h>
*/
import "C"

import (
	"context"
	"errors"
	"time"
	"unsafe"

	events "github.com/book-expert/common-events"
	"github.com/book-expert/logger"
)

const (
	RetryDelay                   = 2 * time.Second
	DefaultMimeTypePNG           = "image/png"
	DefaultExtractionUserPrompt  = "Extract the text from this image."
	DefaultSystemInstruction     = "You are an expert narrator. Extract text cleanly."
	ExclusionInstructionFormat   = "\n\nCRITICAL EXCLUSIONS (Do NOT Read):\n"
	StructuralInstructionFormat  = "\n\nSTRUCTURAL CLEANUP RULES:\n"
	AugmentationInstructionFormat = "\n\nNARRATIVE AUGMENTATION REQUEST:\n"
	AugmentationNote              = "\n\n(Note: You are permitted to insert descriptive text for visuals or explanations IF requested above. Integrate these naturally into the narrative flow, without using brackets, labels, or special tags like [DESCRIPTION]. The goal is a seamless audio book experience.)"
)

var (
	ErrFileEmpty    = errors.New("input file data is empty")
	ErrLlmInitFailed = errors.New("failed to initialize C++ LLM client")
)

type Config struct {
	APIKey            string
	Model             string
	LlmAddress        string
	Provider          string
	Temperature       float64
	MaxTokens         int
	TimeoutSeconds    int
	MaxRetries        int
	SystemInstruction string
	ExtractionPrompt  string
}

type Processor struct {
	handle        C.llm_handle_t
	serviceLogger *logger.Logger
	configuration Config
}

func NewProcessor(_ context.Context, configuration *Config, serviceLogger *logger.Logger) (*Processor, error) {
	apiKey := C.CString(configuration.APIKey)
	defer C.free(unsafe.Pointer(apiKey))
	
	model := C.CString(configuration.Model)
	defer C.free(unsafe.Pointer(model))
	
	llmAddress := C.CString(configuration.LlmAddress)
	defer C.free(unsafe.Pointer(llmAddress))

	provider := C.CString(configuration.Provider)
	defer C.free(unsafe.Pointer(provider))

	cppConfiguration := C.LlmConfig{
		api_key:            apiKey,
		api_key_length:     C.uint32_t(len(configuration.APIKey)),
		model:              model,
		model_length:       C.uint32_t(len(configuration.Model)),
		llm_address:        llmAddress,
		llm_address_length: C.uint32_t(len(configuration.LlmAddress)),
		provider:           provider,
		provider_length:    C.uint32_t(len(configuration.Provider)),
		max_tokens:         C.int32_t(configuration.MaxTokens),
		temperature:        C.float(configuration.Temperature),
	}

	handle := C.llm_init(cppConfiguration)
	if handle == nil {
		return nil, ErrLlmInitFailed
	}

	return &Processor{
		handle:        handle,
		configuration: *configuration,
		serviceLogger: serviceLogger,
	}, nil
}

func (processor *Processor) Close() {
	if processor.handle != nil {
		C.llm_deinit(processor.handle)
		processor.handle = nil
	}
}

func (processor *Processor) ProcessImage(
	parentContext context.Context,
	_ string,
	imageData []byte,
	settings *events.JobSettings,
	refinedPrompt string,
	responseMimeType string,
	responseJsonSchema string,
) (string, error) {
	if len(imageData) == 0 {
		return "", ErrFileEmpty
	}

	systemInstruction := refinedPrompt
	if systemInstruction == "" {
		var exclusions, augmentation string
		if settings != nil {
			exclusions = settings.Exclusions
			augmentation = settings.AugmentationPrompt
		}

		var textDirective string
		if settings != nil && settings.AudioSessionConfig != nil {
			textDirective = settings.AudioSessionConfig.TextDirective
		}

		systemInstruction = processor.buildVisionSystemInstruction(exclusions, augmentation, textDirective)
	}

	userPrompt := processor.configuration.ExtractionPrompt
	if userPrompt == "" {
		userPrompt = DefaultExtractionUserPrompt
	}

	return processor.processImageWithRetries(parentContext, imageData, DefaultMimeTypePNG, systemInstruction, userPrompt, responseMimeType, responseJsonSchema)
}

func (processor *Processor) buildVisionSystemInstruction(exclusions string, augmentation string, textDirective string) string {
	instruction := processor.configuration.SystemInstruction
	if instruction == "" {
		instruction = DefaultSystemInstruction
	}

	if exclusions != "" {
		instruction += ExclusionInstructionFormat + exclusions
	}

	if textDirective != "" {
		instruction += StructuralInstructionFormat + textDirective
	}

	if augmentation != "" {
		instruction += AugmentationInstructionFormat + augmentation + AugmentationNote
	}

	return instruction
}

func (processor *Processor) processImageWithRetries(
	parentContext context.Context,
	data []byte,
	mimeType string,
	systemInstruction string,
	userPrompt string,
	responseMimeType string,
	responseJsonSchema string,
) (string, error) {
	var finalError error

	dataPointer := unsafe.Pointer(&data[0])
	
	cMimeType := C.CString(mimeType)
	defer C.free(unsafe.Pointer(cMimeType))
	
	cSystemInstruction := C.CString(systemInstruction)
	defer C.free(unsafe.Pointer(cSystemInstruction))
	
	cUserPrompt := C.CString(userPrompt)
	defer C.free(unsafe.Pointer(cUserPrompt))

	var cResponseMimeType, cResponseJsonSchema *C.char
	if responseMimeType != "" {
		cResponseMimeType = C.CString(responseMimeType)
		defer C.free(unsafe.Pointer(cResponseMimeType))
	}
	if responseJsonSchema != "" {
		cResponseJsonSchema = C.CString(responseJsonSchema)
		defer C.free(unsafe.Pointer(cResponseJsonSchema))
	}

	for attempt := 1; attempt <= processor.configuration.MaxRetries; attempt++ {
		resultPointer := C.llm_process_image(
			processor.handle,
			dataPointer, C.uint32_t(len(data)),
			cMimeType, C.uint32_t(len(mimeType)),
			cSystemInstruction, C.uint32_t(len(systemInstruction)),
			cUserPrompt, C.uint32_t(len(userPrompt)),
			cResponseMimeType, C.uint32_t(len(responseMimeType)),
			cResponseJsonSchema, C.uint32_t(len(responseJsonSchema)),
		)

		if resultPointer != nil {
			text := C.GoString(resultPointer)
			C.llm_free_string(resultPointer)
			return text, nil
		}

		finalError = errors.New("orchestration failed in C++")
		processor.serviceLogger.Warnf("Process image attempt %d/%d failed", attempt, processor.configuration.MaxRetries)

		if attempt < processor.configuration.MaxRetries {
			select {
			case <-parentContext.Done():
				return "", parentContext.Err()
			case <-time.After(RetryDelay):
				continue
			}
		}
	}
	return "", finalError
}

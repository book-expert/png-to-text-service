/* DO EVERYTHING WITH LOVE, CARE, HONESTY, TRUTH, TRUST, KINDNESS, RELIABILITY, CONSISTENCY, DISCIPLINE, RESILIENCE, CRAFTSMANSHIP, HUMILITY, ALLIANCE, EXPLICITNESS */

package llm

/*
#cgo LDFLAGS: -lzigllm
#include <llm.h>
#include <stdlib.h>
#include <string.h>

void GoHttpClientCallback(
	void* allocator_handle,
	char* method,
	char* url,
	char* headers,
	void* body,
	uint32_t body_length,
	char** response_body_output,
	uint32_t* response_body_length_output,
	uint16_t* status_output
);
*/
import "C"

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	AllocationFloor              = 20 * 1024 * 1024
	DefaultTimeout               = 120 * time.Second
)

var (
	ErrFileEmpty    = errors.New("input file data is empty")
	ErrNoCandidates = errors.New("no candidates found in LLM response")
)

type Config struct {
	APIKey            string
	Model             string
	BaseURL           string
	Temperature       float64
	MaxOutputTokens   int
	TimeoutSeconds    int
	MaxRetries        int
	SystemInstruction string
	ExtractionPrompt  string
}

type Processor struct {
	handle        C.zig_llm_handle_t
	serviceLogger *logger.Logger
	configuration Config
}

var httpClient = &http.Client{
	Timeout: DefaultTimeout,
}

func NewProcessor(_ context.Context, configuration *Config, serviceLogger *logger.Logger) (*Processor, error) {
	apiKey := unsafe.Pointer(unsafe.StringData(configuration.APIKey))
	model := unsafe.Pointer(unsafe.StringData(configuration.Model))
	baseURL := unsafe.Pointer(unsafe.StringData(configuration.BaseURL))

	zigConfiguration := C.zig_llm_config_t{
		api_key:           apiKey,
		api_key_length:    C.uint32_t(len(configuration.APIKey)),
		model:             model,
		model_length:      C.uint32_t(len(configuration.Model)),
		base_url:          baseURL,
		base_url_length:   C.uint32_t(len(configuration.BaseURL)),
		max_output_tokens: C.int32_t(configuration.MaxOutputTokens),
		temperature:       C.float(configuration.Temperature),
		http_callback:     (C.zig_llm_http_callback)(unsafe.Pointer(C.GoHttpClientCallback)),
	}

	handle := C.zig_llm_init(zigConfiguration)
	if handle == nil {
		return nil, errors.New("failed to initialize high-integrity Zig LLM client")
	}

	return &Processor{
		handle:        handle,
		configuration: *configuration,
		serviceLogger: serviceLogger,
	}, nil
}

//export GoHttpClientCallback
func GoHttpClientCallback(
	allocatorHandle unsafe.Pointer,
	method *C.char,
	url *C.char,
	headers *C.char,
	body unsafe.Pointer,
	bodyLength C.uint32_t,
	responseBodyOutput **C.char,
	responseBodyLengthOutput *C.uint32_t,
	statusOutput *C.uint16_t,
) {
	goMethod := C.GoString(method)
	goUrl := C.GoString(url)
	goHeaders := C.GoString(headers)
	goBody := C.GoBytes(body, C.int(bodyLength))

	request, error := http.NewRequest(goMethod, goUrl, bytes.NewReader(goBody))
	if error != nil {
		*statusOutput = 500
		return
	}

	lines := strings.Split(goHeaders, "\r\n")
	for _, line := range lines {
		if parts := strings.SplitN(line, ": ", 2); len(parts) == 2 {
			request.Header.Set(parts[0], parts[1])
		}
	}

	response, error := httpClient.Do(request)
	if error != nil {
		*statusOutput = 500
		return
	}
	defer func() { _ = response.Body.Close() }()

	*statusOutput = C.uint16_t(response.StatusCode)

	var buffer bytes.Buffer
	buffer.Grow(AllocationFloor)
	_, _ = io.Copy(&buffer, response.Body)
	resultBody := buffer.Bytes()

	fmt.Printf("[GO] Received streaming body length: %d (Enforcing 20MB allocation floor)\n", len(resultBody))

	*responseBodyLengthOutput = C.uint32_t(len(resultBody))

	allocSize := len(resultBody)
	if allocSize < AllocationFloor {
		allocSize = AllocationFloor
	}

	cBody := C.zig_llm_alloc(allocatorHandle, C.uint32_t(allocSize))
	if cBody != nil {
		if len(resultBody) > 0 {
			C.memcpy(cBody, unsafe.Pointer(&resultBody[0]), C.size_t(len(resultBody)))
		}
		*responseBodyOutput = (*C.char)(cBody)
	}
}

func (processor *Processor) Close() {
	if processor.handle != nil {
		C.zig_llm_deinit(processor.handle)
		processor.handle = nil
	}
}

func (processor *Processor) ProcessImage(
	parentContext context.Context,
	_ string,
	imageData []byte,
	settings *events.JobSettings,
	responseMimeType string,
	responseJsonSchema string,
) (string, error) {
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
	mimeTypePointer := unsafe.Pointer(unsafe.StringData(mimeType))
	systemInstructionPointer := unsafe.Pointer(unsafe.StringData(systemInstruction))
	userPromptPointer := unsafe.Pointer(unsafe.StringData(userPrompt))

	var responseMimeTypePointer, responseJsonSchemaPointer unsafe.Pointer
	if responseMimeType != "" {
		responseMimeTypePointer = unsafe.Pointer(unsafe.StringData(responseMimeType))
	}
	if responseJsonSchema != "" {
		responseJsonSchemaPointer = unsafe.Pointer(unsafe.StringData(responseJsonSchema))
	}

	for attempt := 1; attempt <= processor.configuration.MaxRetries; attempt++ {
		resultPointer := C.zig_llm_process_image(
			processor.handle,
			dataPointer, C.uint32_t(len(data)),
			mimeTypePointer, C.uint32_t(len(mimeType)),
			systemInstructionPointer, C.uint32_t(len(systemInstruction)),
			userPromptPointer, C.uint32_t(len(userPrompt)),
			responseMimeTypePointer, C.uint32_t(len(responseMimeType)),
			responseJsonSchemaPointer, C.uint32_t(len(responseJsonSchema)),
		)

		if resultPointer != nil {
			text := C.GoString(resultPointer)
			C.zig_llm_free_string(resultPointer)
			return text, nil
		}

		finalError = errors.New("high-integrity orchestration failed in Zig")
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

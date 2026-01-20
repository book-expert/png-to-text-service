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
	size_t body_length,
	char** response_body_out,
	size_t* response_body_length_out,
	uint16_t* status_out
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
	handle        C.zig_llm_handle_t
	serviceLogger *logger.Logger
	configuration Config
}

var (
	httpClient = &http.Client{
		Timeout: 90 * time.Second,
	}
)

func NewProcessor(_ context.Context, configuration *Config, serviceLogger *logger.Logger) (*Processor, error) {
	apiKey := unsafe.Pointer(unsafe.StringData(configuration.APIKey))
	model := unsafe.Pointer(unsafe.StringData(configuration.Model))

	zigConfiguration := C.zig_llm_config_t{
		api_key:           apiKey,
		api_key_len:       C.size_t(len(configuration.APIKey)),
		model:             model,
		model_len:         C.size_t(len(configuration.Model)),
		max_output_tokens: C.int32_t(configuration.MaxOutputTokens),
		temperature:       C.float(configuration.Temperature),
		http_callback:     (C.zig_llm_http_callback)(unsafe.Pointer(C.GoHttpClientCallback)),
	}

	handle := C.zig_llm_init(zigConfiguration)
	if (handle == nil) {
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
	bodyLength C.size_t,
	responseBodyOut **C.char,
	responseBodyLengthOut *C.size_t,
	statusOut *C.uint16_t,
) {
	goMethod := C.GoString(method)
	goUrl := C.GoString(url)
	goHeaders := C.GoString(headers)
	goBody := C.GoBytes(body, C.int(bodyLength))

	req, err := http.NewRequest(goMethod, goUrl, bytes.NewReader(goBody))
	if err != nil {
		*statusOut = 500
		return
	}

	// Set Headers
	lines := strings.Split(goHeaders, "\r\n")
	for _, line := range lines {
		if parts := strings.SplitN(line, ": ", 2); len(parts) == 2 {
			req.Header.Set(parts[0], parts[1])
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		*statusOut = 500
		return
	}
	defer func() { _ = resp.Body.Close() }()

	*statusOut = C.uint16_t(resp.StatusCode)

	var resultBody []byte
	// Special Case: Step 1 of Gemini Resumable Upload
	// We need to return the X-Goog-Upload-URL header as the body
	if resp.Header.Get("X-Goog-Upload-URL") != "" {
		resultBody = []byte(resp.Header.Get("X-Goog-Upload-URL"))
	} else {
		resultBody, _ = io.ReadAll(resp.Body)
	}

	fmt.Printf("[GO] Received body length: %d\n", len(resultBody))

	*responseBodyLengthOut = C.size_t(len(resultBody))
	if len(resultBody) > 0 {
		cBody := C.zig_llm_alloc(allocatorHandle, C.size_t(len(resultBody)))
		if cBody != nil {
			C.memcpy(cBody, unsafe.Pointer(&resultBody[0]), C.size_t(len(resultBody)))
			*responseBodyOut = (*C.char)(cBody)
		}
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
		userPrompt = "Extract the text from this image."
	}

	return processor.processImageWithRetries(parentContext, imageData, "image/png", systemInstruction, userPrompt, responseMimeType, responseJsonSchema)
}

func (processor *Processor) buildVisionSystemInstruction(exclusions string, augmentation string, textDirective string) string {
	instruction := processor.configuration.SystemInstruction
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

func (processor *Processor) processImageWithRetries(
	parentContext context.Context,
	data []byte,
	mimeType string,
	systemInstruction string,
	userPrompt string,
	responseMimeType string,
	responseJsonSchema string,
) (string, error) {
	var lastError error

	pData := unsafe.Pointer(&data[0])
	pMime := unsafe.Pointer(unsafe.StringData(mimeType))
	pSys := unsafe.Pointer(unsafe.StringData(systemInstruction))
	pUsr := unsafe.Pointer(unsafe.StringData(userPrompt))

	var pRespMime, pRespSchema unsafe.Pointer
	if responseMimeType != "" {
		pRespMime = unsafe.Pointer(unsafe.StringData(responseMimeType))
	}
	if responseJsonSchema != "" {
		pRespSchema = unsafe.Pointer(unsafe.StringData(responseJsonSchema))
	}

	for attempt := 1; attempt <= processor.configuration.MaxRetries; attempt++ {
		resultPointer := C.zig_llm_process_image(
			processor.handle,
			pData, C.size_t(len(data)),
			pMime, C.size_t(len(mimeType)),
			pSys, C.size_t(len(systemInstruction)),
			pUsr, C.size_t(len(userPrompt)),
			pRespMime, C.size_t(len(responseMimeType)),
			pRespSchema, C.size_t(len(responseJsonSchema)),
		)

		if resultPointer != nil {
			text := C.GoString(resultPointer)
			C.zig_llm_free_string(resultPointer)
			return text, nil
		}

		lastError = errors.New("high-integrity orchestration failed in Zig")
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
	return "", lastError
}

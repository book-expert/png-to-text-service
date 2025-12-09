# PNG-to-Text Service

A lightweight, event-driven microservice that transforms PNG images into narration-ready text using Google's Gemini LLM.

## Overview

This service is a key component of a document-to-audio pipeline. It consumes `pngs.created` events from NATS, downloads the corresponding image, and uses the Gemini 2.5 Flash model to:

1.  **Extract** text with high accuracy.
2.  **Clean** artifacts (citations, headers, footers).
3.  **Expand** acronyms and describe visual elements for clarity.
4.  **Format** the output as a clean JSON array of strings.

The result is stored in a NATS Object Store, ready for a Text-to-Speech (TTS) service.

## Architecture

The service follows a stateless worker pattern:

1.  **Worker**: Listens to a NATS JetStream queue group.
2.  **LLM Processor**: Uploads the image to the Google GenAI File API and generates content using a strict system prompt.
3.  **Publisher**: Stores the result and emits a `texts.processed` event.

## Prerequisites

*   **Go**: 1.22+
*   **NATS Server**: With JetStream enabled.
*   **Gemini API Key**: A valid Google AI Studio API key.

## Configuration

Configuration is managed via `project.toml`.

```toml
[service]
log_dir = "./logs"

[llm]
api_key_variable = "GEMINI_API_KEY"
model = "gemini-2.5-flash"
temperature = 0.1

[nats]
url = "nats://localhost:4222"
```

## Running

1.  **Set API Key**:
    ```bash
    export GEMINI_API_KEY="your_key_here"
    ```

2.  **Start Service**:
    ```bash
    go run cmd/png-to-text-service/main.go
    ```

## Testing

Use the provided integration test script to verify the pipeline against a running NATS server:

```bash
./test_integration.sh
```

This script checks for processed files in the NATS Object Store and downloads the latest results to `./output/` for inspection.

## License

MIT
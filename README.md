# PNG-to-Text Service

## Project Summary

A NATS-based microservice that extracts and augments text from PNG images using Large Language Models (LLMs). It is designed to prepare text for audio narration by cleaning artifacts and expanding acronyms.

## Detailed Description

This service listens for `pngs.created` messages on a NATS stream. When a message is received, it downloads the PNG file from a NATS object store, sends it to a configured LLM (e.g., Gemini 2.5 Flash) for text extraction and augmentation, and uploads the resulting text (as a JSON array of strings) to another NATS object store. For each processed file, it publishes a `texts.processed` event.

Core capabilities include:

-   **LLM-Powered Extraction**: Uses advanced multimodal LLMs to extract text from images.
-   **Narration-Ready Output**: Automatically cleans text, expands acronyms, describes visual elements, and formats mathematical formulas for speech.
-   **JSON Output**: Returns a clean JSON array of text segments.
-   **NATS Integration**: Fully event-driven architecture using JetStream.
-   **Robust Error Handling**: Implements retry logic for LLM calls and NATS message handling.

## Technology Stack

-   **Programming Language:** Go
-   **Messaging:** NATS JetStream
-   **AI/LLM:** Google GenAI SDK (Gemini)
-   **Configuration:** TOML

## Architecture

```mermaid
flowchart TD
    subgraph "PNG-to-Text Service"
        A[NATS Consumer] --> B{Process Message};
        B --> C[Download PNG from Object Store];
        C --> D[Upload to LLM (File API)];
        D --> E[Generate Content (Text Extraction)];
        E --> F[Upload Text to Object Store];
        F --> G[Publish TextProcessedEvent];
    end

    subgraph "NATS JetStream"
        H[PNGS Stream] --> A;
        G --> I[TEXTS Stream];
        C --> J[PNG_FILES Object Store];
        F --> K[TEXT_FILES Object Store];
    end
```

## Configuration

The service is configured via a local `project.toml` file.

```toml
[service]
log_dir = "./logs"

[llm]
api_key_variable = "GEMINI_API_KEY"
base_url = "https://generativelanguage.googleapis.com"
model = "gemini-2.5-flash"
max_retries = 3
timeout_seconds = 120
temperature = 0.1

[llm.prompts]
system_instruction = "You are an expert narrator assistant..."
extraction_prompt = "Extract text, clean it, and expand acronyms..."

[nats]
url = "nats://192.168.122.102:4222"
dlq_subject = "png-to-text.dlq"

[nats.consumer]
stream = "PNGS"
subject = "pngs.created"
durable = "png-to-text-durable"

[nats.producer]
stream = "TEXTS"
subject = "texts.processed"

[nats.object_store]
png_bucket = "PNG_FILES"
text_bucket = "TEXT_FILES"
```

## Prerequisites

-   **NATS Server**: Running with JetStream enabled.
-   **Google Gemini API Key**: Set in the environment variable specified by `api_key_variable` (e.g., `GEMINI_API_KEY`).

## Usage

1.  **Set API Key**:
    ```bash
    export GEMINI_API_KEY="your-api-key"
    ```

2.  **Run Service**:
    ```bash
    go run cmd/png-to-text-service/main.go
    ```

## Testing

### Integration Test

An integration test script `test_integration.sh` is provided to verify the pipeline. It checks for existing processed files in the NATS Object Store and downloads them for inspection.

```bash
./test_integration.sh
```

## License

Distributed under the MIT License. See the `LICENSE` file for more information.

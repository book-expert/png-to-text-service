# PNG-to-Text Service

A lightweight, event-driven microservice that transforms PNG images into narration-ready text using Google's Gemini LLM.

## Overview

This service is a key component of a document-to-audio pipeline. It consumes `pngs.created` events from NATS, downloads the corresponding image, and uses the Gemini 2.5 Flash model to:

1.  **Extract** text with high accuracy.
2.  **Clean** artifacts (citations, headers, footers) based on user-defined exclusion rules and AI-generated text directives.
3.  **Expand** acronyms and abbreviations ("Fig." -> "Figure") for smooth speech.
4.  **Narrate Technical Content**: Converts code blocks and mathematical formulas (LaTeX) into spoken English descriptions (e.g., "x squared", "begin code block").
5.  **Format** the output as a cohesive "Director's Script".

The result is stored in a NATS Object Store, ready for a Text-to-Speech (TTS) service.

## Architecture

The service follows a stateless worker pattern:

1.  **Worker**: Listens to a NATS JetStream queue group (`pngs.created`).
2.  **LLM Processor**: Uploads the image to the Google GenAI File API and generates content using a strict system prompt defined in `project.toml`.
3.  **Publisher**: Stores the result (idempotently derived from the image name) and emits a `texts.processed` event.

## Prerequisites

*   **Go**: 1.25+
*   **NATS Server**: With JetStream enabled.
*   **Gemini API Key**: A valid Google AI Studio API key.

## Configuration

Configuration is managed via `project.toml`.

```toml
[service]
log_dir = "./logs"
workers = 5
system_instruction = """...""" # Contains strict formatting and narration rules

[llm]
api_key_variable = "GEMINI_API_KEY"
model = "gemini-2.5-flash"
temperature = 0.0 # Deterministic output

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

## Development

To build the service:
```bash
make build
```

To run linting:
```bash
make lint
```

## License

MIT
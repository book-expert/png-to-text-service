# PNG‑to‑Text Service

## Project Summary
Converts PNG images into cleaned, optionally AI‑augmented text and publishes results via NATS/JetStream.

## Detailed Description
This service consumes PNGCreatedEvent messages from NATS JetStream, downloads the referenced PNG from a JetStream Object Store, performs OCR using Tesseract, and optionally augments the text with Gemini (Google) based on per‑message or default configuration. The final text is uploaded to a text object bucket and a TextProcessedEvent is published for downstream services (e.g., TTS).

Core flow:
- Consume PNGCreatedEvent from a configured stream/consumer.
- Fetch PNG bytes from the configured object store bucket.
- OCR + clean text using Tesseract; optionally augment (commentary/summary) via Gemini.
- Store final text into the configured text object bucket.
- Publish TextProcessedEvent containing the text object key and TTS defaults.

## Technology Stack
- Programming Language: Go 1.25+
- Messaging & Storage: NATS JetStream (streams, durable consumers, object store)
- OCR: Tesseract (external binary)
- Optional AI: Gemini API
- Libraries: configurator, events, logger, prompt-builder, nats.go, testify

## Getting Started

### Prerequisites
- Go 1.25.1 (https://go.dev/dl/)
- Tesseract OCR
  - Ubuntu/Debian: `sudo apt-get update && sudo apt-get install -y tesseract-ocr`
  - macOS (Homebrew): `brew install tesseract`
- NATS Server (local dev)
  - Docker: `docker run -p 4222:4222 -p 8222:8222 nats:latest -js`
- golangci-lint (for lint): https://golangci-lint.run/usage/install/
- nats CLI (optional for testing): https://github.com/nats-io/natscli

### Installation
1) Clone and enter the repo
- `git clone https://github.com/book-expert/png-to-text-service`
- `cd png-to-text-service`
2) Modules and build
- `make install`
- `make build` (binary at `~/bin/png-to-text-service`)

### Configuration
Set `PROJECT_TOML` to a file path or HTTP URL serving your TOML config. Minimal example:

```toml
[png-to-text-service]
[png-to-text-service.nats]
url = "nats://127.0.0.1:4222"

[[png-to-text-service.object_stores]]
bucket_name = "png-files"

[[png-to-text-service.object_stores]]
bucket_name = "text-files"

[[png-to-text-service.streams]]
name = "pngs"
subjects = ["book-expert.pngs.created"]

[[png-to-text-service.streams]]
name = "texts"
subjects = ["book-expert.texts.created"]

[[png-to-text-service.consumers]]
stream_name = "pngs"
consumer_name = "png-to-text-consumer"
filter_subject = "book-expert.pngs.created"

[png_to_text_service.gemini]
api_key_variable = "GEMINI_API_KEY"
models = ["gemini-1.5-flash"]
max_retries = 3
retry_delay_seconds = 5
timeout_seconds = 60
temperature = 0.5
top_k = 40
top_p = 0.9
max_tokens = 2048

[png_to_text_service.augmentation]
use_prompt_builder = true

[png_to_text_service.augmentation.defaults.commentary]
enabled = true
custom_additions = ""

[png_to_text_service.augmentation.defaults.summary]
enabled = false
placement = "bottom"
custom_additions = ""

[png_to_text_service.tesseract]
language = "eng"
oem = 3
psm = 3
dpi = 300
timeout_seconds = 60

[png_to_text_service.tts_defaults]
voice = "default"
seed = 0
ngl = 4
top_p = 0.9
repetition_penalty = 1.05
temperature = 0.6
```

Also export your Gemini API key as named by `api_key_variable` (example:)
- `export GEMINI_API_KEY=your_secret_key`

## Usage
Run the service:
- `~/bin/png-to-text-service`

Publish a PNGCreatedEvent (example via nats CLI):
```
nats pub book-expert.pngs.created '{
  "header": {
    "tenant_id": "tenant-123",
    "workflow_id": "wf-abc"
  },
  "png_key": "tenant-123/wf-abc/page_0001.png",
  "page_number": 1,
  "total_pages": 10,
  "augmentation": {
    "commentary": {"enabled": true, "custom_instructions": ""},
    "summary": {"enabled": false, "placement": "bottom", "custom_instructions": ""}
  }
}'
```
The service will fetch the PNG, OCR + optionally augment, store the text, and publish a TextProcessedEvent on the configured subject (e.g., `book-expert.texts.created`).

## Testing
- Unit tests: `make test`
- Race detector: `make test-race`
- Lint (must be clean): `make lint`

## License
Distributed under the MIT License. See the LICENSE file for more information.

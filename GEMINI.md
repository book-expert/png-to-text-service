# GEMINI.md - PNG to Text Service

## Service Overview
This service performs **Optical Character Recognition (OCR)** on PNG images using the Gemini Vision API.

## Architecture & Data Flow
1.  **Input**: Listens to NATS JetStream subject `pngs.created`.
    -   Payload: `PNGCreatedEvent` (contains **JobSettings**).
2.  **Processing**:
    -   Downloads the PNG from the Object Store (`png_bucket`).
    -   **Dynamic Prompting**: Uses `prompt-builder` to construct a system prompt based on `JobSettings.StyleProfile` (e.g., "Academic", "Storyteller") and `Exclusions`.
    -   Sends the image to **Gemini Vision API**.
    -   Receives extracted text.
    -   Stores text in Object Store (`text_bucket`).
3.  **Output**: Publishes events to `texts.processed`.
    -   Payload: `TextProcessedEvent` (propagates **JobSettings** to TTS).

## Configuration
-   **Config File**: `project.toml`
-   **Key Settings**:
    -   `workers`: **4** (Parallel processing).
    -   `llm.model`: Model name.

## Current Status (Dec 12, 2025)
-   **Health**: ✅ Healthy
-   **New Features**:
    -   **Style Steering**: Instructs the LLM to format text/tags based on the desired audio style.
    -   **Settings Propagation**: Passes Voice/Language settings to the TTS service.
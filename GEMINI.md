# GEMINI.md - PNG to Text Service

## Service Overview
This service performs **Optical Character Recognition (OCR)** and **Script Generation** using the Gemini Vision API. It acts as the "Director," converting raw visual pages into structured "Director's Scripts" for audio generation.

## Architecture & Data Flow
1.  **Input**: Listens to NATS JetStream subject `pngs.created`.
    -   Payload: `PNGCreatedEvent` (contains **JobSettings**).
2.  **Processing**:
    -   Downloads the PNG from the Object Store (`PNG_FILES`).
    -   **Prompt Engineering**: Uses `promptbuilder` to construct a system instruction based on `JobSettings.StyleProfile` (e.g., "Academic", "Storyteller") and the "Show Bible" concept.
    -   **Gemini Vision API**:
        -   Extracts text.
        -   Expands acronyms and cleans data.
        -   **Dynamic Scene Generation**: Writes a custom "Director's Note" and "Scene Description" for *each page* based on its content, while adhering to the high-level Persona.
    -   Stores the resulting **Director's Script** (with Markdown headers like `# AUDIO PROFILE`) in the Object Store (`TEXT_FILES`).
3.  **Output**: Publishes events to `texts.processed`.
    -   Payload: `TextProcessedEvent`.

## Configuration
-   **Config File**: `project.toml`
-   **Key Settings**:
    -   `workers`: Parallel processing (Default: 4).
    -   `llm.model`: Model name (e.g., `gemini-2.0-flash-exp`).

## Current Status (Dec 12, 2025)
-   **Health**: ✅ Healthy
-   **Key Features**:
    -   **Guided Dynamic Generation**: Combines strict high-level personas (for consistency) with dynamic per-page scene descriptions (for engagement).
    -   **Visual Narration**: Automatically describes charts/graphs in character (e.g., "This figure shows...").

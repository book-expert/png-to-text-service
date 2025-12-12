# GEMINI.md - PNG to Text Service

## Service Overview
This service performs **Optical Character Recognition (OCR)** on PNG images using the Gemini Vision API.

## Architecture & Data Flow
1.  **Input**: Listens to NATS JetStream subject `pngs.created`.
    -   Payload: `PNGCreatedEvent`.
2.  **Processing**:
    -   Downloads the PNG from the Object Store (`png_bucket`).
    -   Sends the image to **Gemini Vision API**.
    -   Receives extracted text.
    -   Stores text in Object Store (`text_bucket`).
3.  **Output**: Publishes events to `texts.processed`.

## Configuration
-   **Config File**: `project.toml`
-   **Key Settings**:
    -   `workers`: Number of concurrent consumers (Updated to **4** for parallelism).
    -   `llm.model`: Model name (e.g., `gemini-2.5-flash`).

## Current Status (Dec 12, 2025)
-   **Health**: ✅ Healthy
-   **Improvements**:
    -   **Parallelism**: Increased workers to 4 (was 1) to match downstream throughput requirements.
-   **Operational Notes**: Consumes Gemini API quota.

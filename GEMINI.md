# GEMINI.md - PNG to Text Service

## Service Overview
This service is the **Worker** of the pipeline. It uses Gemini Vision to extract text from page images while applying strict exclusion rules and prepending the "Master Directive".

## Architecture & Data Flow
1.  **Input**: Listens to NATS JetStream subject `pngs.created`.
    -   Payload: `PNGCreatedEvent` (contains `AudioSessionConfig` with `MasterDirective` + `JobSettings.Exclusions`).
2.  **Processing**:
    -   **Vision Prompt Construction**: Combines the static `SystemInstruction` (from `project.toml`) with dynamic `Exclusions` (from `JobSettings`) to instruct the model on what to skip (e.g., "Skip References").
    -   **Extraction**: Calls Gemini Vision 2.5 Flash to extract text.
    -   **Script Assembly**: Concatenates `MasterDirective` (Header) + `ExtractedText` (Body).
3.  **Output**: Publishes `TextProcessedEvent`.
    -   Payload: A "Director's Script" ready for TTS.

## Configuration
-   **Config File**: `project.toml`
-   **Key Settings**:
    -   `system_instruction`: The base persona for the Vision model (OCR).
    -   `extraction_prompt`: The task instruction.

## Current Status (Dec 13, 2025)
-   **Health**: ✅ Healthy
-   **Features**:
    -   **Exclusion Logic**: Dynamically injects user-defined exclusions into the Vision prompt.
    -   **Master Directive**: Propagates the "Director's Vibe" to the final script.
    -   **Clean Extraction**: Handles tables, code blocks, and artifacts via prompt engineering.

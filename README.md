# PNG-to-Text Service

The **PNG-to-Text Service** is a sophisticated Go-based microservice responsible for document digitization and narration pre-processing. It converts image-based document pages (PNGs) into clean, narrated-ready text using advanced LLM-powered OCR.

## Overview

Unlike traditional OCR engines that merely extract text, this service uses Google Gemini to perform "Intelligent Extraction." It understands document structure (columns, tables, code blocks) and applies a rigorous set of **Narration Rules** to ensure the extracted text is optimized for high-quality speech synthesis.

## Key Features

- **LLM-Powered OCR (Gemini Integration)**: Uses `gemini-2.5-flash` for high-fidelity text extraction that surpasses traditional Tesseract-style OCR.
- **Narration Pre-Processing**:
    - **Expansion**: Automatically expands abbreviations and acronyms into their spoken forms (e.g., "Fig." -> "Figure", "e.g." -> "for example").
    - **STEM Normalization**: Converts mathematical formulas and LaTeX into spoken English (e.g., $x^2$ -> "x squared").
    - **Code Block Handling**: Intelligently narrates the structure and intent of code blocks rather than raw syntax.
    - **Correction**: Joins hyphenated words split across lines and fixes OCR-induced errors.
- **Structure Awareness**: Corrects for multi-column layouts and ignores non-text artifacts like page numbers, headers, and footers.
- **Workflow Orchestration**: Consumes `pngs.created` events and produces `texts.processed` events once extraction is complete.
- **Concurrent Workers**: Scalable worker pool for processing multiple document pages in parallel, powered by the `common-worker` library.

## Requirements

- Go 1.25.5+
- NATS Server with JetStream enabled
- **Gemini API Key**: Required for the intelligent extraction engine.

## Configuration

The service is configured via `project.toml`. Key areas include:

- `[service]`: Worker count and extraction prompts.
- `[llm]`: Model settings, temperature, and detailed **System Instructions** for narration rules.
- `[nats]`: Stream, subject, and object store bucket configurations for PNG inputs and Text outputs.

## Getting Started

### Installation

```bash
make install
```

### Building

```bash
make build
```

### Running

```bash
make run
```

## Internal Architecture

- `cmd/png-to-text-service`: Application entry point and lifecycle management.
- `internal/llm`: Gemini client implementation and prompt orchestration.
- `internal/worker`: NATS JetStream consumer logic and workflow state management.
- `internal/config`: Configuration loading and validation.

## Events

### Consumes
- `pngs.created`: Triggered when a new PNG page is ready for processing.

### Produces
- `texts.started`: Triggered when extraction begins (for UI status updates).
- `texts.processed`: Triggered when the extracted text has been uploaded to the object store.

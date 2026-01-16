# PNG-to-Text Service

The **PNG-to-Text Service** is a sophisticated Go-based microservice responsible for document digitization and narration pre-processing. It converts image-based document pages (PNGs) into clean, narrated-ready text using advanced LLM-powered OCR.

## Overview

Unlike traditional OCR engines that merely extract text, this service uses Google Gemini to perform "Intelligent Extraction." It understands document structure (columns, tables, code blocks) and applies a rigorous set of **Narration Rules** to ensure the extracted text is optimized for high-quality speech synthesis.

## Key Features

- **LLM-Powered OCR (Gemini Integration)**: Uses `gemini-2.5-flash` for high-fidelity text extraction.
- **Narration Pre-Processing**:
    - **Expansion**: Automatically expands abbreviations into their spoken forms (e.g., "Fig." -> "Figure").
    - **STEM Normalization**: Converts formulas and LaTeX into spoken English (e.g., $x^2$ -> "x squared").
    - **Code Block Handling**: Intelligently narrates the structure and intent of code blocks.
    - **Correction**: Fixes hyphenation and OCR-induced errors.
- **Structure Awareness**: Corrects for multi-column layouts and ignores non-text artifacts (headers/footers).
- **Workflow Orchestration**: Consumes `pngs.created` events and produces `texts.processed` events.
- **Concurrent Workers**: Powered by the `common-worker` library for scalable, parallel processing.

## 🛡️ Alignment with Project Standards

This service adheres to the **Manifesto of Truth** and project engineering standards:
- **Whole Words Only**: Naming conventions avoid abbreviations (e.g., `extraction`, `processing`, `context`).
- **Care**: Intelligent pre-processing ensures that synthesized speech sounds natural and professional.
- **Truth**: The service validates that extracted text matches the narration directives provided by the PDF service.

## Requirements

- Go 1.25.5+
- NATS Server with JetStream enabled
- **Gemini API Key**: Required for the intelligent extraction engine.

## Configuration

The service is configured via `project.toml`. Key areas include:

- `[service]`: Worker count and extraction prompts.
- `[llm]`: Model settings and detailed **System Instructions** for narration rules.
- `[nats]`: Stream, subject, and object store bucket configurations.

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

## Events

### Consumes
- `pngs.created`: Triggered when a new PNG page is ready for processing.

### Produces
- `texts.started`: Triggered when extraction begins.
- `texts.processed`: Triggered when the extracted text is ready.

---
*Built with ❤️, Craftsmanship, and Discipline.*

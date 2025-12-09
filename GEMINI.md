# Gemini Integration & Refactoring: Post-Mortem and Learnings

## Overview

This document captures the insights, challenges, and successful strategies discovered during the refactoring of the `png-to-text-service`. The goal was to simplify the architecture by replacing Tesseract OCR with Google's Gemini Multimodal LLM, while strictly adhering to "Simplicity is Non-Negotiable" and event-driven design principles.

## Key Takeaways

1.  **Simplicity Wins**: Removing the complex `internal/pipeline` and `internal/ocr` packages in favor of a direct `Worker -> LLM` pattern reduced code size significantly and improved readability.
2.  **LLM as a Service**: Treating the LLM (Gemini) as a direct processing unit rather than just an augmentation step simplified the workflow. It handles OCR, cleaning, and augmentation in a single pass.
3.  **Standard SDKs vs. Custom HTTP**: Switching to the official `google.golang.org/genai` SDK removed boilerplate code for multipart uploads and request serialization, reducing potential bugs (like the `float32` casting issue).
4.  **Strict Prompts**: To get reliable JSON output for downstream narration systems, the system prompt must be explicit about *what not to include* (citations, headers) and *required transformations* (acronym expansion).

## Post-Mortem

### Challenges
*   **Legacy Code Inertia**: Initial refactoring attempts tried to keep the `pipeline` abstraction, which proved redundant. Recognizing when to delete entire architectural layers is crucial.
*   **Configuration Drift**: `project.toml` structure needed alignment with the new "LLM-generic" approach. Renaming `[gemini]` to `[llm]` required careful cascading changes in `config.go` and `main.go`.
*   **Prompt Engineering**: Initial prompts left artifacts (e.g., `arXiv` IDs). Iterative testing with real data showed the need for negative constraints ("STRICTLY REMOVE").

### Learnings
*   **Gemini File API**: For image-to-text, the File API (Upload -> Generate) is robust. The file must be deleted after processing to avoid cluttering the project storage quota.
*   **JSON Enforcement**: Setting `responseMimeType: "application/json"` in the generation config is the most reliable way to guarantee structured output from Gemini Flash models.
*   **NATS Testing**: Integration testing in an event-driven system requires accounting for race conditions. A test script should verify *outcomes* (files in bucket) rather than just triggering inputs.

## How This Helps Future Development

1.  **Blueprint for Workers**: The simplified `Worker -> Processor -> NATS` pattern established here serves as the template for future microservices (e.g., `tts-service`).
2.  **Golden Rules Validation**: This refactor proved that "Smaller is faster" and "No hard-coded values" lead to more maintainable systems.
3.  **LLM Integration Standard**: We now have a proven pattern for integrating GenAI SDKs with explicit error handling and context management.

## Actionable Next Steps
*   Apply this simplified worker pattern to `pdf-to-png-service` if not already done.
*   Use the "System Instruction" pattern for all future LLM tasks to enforce output formats strictness.

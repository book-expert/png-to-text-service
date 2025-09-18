# Changelog

This log tracks all changes made to the `png-to-text-service` during our interactive session.

---

**File:** `project.toml` (in `png-to-text-service` root)
**Time:** 2025-09-16
**Change:** Deleted the local `project.toml` file.
**Reason:** Per user instruction, configuration is never local; it is always fetched from a URL. This file was incorrect.

---

**File:** `internal/pipeline/pipeline.go`
**Time:** 2025-09-16
**Change:**
- Replaced the undefined `ocr.TesseractOCR` struct with `ocr.Processor`.
- Updated the OCR processing call to the correct `p.ocr.ProcessPNG`.
- Removed a redundant and broken call to an undefined `cleanText` function.
- Implemented the missing `generateOutputFileName` helper function.
- Added error handling for a `tmpFile.Close()` call.
- Refactored to use interfaces for dependencies, improving testability.
- Modified `Process` to return the processed text and an error, removing file I/O.
**Reason:** To fix initial compilation errors, an `errcheck` linting error, and to decouple the pipeline from file system writes, aligning with a NATS-based architecture.

---

**File:** `internal/config/config.go`
**Time:** 2025-09-16
**Change:**
- Refactored the main `Config` struct to be composed of nested structs (`NATSConfig`, `PathsConfig`, `PNGToTextServiceConfig`) to correctly parse the global `project.toml`.
- Removed obsolete fields and validation logic related to local file paths.
- Updated helper methods (`GetAPIKey`, `EnsureDirectories`, etc.) to use the new nested structure.
- Modified the `Load` function to accept a logger and a test-specific path.
- Added `DeadLetterSubject` to `NATSConfig`.
**Reason:** To align the service's configuration parsing with the centralized, URL-fetched `project.toml`, fix compilation errors, and support dead-letter queue functionality.

---

**File:** `internal/config/config_test.go`
**Time:** 2025-09-16
**Change:**
- Rewrote the test suite to work with the new nested `Config` struct and URL-based loading.
- Added a `newTestLogger` helper to provide the required logger dependency to `config.Load`.
- Removed obsolete tests that were no longer valid after the configuration refactoring.
**Reason:** To fix compilation errors in the test file after the main `config.go` was refactored.

---

**File:** `cmd/png-to-text-service/main.go`
**Time:** 2025-09-16
**Change:**
- Removed all command-line flag parsing and local file processing logic.
- Changed the application to initialize all components (`ocr`, `gemini`, `pipeline`, `worker`) using the new centrally-loaded configuration struct.
- Corrected the `logger.New` initialization call and fixed typos from previous refactoring steps.
- Updated `worker.New` to pass the dead-letter queue subject.
**Reason:** To refactor the application into a pure, NATS-only service worker and fix compilation errors.

---

**File:** `internal/augment/gemini.go`
**Time:** 2025-09-16
**Change:** Added error handling for the `resp.Body.Close()` deferred call.
**Reason:** To fix an `errcheck` linting error and prevent potential resource leaks.

---


**File:** `internal/worker/worker.go`
**Time:** 2025-09-16
**Change:**
- Added error handling for all `msg.Ack()` calls.
- Updated to publish processed text to a NATS subject instead of writing to a file.
- Implemented dead-letter queue logic to forward failed messages.
**Reason:** To fix `errcheck` linting errors, make message acknowledgment more robust, and align with a pure NATS-based architecture.

---

**File:** `README.md`
**Time:** 2025-09-16
**Change:** Rewrote the `README.md` to conform to documentation standards and reflect the service's NATS-only architecture.
**Reason:** To provide accurate, up-to-date documentation for the refactored service.

---

**File:** `docs/PROMPT_ENGINEERING.md`
**Time:** 2025-09-16
**Change:** Created a new document outlining prompt engineering strategies.
**Reason:** To provide guidance on creating effective prompts for the Gemini model.

---

**File:** `.gemini/GEMINI.md`
**Time:** 2025-09-16
**Change:** Added a new section on Prompt Engineering Principles.
**Reason:** To integrate prompt engineering best practices into the core design principles.

---

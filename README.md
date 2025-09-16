# PNG-to-Text Service

A service that performs OCR on PNG images to extract text and optionally augments it with AI-generated commentary.

## Detailed Description

This service provides a robust pipeline for converting PNG images into structured text. It is designed to handle large volumes of images concurrently, making it suitable for batch processing tasks.

The core workflow consists of three main stages:
1.  **Extract**: Raw text is extracted from PNG images using the Tesseract OCR engine. The text is then cleaned to remove common OCR artifacts.
2.  **Augment (Optional)**: If enabled, the extracted text and the source image are sent to a multimodal AI model (e.g., Google Gemini) to generate additional context, such as a summary or commentary.
3.  **Output**: The final text, either the cleaned OCR output or the AI-augmented version, is saved to a file.

The service is configured via a `project.toml` file and is designed to be run as a worker processing jobs from a NATS message queue.

## Technology Stack

*   **Language:** Go (1.25+)
*   **OCR Engine:** Tesseract
*   **AI Integration:** Google Gemini (pluggable for other providers)
*   **Messaging:** NATS

## Getting Started

### Prerequisites

- **Go**: Version 1.25 or later must be installed.
  ```bash
  # Example for Ubuntu/Debian
  sudo apt-get update && sudo apt-get install golang
  ```
- **Tesseract OCR**: The Tesseract binary must be in your system's `PATH`.
  ```bash
  # Example for Ubuntu/Debian
  sudo apt-get update && sudo apt-get install tesseract-ocr
  ```
- **AI Provider API Key**: If using AI augmentation, an API key from your provider is required. This should be set as an environment variable.
  ```bash
  export GEMINI_API_KEY="your-api-key-here"
  ```

### Installation

1.  Clone the repository:
    ```bash
    git clone <repository-url>
    ```
2.  Navigate to the project directory:
    ```bash
    cd png-to-text-service
    ```
3.  Build the application:
    ```bash
    make build
    ```
    This will create the `png-to-text-service` binary in the `bin/` directory.

## Usage

The service is run as a NATS worker. It connects to a NATS server, subscribes to a subject, and processes messages as they arrive.

```bash
# Run the service as a NATS worker (requires NATS server and environment variables set)
./bin/png-to-text-service
```

## Testing

To run the complete suite of automated tests, execute the following command:

```bash
make test
```
This will run all unit and integration tests and display the results.

## License

Distributed under the MIT License. See the `LICENSE` file for more information.

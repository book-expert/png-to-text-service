# png-to-text-service NATS Configuration

This document outlines the NATS configuration for the `png-to-text-service`. This service utilizes NATS JetStream for asynchronous message processing.

## Connection

- **NATS URL:** The service connects to a NATS server using the URL specified in the `nats.url` field of the configuration file.

## JetStream Usage

The service uses NATS JetStream for reliable, persistent messaging.

### Stream and Consumer

- **PNG Stream:** The service consumes messages from the `PNG_PROCESSING` stream.
- **PNG Consumer:** It uses a durable consumer named `png-text-workers` to pull messages from the `PNG_PROCESSING` stream.
- **Text Stream:** The service may also interact with the `TTS_JOBS` stream, particularly for publishing processed text.

### Subjects

The service interacts with the following subjects:

- **Inbound (Consumption):**
  - **Subject:** `png.created`
  - **Purpose:** The service listens on this subject for new jobs. Each message is expected to contain the raw PNG image data to be processed.

- **Outbound (Publication):**
  - **Subject:** `text.processed`
  - **Purpose:** After successfully processing a PNG image, the service publishes the extracted text to this subject.

- **Dead-Letter Subject:**
  - **Subject:** `dead.letter` (default, configurable via `nats.dead_letter_subject`)
  - **Purpose:** Messages that fail processing are published to this subject for further inspection or reprocessing.

## Message Payloads

- **Incoming (`png.created`):** The message payload should be the raw binary data of the PNG image.
- **Outgoing (`text.processed`):** The message payload will be the UTF-8 encoded string of the text extracted from the image.
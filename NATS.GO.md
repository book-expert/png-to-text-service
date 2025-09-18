# png-to-text-service NATS Configuration

This document outlines the NATS configuration for the `png-to-text-service`. This service utilizes NATS JetStream for asynchronous message processing.

## Connection

- **NATS URL:** The service connects to a NATS server using the URL specified in the `nats.url` field of the configuration file.

## JetStream Usage

The service uses NATS JetStream for reliable, persistent messaging.

### Stream and Consumer

- **Stream:** The service consumes messages from the `PNG_PROCESSING` stream.
- **Consumer:** It uses a durable consumer named `png-text-workers` to pull messages from the stream.

### Subjects

The service interacts with the following subjects:

- **Inbound (Consumption):**
  - **Subject:** `png.created`
  - **Purpose:** The service listens on this subject for new jobs. Each message is expected to contain the raw PNG image data to be processed.

- **Outbound (Publication):**
  - **Subject:** `text.processed`
  - **Purpose:** After successfully processing a PNG image, the service publishes the extracted text to this subject.

- **Dead-Letter Subject:**
  - **Subject:** The service is configured to use a dead-letter subject for messages that fail processing. The specific subject name is defined in the `nats.dead_letter_subject` field of the configuration file.

## Message Payloads

- **Incoming (`png.created`):** The message payload should be the raw binary data of the PNG image.
- **Outgoing (`text.processed`):** The message payload will be the UTF-8 encoded string of the text extracted from the image.

## Deprecated Features

It is important to be aware of deprecated features in the NATS ecosystem to ensure future compatibility.

### NATS Streaming (STAN)

NATS Streaming (STAN) is deprecated and has been replaced by **NATS JetStream**. This project uses JetStream and does not rely on STAN.

### JSON Tags in `server.Options`

The JSON tags within the `server.Options` struct of the `nats-server` are deprecated, particularly for monitoring endpoints. Configuration and interaction with the server should be done through other means where possible.

### "durable" Term for Consumers

There is an ongoing proposal to deprecate the term "durable" for consumers in favor of more explicit configuration options. While still in use, it is advisable to monitor the NATS documentation for future changes in this area.

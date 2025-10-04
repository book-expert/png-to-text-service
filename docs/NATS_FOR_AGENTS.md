# NATS/JetStream Blueprint for Agents

This document is the strict, prescriptive blueprint for implementing services that use NATS and JetStream in this project. It is mandatory and takes precedence over examples elsewhere. All services must conform to this blueprint to ensure reliability, consistency, and operability.

## 1. Purpose and Scope
- Define a single, unambiguous pattern for each service role (Worker, Publisher, Aggregator).
- Mandate robust dead‑letter handling, durable consumption, strict configuration validation, descriptive naming, context propagation, and automated quality gates.

## 2. Roles and Responsibilities
- Worker (durable, at‑least‑once): Consumes from a JetStream durable consumer; processes work; publishes an output event; on failure, publishes to a dead‑letter subject and acknowledges the original message only after the DLQ publish succeeds.
- Publisher: Emits events to a configured stream/subject; does not consume.
- Aggregator: Consumes events, tracks workflow progress via KV/ObjectStore, writes final artifacts; follows the Worker pattern for consumption and DLQ.

## 3. Configuration Contract (Configurator)
- Use `configurator.ServiceNATSConfig` exclusively for NATS setup. No ad‑hoc or local TOML beyond what configurator manages.
- Required fields per Worker service:
  - `NATS.URL`: Non‑empty.
  - `Streams`: Must include at least one input stream (the one the consumer reads from) and one output stream (where the service publishes its result events). Each stream must have ≥1 subject.
  - `Consumers`: At least one durable consumer for the input stream with:
    - `AckExplicitPolicy`
    - `DeliverPolicy` appropriate for the workload (usually `All`)
    - `MaxDeliver` > 0 and a `BackOff` policy configured
    - Queue group name when horizontally scaling
  - `ObjectStores`: List all required buckets. Workers that read/write large objects must declare:
    - Source bucket (read)
    - Destination bucket (write)
  - `Dead‑Letter Subject`: Mandatory. Each Worker MUST have a configured DLQ subject (e.g., `book-expert.png-to-text.dlq`). The DLQ subject MUST belong to a dedicated stream (e.g., `dlq`) with sufficient retention.
  - `KeyValue` (if applicable): Aggregators should define a KV bucket for workflow state.

Validation rules (startup MUST fail fast with explicit errors):
- Consumers: ≥1; input consumer must point to an existing stream and have a non‑empty `filter_subject`.
- Streams: Output stream must expose ≥1 subject for publishing.
- ObjectStores: Required buckets present by name; no reliance on brittle indexing.
- DLQ subject: Non‑empty for Worker/Aggregator roles.

## 4. Canonical Lifecycle — Worker
1) Initialize NATS/JetStream
- `configurator.SetupNATSComponents(cfg.ServiceNATS.NATS)`
- `CreateOrUpdateStreams`, `CreateOrUpdateConsumers`, `CreateOrUpdateObjectStores`

2) Validate Configuration
- Verify presence and integrity of: input consumer, output subject, required object stores, DLQ subject.
- Abort startup on any violation with descriptive error messages that include tenant/service identifiers where available.

3) Run Loop (Durable Consumption)
- Use a single `requestContext` that carries deadlines/cancellation across all calls.
- Acquire consumer once at startup; fetch messages via `consumer.Fetch(…, FetchMaxWait=bounded)`; never block indefinitely.
- Manual acks only (`AckExplicit`).

4) Process Message (Happy Path)
- Read input object via ObjectStore with the same `requestContext`.
- Execute the pipeline (OCR/augment/etc.).
- Write results to the destination ObjectStore.
- Publish output event to the configured output subject.
- Ack the original message after successful publish.

5) Failure Handling (Dead‑Letter is Mandatory)
- On processing failure:
  - Attempt to publish the original message payload (or a structured failure envelope with context) to the DLQ subject.
  - Use bounded retries with backoff for the DLQ publish (retry count and delay configured in service config). Log each attempt with context: tenant, workflow, object key, error category.
  - If DLQ publish succeeds: Ack the original message.
  - If DLQ publish exhausts retries: Do NOT Ack. Prefer `Nak` with a delay to respect consumer `BackOff` and permit another attempt to get the message into the DLQ. Never lose messages.

6) Shutdown
- Honor OS signals. Stop fetching; allow in‑flight processing to complete (bounded by a grace period); close NATS connection explicitly.

## 5. Canonical Lifecycle — Publisher
- Initialize NATS/JetStream components.
- Validate that the output stream exists and has ≥1 subject.
- Publish events with a `requestContext` that carries deadlines.
- No consumption, no DLQ.

## 6. Canonical Lifecycle — Aggregator
- Same as Worker for consumption and DLQ policy.
- Maintain progress in a KV bucket keyed by workflow (e.g., `tenant/workflow`).
- When all required parts arrive, gather objects from ObjectStore, produce the final artifact, store it, publish the final event, then Ack.

## 7. Context Propagation
- Use a descriptive variable name: `requestContext` (NOT `ctx`).
- Pass `requestContext` as the first parameter to any function that performs I/O, can block, or is long‑running, including:
  - NATS connect, stream/consumer/object store create or lookup
  - Consumer fetch and message ack/nak
  - ObjectStore get/put
  - Event publish

## 8. Error Handling and Naming
- Unique, descriptive error variable per call (no reuse of a generic `err`).
- Always wrap with context using `fmt.Errorf("…: %w", originalError)`.
- Include identifiers in messages (tenant ID, workflow ID, object key, consumer, subject) to support incident triage.

## 9. Quality Gates (Non‑Negotiable)
- Linting: `golangci-lint` clean, no suppressions (`//nolint` forbidden).
- Formatting: `gofmt`/`gofumpt` clean.
- Testing: ≥80% coverage for Worker/Aggregator packages; table‑driven tests covering:
  - Happy path: fetch → process → store → publish → ack.
  - ObjectStore read failures (retries policy at the pipeline boundary where applicable).
  - Publish failures (including DLQ publish retry/exhaustion behavior).
  - Shutdown behavior while idle and while processing.

## 10. Conformance Checklist (Reviewers Use This)
- Config validated at startup: consumers, streams/subjects, object stores, DLQ.
- Dead‑letter subject present and used on all processing failures.
- Ack only after success (or after DLQ publish success on failure).
- Nak with delay when DLQ publish cannot be completed (no ack on failure without DLQ).
- `requestContext` propagated through all I/O APIs; no `context.Background()` in runtime paths.
- Descriptive error variable names and wrapped errors with context.
- Structured logs include tenant, workflow, object key.
- Lint/test gates pass with zero issues; coverage ≥80% for critical packages.

## 11. Reference Subjects and Streams (Naming Pattern)
- Input event subjects: `book-expert.<domain>.created`
- Output event subjects: `book-expert.<domain>.created`
- Dead‑letter subjects: `book-expert.<service>.dlq`
- DLQ stream: `dlq` (retention sized for peak failure volume)

Adopt exact names per service domain; the pattern is mandatory to keep topology predictable.


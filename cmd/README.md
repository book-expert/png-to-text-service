# Command Line Applications

Entry points for executable binaries.

## Contents

- `png-to-text-service/`: Main OCR pipeline application

## Building

```bash
go build -o bin/png-to-text-service ./cmd/png-to-text-service
# or: make build
```

## Usage

```bash
./bin/png-to-text-service -help
```

Applications load configuration from `project.toml`, with command-line flags overriding config values.
# Command Line Applications

This directory contains the entry points for all executable applications in the PNG-to-Text Service.

## What It Contains

- **`png-to-text-service/`**: Main service application that orchestrates the complete OCR and text enhancement pipeline

## Why This Structure

Following Go conventions, command-line applications are placed in `cmd/` to clearly separate executable entry points from reusable library code. This structure:

- **Separates Concerns**: Keeps application logic separate from CLI interface logic
- **Enables Testing**: Makes the core business logic testable independent of command-line parsing
- **Supports Multiple Binaries**: Allows for future expansion with additional command-line tools
- **Follows Standards**: Adheres to standard Go project layout practices

## How It Works

Each subdirectory represents a separate binary application:

1. **Configuration Loading**: Applications load configuration from TOML files and environment variables
2. **Argument Parsing**: Command-line flags override configuration file settings
3. **Pipeline Setup**: Core processing components are initialized with validated configuration
4. **Execution Coordination**: Applications orchestrate the flow between internal packages
5. **Error Handling**: Top-level error handling and user-facing error messages

## How To Use

### Building Applications

```bash
# Build the main service
go build -o bin/png-to-text-service ./cmd/png-to-text-service

# Or use the Makefile
make build
```

### Running Applications

Each application provides help information:

```bash
./bin/png-to-text-service -help
```

## Architecture Principles

Command applications in this directory follow these design principles:

- **Thin Layer**: Minimal logic, primarily configuration and coordination
- **Clear Interfaces**: Well-defined contracts with internal packages
- **Error Transparency**: Meaningful error messages for end users
- **Configuration First**: Behavior controlled through configuration, not hardcoded values
- **Testable Components**: Core logic remains in internal packages for easy unit testing

This structure ensures that the command-line interface remains simple and focused while keeping the complex business logic properly encapsulated and testable.
# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`ghloner` is a Go command-line tool that clones all repositories from a GitHub organization. It handles concurrent cloning, automatic retries, and graceful error recovery including non-fast-forward Git errors.

## Key Commands

### Development Commands

```bash
# Build the application
go build -o ghloner ./cmd/ghloner

# Run tests (when implemented)
go test ./...

# Run linter
golangci-lint run ./...

# Update dependencies
go mod tidy

# Docker build
docker build -t ghloner .
```

### Running the Application

```bash
# Using environment variables
export GITHUB_ORG=myorg
export GITHUB_TOKEN=ghp_xxxxxxxxxxxx
export OUTPUT_DIR=./repos
./ghloner

# Using command-line flags
./ghloner -org myorg -token ghp_xxxxxxxxxxxx -output ./repos -workers 10 -retry 5
```

## Architecture Overview

The codebase follows a clean architecture pattern with clear separation of concerns:

### Entry Point
- `cmd/ghloner/main.go`: Parses configuration, sets up logging, initializes components, and handles graceful shutdown

### Core Components

1. **Configuration (`internal/config/`)**: Handles environment variables and command-line flags with precedence rules

2. **Repository Processing (`internal/repository/`)**: 
   - `processor.go`: Orchestrates the entire cloning process
   - Uses worker pool pattern for concurrent operations
   - Implements graceful shutdown handling

3. **GitHub Integration (`internal/repository/github/`)**: 
   - `client.go`: Manages GitHub API authentication
   - `repository_lister.go`: Lists all repositories in an organization

4. **Git Operations (`internal/repository/git/`)**: 
   - `manager.go`: High-level repository management (clone/update logic)
   - `operations.go`: Low-level Git operations with retry logic
   - Handles non-fast-forward errors by re-cloning repositories

5. **Concurrency (`internal/repository/concurrency/`)**: 
   - `worker_pool.go`: Generic worker pool implementation for parallel processing

6. **Storage (`internal/repository/storage/`)**: 
   - `file_manager.go`: Manages file system operations and directory creation

### Key Design Patterns

- **Worker Pool**: Configurable concurrent processing with graceful shutdown
- **Retry Logic**: Built into Git operations for handling transient failures
- **Error Recovery**: Special handling for non-fast-forward Git errors (delete and re-clone)
- **Interface-based Design**: Clean contracts between components
- **Context Propagation**: Proper cancellation support throughout

### Error Handling Strategy

The application implements a robust error handling approach:
- Transient errors are retried (configurable retry count)
- Non-fast-forward Git errors trigger a delete and re-clone operation
- All errors are logged with context for debugging
- Graceful shutdown on interrupt signals

### Logging

Uses structured logging with `slog`:
- Info level for normal operations
- Warn level for recoverable issues
- Error level for failures
- Includes timing information for performance monitoring
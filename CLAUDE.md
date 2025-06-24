# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`ghloner` is a Go command-line tool that clones all repositories from a GitHub organization. It handles concurrent cloning, automatic retries, and graceful error recovery including non-fast-forward Git errors.

## Key Commands

### Development Commands

```bash
# Build the application
go build -o ghloner ./cmd/ghloner

# Run all tests
go test ./...

# Run tests with coverage
go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# Run specific package tests
go test -v ./internal/config
go test -v ./internal/repository/git

# Run tests with timeout (useful for integration tests)
go test -v -timeout 30s ./...

# Run short tests only (skip long-running tests)
go test -v -short ./...

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

## Testing Strategy

The project implements comprehensive testing with the following structure:

### Test Organization

```
ghloner/
├── internal/
│   ├── config/config_test.go            # Configuration parsing tests
│   ├── repository/
│   │   ├── git/
│   │   │   ├── manager_test.go         # Git manager tests
│   │   │   └── operations_test.go      # Git operations tests
│   │   ├── github/
│   │   │   └── repository_lister_test.go # GitHub API tests
│   │   ├── concurrency/
│   │   │   └── worker_pool_test.go      # Concurrent processing tests
│   │   └── storage/
│   │       └── file_manager_test.go      # File system operations tests
└── test/
    ├── fixtures/                         # Test data generators
    │   └── github_responses.go           # Mock GitHub API responses
    └── helpers/                          # Test utilities
        ├── git_helpers.go                # Git repository test helpers
        └── mock_helpers.go               # Mock implementations
```

### Test Coverage by Component

- **Config Package** (95.5%): Tests configuration parsing, validation, and environment variable precedence
- **Worker Pool** (100%): Tests concurrent processing, context cancellation, and graceful shutdown
- **Storage/File Manager** (92.3%): Tests file operations, cleanup logic, and permission handling
- **Git Operations** (31.8%): Tests retry logic, error detection, and non-fast-forward handling
- **GitHub Repository Lister** (17.6%): Tests API pagination and error handling

### Testing Best Practices

1. **Table-Driven Tests**: Used extensively for testing multiple scenarios
2. **Mock Interfaces**: Created for external dependencies (Git, GitHub API)
3. **Test Helpers**: Utilities for creating test repositories and fixtures
4. **Context Testing**: Proper testing of context cancellation in concurrent operations
5. **Error Scenarios**: Comprehensive testing of error paths and edge cases

### Running Tests

```bash
# Run all tests with race detection
go test -race ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run specific test
go test -v -run TestWorkerPool ./internal/repository/concurrency

# Skip long-running tests
go test -short ./...
```
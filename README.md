# ghloner

A tool to clone all repositories from a GitHub organization.

## Usage

You can run ghloner using environment variables or command-line flags:

### Environment Variables
- `GITHUB_ORG`: GitHub organization name
- `GITHUB_TOKEN`: GitHub personal access token
- `OUTPUT_DIR`: Directory where repositories will be cloned

### Command Line Flags

```
  -org string
    	GitHub organization name
  -output string
    	Output directory for cloned repositories
  -retry int
    	Number of retry attempts (default 5)
  -token string
    	GitHub personal access token
  -workers int
    	Number of concurrent workers (default 10)
```

### Examples

Basic usage with environment variables:

```bash
export GITHUB_ORG=myorg
export GITHUB_TOKEN=ghp_xxxxxxxxxxxx
export OUTPUT_DIR=./repos
ghloner
```

Using command line flags:

```bash
ghloner -org myorg -token ghp_xxxxxxxxxxxx -output ./repos
```

## Features

- **Concurrent cloning**: Clone multiple repositories in parallel with configurable worker pool
- **Automatic retries**: Retry failed operations with exponential backoff
- **Non-fast-forward recovery**: Automatically handles non-fast-forward errors by re-cloning
- **Progress tracking**: Real-time progress updates and detailed logging
- **Repository cleanup**: Remove local repositories that no longer exist in the organization
- **Graceful shutdown**: Properly handles interrupts (Ctrl+C) without corrupting repositories

## Development

### Building from Source

```bash
# Clone the repository
git clone https://github.com/truemilk/ghloner.git
cd ghloner

# Build the binary
go build -o ghloner ./cmd/ghloner

# Run tests
go test ./...

# Run tests with coverage
go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### Testing

The project includes comprehensive test coverage:

- **Unit tests** for all major components
- **Table-driven tests** for multiple scenarios
- **Mock implementations** for external dependencies
- **Test helpers** for creating test fixtures

Run tests with:

```bash
# All tests
go test ./...

# Specific package
go test -v ./internal/config

# With race detection
go test -race ./...

# Skip long-running tests
go test -short ./...
```

### Docker Support

Build and run with Docker:

```bash
# Build image
docker build -t ghloner .

# Run with environment variables
docker run -e GITHUB_ORG=myorg -e GITHUB_TOKEN=ghp_xxx -e OUTPUT_DIR=/repos -v $(pwd)/repos:/repos ghloner
```

## Requirements

- Go 1.23.2 or later
- GitHub personal access token with repo scope
- Git installed on the system

## License

MIT
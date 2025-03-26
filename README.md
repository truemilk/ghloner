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
ghloner -org myorg -token ghp_xxxxxxxxxxxx -output ./repos"
```
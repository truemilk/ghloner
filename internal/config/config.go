// Package config provides functionality for loading and managing the application configuration.
package config

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

// Config holds the configuration settings for the application.
type Config struct {
	Token           string
	OrgName         string
	OutputDir       string
	Workers         int
	RetryCount      int
	PostSyncCommand string
}

// NewConfig creates a new Config instance by loading configuration values from environment variables.
// It sets default values for Workers and RetryCount, and returns an error if any required environment
// variables are not set.
func NewConfig() *Config {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		fmt.Println("Error: GITHUB_TOKEN environment variable is not set")
		os.Exit(1)
	}

	orgName := os.Getenv("GITHUB_ORG")
	if orgName == "" {
		fmt.Println("Error: GITHUB_ORG environment variable is not set")
		os.Exit(1)
	}

	outputDir := os.Getenv("OUTPUT_DIR")
	if outputDir == "" {
		fmt.Println("Error: OUTPUT_DIR environment variable is not set")
		os.Exit(1)
	}

	return &Config{
		Token:      token,
		OrgName:    orgName,
		OutputDir:  outputDir,
		Workers:    10, // Default number of workers
		RetryCount: 3,  // Default number of retries
	}
}

// Parse reads configuration values from environment variables and command-line flags,
// and returns a Config instance. It sets default values for Workers and RetryCount,
// and returns an error if any required environment variables are not set.
func Parse() (*Config, error) {
	cfg := &Config{}

	// Set defaults
	cfg.Workers = 10        // Default number of workers
	cfg.RetryCount = 3      // Default number of retries
	cfg.OutputDir = "repos" // Default output directory

	// Define flags
	flag.StringVar(&cfg.OrgName, "org", os.Getenv("GITHUB_ORG"), "GitHub organization name")
	flag.StringVar(&cfg.Token, "token", os.Getenv("GITHUB_TOKEN"), "GitHub personal access token")
	flag.StringVar(&cfg.OutputDir, "output", os.Getenv("OUTPUT_DIR"), "Output directory for cloned repositories")
	flag.IntVar(&cfg.Workers, "workers", cfg.Workers, "Number of concurrent workers")
	flag.IntVar(&cfg.RetryCount, "retry", cfg.RetryCount, "Number of retry attempts")
	flag.StringVar(&cfg.PostSyncCommand, "post-sync", "", "Command to execute after syncing each repository (executed in the repository directory)")
	flag.Parse()
	// Validate required fields
	if cfg.OrgName == "" {
		return nil, fmt.Errorf("org is required (via --org flag or GITHUB_ORG environment variable)")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("token is required (via --token flag or GITHUB_TOKEN environment variable)")
	}
	if cfg.OutputDir == "" {
		return nil, fmt.Errorf("output directory is required (via --output flag or OUTPUT_DIR environment variable)")
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("error creating output directory: %w", err)
	}

	return cfg, nil
}

// NewGitHubClient creates a new GitHub API client using the provided personal access token.
// The client is configured to use the provided token for authentication.
// It returns the created client and any error that occurred during the creation.
func NewGitHubClient(token string) (*github.Client, error) {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc), nil
}

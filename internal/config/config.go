package config

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

type Config struct {
	Token      string
	OrgName    string
	OutputDir  string
	Workers    int
	RetryCount int
}

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
		outputDir = "repos" // Default output directory
	}

	return &Config{
		Token:      token,
		OrgName:    orgName,
		OutputDir:  outputDir,
		Workers:    10, // Default number of workers
		RetryCount: 3,  // Default number of retries
	}
}

func Parse() (*Config, error) {
	cfg := &Config{}

	// Set defaults
	cfg.Workers = 10    // Default number of workers
	cfg.RetryCount = 3  // Default number of retries
	cfg.OutputDir = "repos" // Default output directory

	// Define flags
	flag.StringVar(&cfg.OrgName, "org", os.Getenv("GITHUB_ORG"), "GitHub organization name")
	flag.StringVar(&cfg.Token, "token", os.Getenv("GITHUB_TOKEN"), "GitHub personal access token")
	flag.StringVar(&cfg.OutputDir, "output", cfg.OutputDir, "Output directory for cloned repositories")
	flag.IntVar(&cfg.Workers, "workers", cfg.Workers, "Number of concurrent workers")
	flag.IntVar(&cfg.RetryCount, "retry", cfg.RetryCount, "Number of retry attempts")
	flag.Parse()

	// Validate required fields
	if cfg.OrgName == "" || cfg.Token == "" {
		return nil, fmt.Errorf("Error: org and token are required (via flags or GITHUB_ORG/GITHUB_TOKEN environment variables)")
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("error creating output directory: %w", err)
	}

	return cfg, nil
}

func NewGitHubClient(token string) (*github.Client, error) {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc), nil
}

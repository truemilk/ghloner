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

func Parse() (*Config, error) {
	cfg := &Config{}

	cfg.Workers = 10
	cfg.RetryCount = 5

	flag.StringVar(&cfg.OrgName, "org", os.Getenv("GITHUB_ORG"), "GitHub organization name")
	flag.StringVar(&cfg.Token, "token", os.Getenv("GITHUB_TOKEN"), "GitHub personal access token")
	flag.StringVar(&cfg.OutputDir, "output", os.Getenv("OUTPUT_DIR"), "Output directory for cloned repositories")
	flag.IntVar(&cfg.Workers, "workers", cfg.Workers, "Number of concurrent workers")
	flag.IntVar(&cfg.RetryCount, "retry", cfg.RetryCount, "Number of retry attempts")
	flag.Parse()

	if cfg.OrgName == "" {
		return nil, fmt.Errorf("org is required (via --org flag or GITHUB_ORG environment variable)")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("token is required (via --token flag or GITHUB_TOKEN environment variable)")
	}
	if cfg.OutputDir == "" {
		return nil, fmt.Errorf("output directory is required (via --output flag or OUTPUT_DIR environment variable)")
	}

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

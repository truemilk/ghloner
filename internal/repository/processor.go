package repository

import (
	"context"
	"log/slog"

	"github.com/google/go-github/v60/github"
	"github.com/truemilk/ghloner/internal/config"
	"github.com/truemilk/ghloner/internal/repository/concurrency"
	"github.com/truemilk/ghloner/internal/repository/git"
	repoGithub "github.com/truemilk/ghloner/internal/repository/github"
	"github.com/truemilk/ghloner/internal/repository/progress"
	"github.com/truemilk/ghloner/internal/repository/storage"
)

// Processor coordinates the repository processing workflow
type Processor struct {
	config        *config.Config
	client        *github.Client
	repoLister    *repoGithub.RepositoryLister
	gitManager    *git.Manager
	fileManager   *storage.FileManager
	workerPool    *concurrency.WorkerPool
}

// NewProcessor creates a new processor instance
func NewProcessor(client *github.Client, cfg *config.Config) *Processor {
	return &Processor{
		config:      cfg,
		client:      client,
		repoLister:  repoGithub.NewRepositoryLister(client, cfg),
		gitManager:  git.NewManager(cfg),
		fileManager: storage.NewFileManager(cfg),
		workerPool:  concurrency.NewWorkerPool(cfg.Workers),
	}
}

// Run executes the repository processing workflow
func (p *Processor) Run(ctx context.Context) error {
	slog.Info("Starting processor", "workers", p.config.Workers, "retries", p.config.RetryCount)

	// List repositories from GitHub
	allRepos, err := p.repoLister.ListRepositories(ctx, p.config.OrgName)
	if err != nil {
		return err
	}
	
	slog.Info("Found repositories", "count", len(allRepos), "organization", p.config.OrgName)

	// Save repository list
	if err := p.fileManager.SaveRepositoryList(allRepos, p.config.OrgName); err != nil {
		return err
	}

	// Clean up old repositories
	if err := p.fileManager.CleanupOldRepositories(allRepos); err != nil {
		return err
	}

	// Create progress tracker
	showProgress := !p.config.NoProgress
	progressTracker := progress.NewProgressTracker(len(allRepos), p.config.Workers, showProgress, p.config.ProgressStyle)
	p.workerPool.SetProgressTracker(progressTracker)

	// Process repositories
	if err := p.processRepositories(ctx, allRepos); err != nil {
		progressTracker.PrintSummary()
		return err
	}

	progressTracker.PrintSummary()
	slog.Info("Successfully processed repositories", "count", len(allRepos), "outputDir", p.config.OutputDir)
	return nil
}

// processRepositories handles the concurrent processing of repositories
func (p *Processor) processRepositories(ctx context.Context, allRepos []*github.Repository) error {
	return p.workerPool.ProcessRepositories(ctx, allRepos, func(repo *github.Repository) error {
		return p.gitManager.ProcessRepository(repo, p.config.OrgName, p.config.Token)
	})
}

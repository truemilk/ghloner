package concurrency

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/go-github/v60/github"
	"github.com/truemilk/ghloner/internal/repository/progress"
)

// WorkerPool manages concurrent execution of tasks
type WorkerPool struct {
	workers         int
	progressTracker *progress.ProgressTracker
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(workers int) *WorkerPool {
	return &WorkerPool{
		workers: workers,
	}
}

// SetProgressTracker sets the progress tracker for the worker pool
func (p *WorkerPool) SetProgressTracker(tracker *progress.ProgressTracker) {
	p.progressTracker = tracker
}

// ProcessRepositories processes a slice of repositories concurrently
func (p *WorkerPool) ProcessRepositories(
	ctx context.Context,
	repos []*github.Repository,
	processFunc func(*github.Repository) error,
) error {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, p.workers)

	for i, repo := range repos {
		select {
		case <-ctx.Done():
			slog.Info("Stopping new repository processing")
			goto cleanup
		default:
			wg.Add(1)
			semaphore <- struct{}{}
			go func(repo *github.Repository, index int, workerID int) {
				defer wg.Done()
				defer func() { <-semaphore }()
				
				repoName := *repo.Name
				
				// Report start to progress tracker
				if p.progressTracker != nil {
					p.progressTracker.StartWorker(workerID, repoName, "processing")
				}
				
				// Process the repository
				err := processFunc(repo)
				
				// Report completion to progress tracker
				if p.progressTracker != nil {
					p.progressTracker.CompleteRepository(repoName, err == nil, err)
				}
				
				if err != nil {
					slog.Error("Error processing repository", 
						"repository", repoName, 
						"index", index, 
						"error", err)
				}
			}(repo, i, i%p.workers)
		}
	}

cleanup:
	wg.Wait()
	close(semaphore)

	if ctx.Err() != nil {
		return fmt.Errorf("program interrupted before completion: %w", ctx.Err())
	}

	return nil
}
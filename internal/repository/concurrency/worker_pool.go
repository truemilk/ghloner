package concurrency

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/go-github/v60/github"
)

// WorkerPool manages concurrent execution of tasks
type WorkerPool struct {
	workers int
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(workers int) *WorkerPool {
	return &WorkerPool{
		workers: workers,
	}
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
			go func(repo *github.Repository, index int) {
				defer wg.Done()
				defer func() { <-semaphore }()
				
				if err := processFunc(repo); err != nil {
					slog.Error("Error processing repository", 
						"repository", *repo.Name, 
						"index", index, 
						"error", err)
				}
			}(repo, i)
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
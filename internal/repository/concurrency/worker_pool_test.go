package concurrency

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWorkerPool(t *testing.T) {
	tests := []struct {
		name    string
		workers int
	}{
		{
			name:    "single worker",
			workers: 1,
		},
		{
			name:    "multiple workers",
			workers: 5,
		},
		{
			name:    "many workers",
			workers: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewWorkerPool(tt.workers)
			assert.NotNil(t, pool)
			assert.Equal(t, tt.workers, pool.workers)
		})
	}
}

func TestWorkerPool_ProcessRepositories(t *testing.T) {
	tests := []struct {
		name         string
		workers      int
		repoCount    int
		shouldError  []int // indices of repos that should error
		expectError  bool
	}{
		{
			name:      "process all repositories successfully",
			workers:   3,
			repoCount: 10,
		},
		{
			name:        "process with some errors",
			workers:     3,
			repoCount:   10,
			shouldError: []int{0, 3, 6},
		},
		{
			name:      "single worker processes all repositories",
			workers:   1,
			repoCount: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			pool := NewWorkerPool(tt.workers)

			// Create test repositories
			repos := make([]*github.Repository, tt.repoCount)
			for i := 0; i < tt.repoCount; i++ {
				name := "repo-" + string(rune('a'+i))
				repos[i] = &github.Repository{
					Name: github.String(name),
				}
			}

			// Track which repos were processed and errors
			var processed int32
			errorIndices := make(map[int]bool)
			for _, idx := range tt.shouldError {
				errorIndices[idx] = true
			}

			// Process repositories
			err := pool.ProcessRepositories(ctx, repos, func(repo *github.Repository) error {
				atomic.AddInt32(&processed, 1)
				
				// Find index of this repo
				for i, r := range repos {
					if r == repo {
						if errorIndices[i] {
							return errors.New("processing error")
						}
						break
					}
				}
				
				time.Sleep(10 * time.Millisecond)
				return nil
			})

			// Verify results
			assert.NoError(t, err) // ProcessRepositories doesn't return individual errors
			assert.Equal(t, int32(tt.repoCount), atomic.LoadInt32(&processed))
		})
	}
}

func TestWorkerPool_ProcessWithContextCancellation(t *testing.T) {
	pool := NewWorkerPool(3)
	ctx, cancel := context.WithCancel(context.Background())

	// Create test repositories
	repos := make([]*github.Repository, 10)
	for i := 0; i < 10; i++ {
		repos[i] = &github.Repository{
			Name: github.String("repo-" + string(rune('a'+i))),
		}
	}

	var processedCount int32

	// Cancel context after a short delay
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	// Process repositories
	err := pool.ProcessRepositories(ctx, repos, func(repo *github.Repository) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
			atomic.AddInt32(&processedCount, 1)
			return nil
		}
	})

	// Should have context cancellation error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interrupted")
	
	// Some repos might have been processed before cancellation
	processed := atomic.LoadInt32(&processedCount)
	assert.True(t, processed < 10, "Not all repositories should be processed")
}

func TestWorkerPool_ProcessEmptyRepositories(t *testing.T) {
	pool := NewWorkerPool(5)
	ctx := context.Background()

	// Process empty repository list
	err := pool.ProcessRepositories(ctx, []*github.Repository{}, func(repo *github.Repository) error {
		return nil
	})

	// Should not error
	assert.NoError(t, err)
}

func TestWorkerPool_ConcurrentProcessing(t *testing.T) {
	pool := NewWorkerPool(5)
	ctx := context.Background()

	// Track concurrent executions
	var concurrentCount int32
	maxConcurrent := int32(0)
	
	// Create test repositories
	repos := make([]*github.Repository, 20)
	for i := 0; i < 20; i++ {
		repos[i] = &github.Repository{
			Name: github.String("repo-" + string(rune('a'+i))),
		}
	}

	// Process repositories
	err := pool.ProcessRepositories(ctx, repos, func(repo *github.Repository) error {
		// Increment concurrent count
		current := atomic.AddInt32(&concurrentCount, 1)
		
		// Update max if needed
		for {
			max := atomic.LoadInt32(&maxConcurrent)
			if current <= max || atomic.CompareAndSwapInt32(&maxConcurrent, max, current) {
				break
			}
		}
		
		// Simulate work
		time.Sleep(20 * time.Millisecond)
		
		// Decrement concurrent count
		atomic.AddInt32(&concurrentCount, -1)
		return nil
	})

	// Verify no errors
	assert.NoError(t, err)
	
	// Verify concurrent processing occurred
	assert.True(t, maxConcurrent > 1, "Should have processed repositories concurrently")
	assert.True(t, maxConcurrent <= 5, "Should not exceed worker pool size")
}

func TestWorkerPool_LongRunningTasks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test")
	}

	pool := NewWorkerPool(2)
	ctx := context.Background()

	start := time.Now()
	
	// Create 4 repositories
	repos := make([]*github.Repository, 4)
	for i := 0; i < 4; i++ {
		repos[i] = &github.Repository{
			Name: github.String("repo-" + string(rune('a'+i))),
		}
	}

	// Process repositories with 100ms delay each
	// With 2 workers, this should take ~200ms total
	err := pool.ProcessRepositories(ctx, repos, func(repo *github.Repository) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	elapsed := time.Since(start)

	// Verify no errors
	assert.NoError(t, err)
	
	// Verify parallel processing (should take ~200ms, not 400ms)
	assert.True(t, elapsed < 300*time.Millisecond, "Repositories should be processed in parallel")
	assert.True(t, elapsed > 150*time.Millisecond, "Processing should take expected time")
}
package progress

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProgressTracker(t *testing.T) {
	tests := []struct {
		name          string
		totalRepos    int
		workers       int
		showProgress  bool
		progressStyle string
	}{
		{
			name:          "with progress bar",
			totalRepos:    10,
			workers:       4,
			showProgress:  true,
			progressStyle: "bar",
		},
		{
			name:          "without progress bar",
			totalRepos:    5,
			workers:       2,
			showProgress:  false,
			progressStyle: "simple",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewProgressTracker(tt.totalRepos, tt.workers, tt.showProgress, tt.progressStyle)
			require.NotNil(t, tracker)
			assert.Equal(t, tt.totalRepos, tracker.totalRepos)
			assert.Equal(t, tt.showProgress, tracker.showProgress)
			assert.Equal(t, tt.progressStyle, tracker.progressStyle)
			assert.Equal(t, 0, tracker.completedRepos)
			assert.Equal(t, 0, tracker.failedRepos)
			assert.NotNil(t, tracker.workerStatuses)
			assert.NotNil(t, tracker.recentResults)
		})
	}
}

func TestStartWorker(t *testing.T) {
	tracker := NewProgressTracker(10, 4, false, "simple")
	
	tracker.StartWorker(1, "repo1", "cloning")
	tracker.StartWorker(2, "repo2", "updating")
	
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()
	
	assert.Len(t, tracker.workerStatuses, 2)
	assert.Equal(t, "repo1", tracker.workerStatuses[1].Repository)
	assert.Equal(t, "cloning", tracker.workerStatuses[1].Operation)
	assert.Equal(t, "repo2", tracker.workerStatuses[2].Repository)
	assert.Equal(t, "updating", tracker.workerStatuses[2].Operation)
}

func TestCompleteRepository(t *testing.T) {
	tests := []struct {
		name        string
		repoName    string
		success     bool
		expectError bool
	}{
		{
			name:        "successful completion",
			repoName:    "repo1",
			success:     true,
			expectError: false,
		},
		{
			name:        "failed completion",
			repoName:    "repo2",
			success:     false,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewProgressTracker(10, 4, false, "simple")
			
			// Start a worker
			tracker.StartWorker(1, tt.repoName, "processing")
			
			// Complete the repository
			var err error
			if tt.expectError {
				err = assert.AnError
			}
			tracker.CompleteRepository(tt.repoName, tt.success, err)
			
			// Check results
			assert.Equal(t, 1, tracker.completedRepos)
			if !tt.success {
				assert.Equal(t, 1, tracker.failedRepos)
			} else {
				assert.Equal(t, 0, tracker.failedRepos)
			}
			
			// Check worker status was removed
			tracker.mu.RLock()
			_, exists := tracker.workerStatuses[1]
			tracker.mu.RUnlock()
			assert.False(t, exists)
			
			// Check recent results
			assert.Len(t, tracker.recentResults, 1)
			assert.Equal(t, tt.repoName, tracker.recentResults[0].Name)
			assert.Equal(t, tt.success, tracker.recentResults[0].Success)
			if tt.expectError {
				assert.NotNil(t, tracker.recentResults[0].Error)
			}
		})
	}
}

func TestGetETA(t *testing.T) {
	tracker := NewProgressTracker(10, 4, false, "simple")
	
	// No completions yet
	eta := tracker.GetETA()
	assert.Equal(t, time.Duration(0), eta)
	
	// Simulate some completions
	tracker.StartWorker(1, "repo1", "processing")
	time.Sleep(100 * time.Millisecond)
	tracker.CompleteRepository("repo1", true, nil)
	
	tracker.StartWorker(1, "repo2", "processing")
	time.Sleep(100 * time.Millisecond)
	tracker.CompleteRepository("repo2", true, nil)
	
	// Now ETA should be calculated
	eta = tracker.GetETA()
	assert.Greater(t, eta, time.Duration(0))
}

func TestGetRate(t *testing.T) {
	tracker := NewProgressTracker(10, 4, false, "simple")
	
	// Initially rate should be 0
	rate := tracker.GetRate()
	assert.Equal(t, float64(0), rate)
	
	// Wait to ensure we have at least 1 second elapsed
	time.Sleep(1100 * time.Millisecond)
	
	// Complete some repositories
	for i := 0; i < 5; i++ {
		tracker.StartWorker(1, "repo"+string(rune('0'+i)), "processing")
		tracker.CompleteRepository("repo"+string(rune('0'+i)), true, nil)
	}
	
	rate = tracker.GetRate()
	assert.Greater(t, rate, float64(0))
}

func TestPrintSummary(t *testing.T) {
	var buf bytes.Buffer
	tracker := NewProgressTracker(10, 4, false, "simple")
	tracker.output = &buf
	
	// Simulate some processing
	tracker.completedRepos = 8
	tracker.failedRepos = 2
	tracker.avgDuration = 5 * time.Second
	
	tracker.PrintSummary()
	
	output := buf.String()
	assert.Contains(t, output, "=== Summary ===")
	assert.Contains(t, output, "Total repositories: 10")
	assert.Contains(t, output, "Successfully processed: 6")
	assert.Contains(t, output, "Failed: 2")
}

func TestRecentResultsLimit(t *testing.T) {
	tracker := NewProgressTracker(10, 4, false, "simple")
	
	// Add more than 5 results
	for i := 0; i < 10; i++ {
		tracker.CompleteRepository(string(rune('a'+i)), true, nil)
	}
	
	// Should only keep the last 5
	assert.Len(t, tracker.recentResults, 5)
	assert.Equal(t, "f", tracker.recentResults[0].Name)
	assert.Equal(t, "j", tracker.recentResults[4].Name)
}

func TestConcurrentAccess(t *testing.T) {
	tracker := NewProgressTracker(100, 10, false, "simple")
	
	// Simulate concurrent workers
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(workerID int) {
			for j := 0; j < 10; j++ {
				repoName := string(rune('a'+workerID)) + string(rune('0'+j))
				tracker.StartWorker(workerID, repoName, "processing")
				time.Sleep(10 * time.Millisecond)
				tracker.CompleteRepository(repoName, true, nil)
			}
			done <- true
		}(i)
	}
	
	// Wait for all workers to complete
	for i := 0; i < 10; i++ {
		<-done
	}
	
	assert.Equal(t, 100, tracker.completedRepos)
	assert.Equal(t, 0, tracker.failedRepos)
}
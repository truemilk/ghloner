package progress

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/term"
)

// WorkerStatus represents the current status of a worker
type WorkerStatus struct {
	ID         int
	Repository string
	Operation  string
	StartTime  time.Time
}

// RepositoryResult represents the result of processing a repository
type RepositoryResult struct {
	Name      string
	Success   bool
	Duration  time.Duration
	Error     error
	Timestamp time.Time
}

// ProgressTracker manages progress reporting for repository operations
type ProgressTracker struct {
	totalRepos      int
	completedRepos  int
	failedRepos     int
	startTime       time.Time
	progressBar     *progressbar.ProgressBar
	workerStatuses  map[int]*WorkerStatus
	recentResults   []RepositoryResult
	avgDuration     time.Duration
	durationsSum    time.Duration
	durationCount   int
	mu              sync.RWMutex
	output          io.Writer
	showProgress    bool
	progressStyle   string
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(totalRepos int, workers int, showProgress bool, progressStyle string) *ProgressTracker {
	tracker := &ProgressTracker{
		totalRepos:     totalRepos,
		startTime:      time.Now(),
		workerStatuses: make(map[int]*WorkerStatus),
		recentResults:  make([]RepositoryResult, 0, 10),
		output:         os.Stderr,
		showProgress:   showProgress,
		progressStyle:  progressStyle,
	}

	if showProgress && isTerminal() {
		tracker.progressBar = progressbar.NewOptions(totalRepos,
			progressbar.OptionSetWriter(tracker.output),
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
			progressbar.OptionSetItsString("repos"),
			progressbar.OptionSetWidth(40),
			progressbar.OptionSetDescription("Processing repositories..."),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "[green]█[reset]",
				SaucerHead:    "[green]█[reset]",
				SaucerPadding: "░",
				BarStart:      "[",
				BarEnd:        "]",
			}),
			progressbar.OptionOnCompletion(func() {
				fmt.Fprint(tracker.output, "\n")
			}),
		)
	}

	return tracker
}

// StartWorker records that a worker has started processing a repository
func (t *ProgressTracker) StartWorker(workerID int, repoName string, operation string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.workerStatuses[workerID] = &WorkerStatus{
		ID:         workerID,
		Repository: repoName,
		Operation:  operation,
		StartTime:  time.Now(),
	}

	t.updateDisplay()
}

// CompleteRepository records the completion of a repository
func (t *ProgressTracker) CompleteRepository(repoName string, success bool, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.completedRepos++
	if !success {
		t.failedRepos++
	}

	// Find and remove the worker status
	var duration time.Duration
	for id, status := range t.workerStatuses {
		if status.Repository == repoName {
			duration = time.Since(status.StartTime)
			delete(t.workerStatuses, id)
			break
		}
	}

	// Update average duration
	if success && duration > 0 {
		t.durationsSum += duration
		t.durationCount++
		t.avgDuration = t.durationsSum / time.Duration(t.durationCount)
	}

	// Add to recent results
	result := RepositoryResult{
		Name:      repoName,
		Success:   success,
		Duration:  duration,
		Error:     err,
		Timestamp: time.Now(),
	}

	t.recentResults = append(t.recentResults, result)
	if len(t.recentResults) > 5 {
		t.recentResults = t.recentResults[1:]
	}

	if t.progressBar != nil {
		t.progressBar.Add(1)
	}

	t.updateDisplay()
}

// GetETA calculates the estimated time of completion
func (t *ProgressTracker) GetETA() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.completedRepos == 0 || t.avgDuration == 0 {
		return 0
	}

	remainingRepos := t.totalRepos - t.completedRepos
	return time.Duration(remainingRepos) * t.avgDuration
}

// GetRate calculates the current processing rate (repos per minute)
func (t *ProgressTracker) GetRate() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	elapsed := time.Since(t.startTime)
	if elapsed < time.Second {
		return 0
	}

	return float64(t.completedRepos) / elapsed.Minutes()
}

// PrintSummary prints a final summary of the operation
func (t *ProgressTracker) PrintSummary() {
	t.mu.RLock()
	defer t.mu.RUnlock()

	elapsed := time.Since(t.startTime)
	fmt.Fprintf(t.output, "\n=== Summary ===\n")
	fmt.Fprintf(t.output, "Total repositories: %d\n", t.totalRepos)
	fmt.Fprintf(t.output, "Successfully processed: %d\n", t.completedRepos-t.failedRepos)
	fmt.Fprintf(t.output, "Failed: %d\n", t.failedRepos)
	fmt.Fprintf(t.output, "Total time: %s\n", elapsed.Round(time.Second))
	if t.completedRepos > 0 {
		fmt.Fprintf(t.output, "Average time per repo: %s\n", t.avgDuration.Round(time.Second))
		fmt.Fprintf(t.output, "Processing rate: %.2f repos/minute\n", t.GetRate())
	}
}

// updateDisplay updates the progress display
func (t *ProgressTracker) updateDisplay() {
	if !t.showProgress || t.progressStyle != "verbose" {
		return
	}

	// This is where we would implement the multi-line display
	// For now, the progress bar handles the basic display
}

// isTerminal checks if the output is a terminal
func isTerminal() bool {
	if fileInfo, _ := os.Stderr.Stat(); (fileInfo.Mode() & os.ModeCharDevice) != 0 {
		return term.IsTerminal(int(os.Stderr.Fd()))
	}
	return false
}
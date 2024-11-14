package repository

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/truemilk/ghloner/internal/config"
)

type Processor struct {
	client     *github.Client
	config     *config.Config
	printMutex sync.Mutex
	stats      *ProcessorStats
}

type ProcessorStats struct {
	startTime     time.Time
	updatedRepos  int
	newRepos      int
	deletedRepos  int
	skippedRepos  int
	retriedSuccess int
	retriedFailure int
	printMutex    sync.Mutex
}

func NewProcessor(client *github.Client, cfg *config.Config) *Processor {
	return &Processor{
		client: client,
		config: cfg,
		stats: &ProcessorStats{
			startTime: time.Now(),
		},
	}
}

func (p *Processor) Run(ctx context.Context) error {
	fmt.Printf("Starting processor with %d workers...\n", p.config.Workers)

	// List all repositories
	allRepos, err := p.listRepositories(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Found %d repositories in organization %s\n", len(allRepos), p.config.OrgName)

	// Save repository list
	if err := p.saveRepositoryList(allRepos); err != nil {
		return err
	}

	// Clean up old repositories
	if err := p.cleanupOldRepositories(allRepos); err != nil {
		return err
	}

	// Process repositories concurrently
	if err := p.processRepositories(ctx, allRepos); err != nil {
		return err
	}

	p.printSummary()
	return nil
}

func (p *Processor) listRepositories(ctx context.Context) ([]*github.Repository, error) {
	opt := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var allRepos []*github.Repository
	for {
		repos, resp, err := p.client.Repositories.ListByOrg(ctx, p.config.OrgName, opt)
		if err != nil {
			return nil, fmt.Errorf("error fetching repositories: %w", err)
		}
		allRepos = append(allRepos, repos...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	return allRepos, nil
}

func (p *Processor) saveRepositoryList(allRepos []*github.Repository) error {
	repoListPath := filepath.Join(p.config.OutputDir, "repository_list.txt")
	f, err := os.Create(repoListPath)
	if err != nil {
		return fmt.Errorf("error creating repository list file: %w", err)
	}
	defer f.Close()

	for _, repo := range allRepos {
		if _, err := f.WriteString(fmt.Sprintf("%s - %s\n", *repo.Name, *repo.SSHURL)); err != nil {
			return fmt.Errorf("error writing to repository list file: %w", err)
		}
	}

	fmt.Printf("Repository list saved to: %s\n", repoListPath)
	return nil
}

func (p *Processor) cleanupOldRepositories(allRepos []*github.Repository) error {
	validRepos := make(map[string]bool)
	for _, repo := range allRepos {
		validRepos[*repo.Name] = true
	}

	entries, err := os.ReadDir(p.config.OutputDir)
	if err != nil {
		return fmt.Errorf("error reading output directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != ".git" {
			if !validRepos[entry.Name()] {
				fullPath := filepath.Join(p.config.OutputDir, entry.Name())
				fmt.Printf("Removing %s as it no longer exists in the organization...\n", entry.Name())
				if err := os.RemoveAll(fullPath); err != nil {
					return fmt.Errorf("error removing directory %s: %w", fullPath, err)
				}
				p.stats.printMutex.Lock()
				p.stats.deletedRepos++
				p.stats.printMutex.Unlock()
			}
		}
	}

	return nil
}

func (p *Processor) processRepositories(ctx context.Context, allRepos []*github.Repository) error {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, p.config.Workers)

	for i, repo := range allRepos {
		select {
		case <-ctx.Done():
			fmt.Println("Stopping new repository processing...")
			goto cleanup
		default:
			wg.Add(1)
			semaphore <- struct{}{} // Acquire semaphore
			go func(repo *github.Repository, index int) {
				p.processRepository(&wg, repo, index, len(allRepos))
				<-semaphore // Release semaphore
			}(repo, i)
		}
	}

cleanup:
	wg.Wait()
	close(semaphore)

	if ctx.Err() != nil {
		return fmt.Errorf("program interrupted before completion")
	}

	fmt.Printf("\nSuccessfully processed %d repositories in %s\n", len(allRepos), p.config.OutputDir)
	return nil
}

// Add a helper function for retrying commands
func (p *Processor) runCommandWithRetry(cmd *exec.Cmd, repoName string, operation string) error {
	for attempt := 1; attempt <= p.config.RetryCount; attempt++ {
		if output, err := cmd.CombinedOutput(); err != nil {
			if attempt == p.config.RetryCount {
				p.stats.printMutex.Lock()
				p.stats.retriedFailure++
				p.stats.printMutex.Unlock()
				return fmt.Errorf("%s\n%s", err, output)
			}
			p.printMutex.Lock()
			fmt.Printf("Attempt %d/%d: Error %s %s: %v\nRetrying in 5 seconds...\n", 
				attempt, p.config.RetryCount, operation, repoName, err)
			p.printMutex.Unlock()
			time.Sleep(5 * time.Second)
			cmd = exec.Command(cmd.Path, cmd.Args[1:]...) // Create a new command with the same arguments
			continue
		}
		if attempt > 1 {
			p.stats.printMutex.Lock()
			p.stats.retriedSuccess++
			p.stats.printMutex.Unlock()
		}
		return nil
	}
	return nil
}

func (p *Processor) processRepository(wg *sync.WaitGroup, repo *github.Repository, index, total int) {
	defer wg.Done()

	p.printMutex.Lock()
	fmt.Printf("[%d/%d] ", index+1, total)
	p.printMutex.Unlock()

	repoPath := filepath.Join(p.config.OutputDir, *repo.Name)
	cloneURL := fmt.Sprintf("git@github.com:%s/%s.git", p.config.OrgName, *repo.Name)

	// Check if directory exists
	if _, err := os.Stat(repoPath); err == nil {
		// Directory exists, check if it's a git repo
		cmd := exec.Command("git", "-C", repoPath, "rev-parse")
		if err := cmd.Run(); err != nil {
			fmt.Printf("Warning: %s exists but is not a git repository. Skipping...\n", repoPath)
			p.stats.printMutex.Lock()
			p.stats.skippedRepos++
			p.stats.printMutex.Unlock()
			return
		}

		// It's a git repo, fetch updates
		fmt.Printf("Fetching updates for %s...\n", *repo.Name)
		cmd = exec.Command("git", "-C", repoPath, "fetch", "--all")
		if err := p.runCommandWithRetry(cmd, *repo.Name, "fetching updates for"); err != nil {
			fmt.Printf("Error fetching updates for %s: %v\n", *repo.Name, err)
			return
		}

		// Get the current branch
		branchCmd := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
		branch, err := branchCmd.Output()
		if err != nil {
			fmt.Printf("Error getting current branch for %s: %v\n", *repo.Name, err)
			return
		}
		branchName := strings.TrimSpace(string(branch))

		// Show files that would be changed by pulling
		diffCmd := exec.Command("git", "-C", repoPath, "diff", "--name-status",
			fmt.Sprintf("HEAD..origin/%s", branchName))
		diffOutput, err := diffCmd.Output()
		if err != nil {
			fmt.Printf("Error getting diff for %s: %v\n", *repo.Name, err)
			return
		}

		// If there are changes, display them
		if len(diffOutput) > 0 {
			p.stats.printMutex.Lock()
			p.stats.updatedRepos++
			p.stats.printMutex.Unlock()
			fmt.Printf("Changes in %s:\n%s\n", *repo.Name, string(diffOutput))
		} else {
			fmt.Printf("No changes in %s\n", *repo.Name)
		}
	} else if os.IsNotExist(err) {
		// Directory doesn't exist, clone it
		fmt.Printf("Cloning %s...\n", *repo.Name)
		cmd := exec.Command("git", "clone", cloneURL, repoPath)
		if err := p.runCommandWithRetry(cmd, *repo.Name, "cloning"); err != nil {
			fmt.Printf("Error cloning %s: %v\n", *repo.Name, err)
			return
		}
		p.stats.printMutex.Lock()
		p.stats.newRepos++
		p.stats.printMutex.Unlock()
	} else {
		// Some other error occurred
		fmt.Printf("Error checking directory %s: %v\n", repoPath, err)
		return
	}
}

func (p *Processor) printSummary() {
	fmt.Printf("\nSummary:\n")
	fmt.Printf("- New repositories cloned: %d\n", p.stats.newRepos)
	fmt.Printf("- Existing repositories updated: %d\n", p.stats.updatedRepos)
	fmt.Printf("- Repositories skipped: %d\n", p.stats.skippedRepos)
	fmt.Printf("- Operations succeeded after retries: %d\n", p.stats.retriedSuccess)
	fmt.Printf("- Operations failed despite retries: %d\n", p.stats.retriedFailure)
}

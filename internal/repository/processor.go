// Package repository provides functionality for managing GitHub repositories.
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

// Processor is a struct that manages the processing of GitHub repositories.
// It contains a GitHub client, configuration, a mutex for printing, and statistics about the processing.
type Processor struct {
	client     *github.Client
	config     *config.Config
	printMutex sync.Mutex
	stats      *ProcessorStats
}

// ProcessorStats contains statistics about the processing of GitHub repositories.
type ProcessorStats struct {
	startTime         time.Time
	updatedRepos      int
	newRepos          int
	deletedRepos      int
	skippedRepos      int
	cloneRetrySuccess int
	fetchRetrySuccess int
	retriedFailure    int
	newRepoNames      []string
	updatedRepoNames  []string
	deletedRepoNames  []string
	skippedRepoNames  []string
	failedRepoNames   []string
	printMutex        sync.Mutex
}

// NewProcessor creates a new Processor instance with the provided GitHub client and configuration.
// The Processor is responsible for managing the processing of GitHub repositories, including
// listing all repositories in an organization, saving the repository list, cleaning up old
// repositories, and processing the repositories concurrently.
func NewProcessor(client *github.Client, cfg *config.Config) *Processor {
	return &Processor{
		client: client,
		config: cfg,
		stats: &ProcessorStats{
			startTime: time.Now(),
		},
	}
}

// Run executes the processor, which lists all repositories in the configured organization,
// saves the repository list, cleans up old repositories, and processes the repositories
// concurrently. It prints a summary of the processing at the end.
func (p *Processor) Run(ctx context.Context) error {
	fmt.Printf("Starting processor with %d workers and %d retries...\n", p.config.Workers, p.config.RetryCount)

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

// listRepositories fetches all repositories for the configured organization using the GitHub API.
// It handles pagination and returns a slice of all repositories found.
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

// saveRepositoryList saves a list of all repositories in the configured organization to a file.
// The file is saved in the configured output directory with the name "repository_list.txt".
// Each line in the file contains the repository name and its SSH URL, separated by a hyphen.
// The function returns an error if there is a problem creating or writing to the file.
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

// cleanupOldRepositories removes any directories in the configured output directory that
// do not correspond to a repository that currently exists in the organization. This helps
// keep the output directory clean and up-to-date.
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
				p.stats.deletedRepoNames = append(p.stats.deletedRepoNames, entry.Name())
				p.stats.printMutex.Unlock()
			}
		}
	}

	return nil
}

// processRepositories processes a list of GitHub repositories in parallel, using a semaphore to limit
// the number of concurrent operations. It checks out each repository, and if the repository already
// exists, it checks if it's a valid Git repository. If not, it skips the repository. The function
// also creates a repository list file containing the repository names and SSH URLs.
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

// runCommandWithRetry is a helper function that runs the provided command and retries it up to the configured
// number of times if an error occurs. It tracks the number of successful and failed retries in the Processor's
// stats. If the maximum number of retries is reached and the command still fails, the function returns the
// error along with the command's output.
func (p *Processor) runCommandWithRetry(cmd *exec.Cmd, repoName string, operation string) error {
	for attempt := 1; attempt <= p.config.RetryCount; attempt++ {
		if output, err := cmd.CombinedOutput(); err != nil {
			if attempt == p.config.RetryCount {
				p.stats.printMutex.Lock()
				p.stats.retriedFailure++
				p.stats.failedRepoNames = append(p.stats.failedRepoNames, repoName)
				p.stats.printMutex.Unlock()
				return fmt.Errorf("%s\n%s", err, output)
			}
			p.printMutex.Lock()
			fmt.Printf("Attempt %d/%d: Error %s %s: %v\nRetrying in 5 seconds...\n",
				attempt, p.config.RetryCount, operation, repoName, err)
			p.printMutex.Unlock()
			time.Sleep(5 * time.Second)
			cmd = exec.Command(cmd.Path, cmd.Args[1:]...)
			continue
		}
		if attempt > 1 {
			p.stats.printMutex.Lock()
			if strings.Contains(operation, "cloning") {
				p.stats.cloneRetrySuccess++
			} else if strings.Contains(operation, "fetching") {
				p.stats.fetchRetrySuccess++
			}
			p.stats.printMutex.Unlock()
		}
		return nil
	}
	return nil
}

// processRepository processes a single repository by either cloning it or fetching updates,
// and prints a summary of the operations performed. It is called concurrently for each
// repository to be processed.
//
// The function first checks if the repository directory exists. If it does, it checks if
// it's a valid Git repository and fetches updates. If the directory doesn't exist, it
// clones the repository.
//
// The function updates the Processor's stats to track the number of new, updated, deleted,
// and skipped repositories, as well as the number of successful and failed retries.
func (p *Processor) processRepository(wg *sync.WaitGroup, repo *github.Repository, index, total int) {
	defer wg.Done()

	p.printMutex.Lock()
	fmt.Printf("[%d/%d] ", index+1, total)
	p.printMutex.Unlock()

	repoPath := filepath.Join(p.config.OutputDir, *repo.Name)
	cloneURL := fmt.Sprintf("git@github.com:%s/%s.git", p.config.OrgName, *repo.Name)

	wasUpdated := false

	// Check if directory exists
	if _, err := os.Stat(repoPath); err == nil {
		// Directory exists, fetch updates
		fetchCmd := exec.Command("git", "-C", repoPath, "fetch", "--all")
		if err := p.runCommandWithRetry(fetchCmd, *repo.Name, "fetching updates for"); err != nil {
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

		// Get the current commit hash before fetch
		beforeCmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
		beforeHash, err := beforeCmd.Output()
		if err != nil {
			fmt.Printf("Error getting current hash for %s: %v\n", *repo.Name, err)
			return
		}

		// Fetch updates
		fmt.Printf("Fetching updates for %s...\n", *repo.Name)
		fetchCmd = exec.Command("git", "-C", repoPath, "fetch", "origin", branchName)
		if err := p.runCommandWithRetry(fetchCmd, *repo.Name, "fetching updates for"); err != nil {
			fmt.Printf("Error fetching updates for %s: %v\n", *repo.Name, err)
			return
		}

		// Get the remote commit hash
		remoteCmd := exec.Command("git", "-C", repoPath, "rev-parse", fmt.Sprintf("origin/%s", branchName))
		remoteHash, err := remoteCmd.Output()
		if err != nil {
			fmt.Printf("Error getting remote hash for %s: %v\n", *repo.Name, err)
			return
		}

		// Compare the hashes
		if string(beforeHash) != string(remoteHash) {
			// Only if there are actual changes, pull them
			pullCmd := exec.Command("git", "-C", repoPath, "pull", "origin", branchName)
			if err := p.runCommandWithRetry(pullCmd, *repo.Name, "pulling updates for"); err != nil {
				fmt.Printf("Error pulling updates for %s: %v\n", *repo.Name, err)
				return
			}

			p.stats.printMutex.Lock()
			p.stats.updatedRepos++
			p.stats.updatedRepoNames = append(p.stats.updatedRepoNames, *repo.Name)
			p.stats.printMutex.Unlock()
			fmt.Printf("Updated %s from %s to %s\n", *repo.Name,
				strings.TrimSpace(string(beforeHash))[:8],
				strings.TrimSpace(string(remoteHash))[:8])
			wasUpdated = true
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
		p.stats.newRepoNames = append(p.stats.newRepoNames, *repo.Name)
		p.stats.printMutex.Unlock()
		wasUpdated = true
	} else {
		// Some other error occurred
		fmt.Printf("Error checking directory %s: %v\n", repoPath, err)
		return
	}

	// Execute post-sync command if specified and the repository was cloned or updated
	if p.config.PostSyncCommand != "" && wasUpdated {
		fmt.Printf("Executing post-sync command '%s' for %s...\n", p.config.PostSyncCommand, *repo.Name)
		cmd := exec.Command("sh", "-c", p.config.PostSyncCommand)
		cmd.Dir = repoPath // Set working directory to the repository
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("Error executing post-sync command for %s: %v\nOutput: %s\n", *repo.Name, err, output)
		} else {
			fmt.Printf("Post-sync command completed successfully for %s\nOutput: %s\n", *repo.Name, output)
		}
	}
}

// printSummary prints a summary of the repository processing operations, including the number of new repositories cloned, existing repositories updated, repositories deleted, repositories skipped, and operations that failed despite retries. It also prints the total time taken for the processing.
func (p *Processor) printSummary() {
	fmt.Printf("\nSummary:\n")

	fmt.Printf("- New repositories cloned (%d):\n", p.stats.newRepos)
	for _, name := range p.stats.newRepoNames {
		repoPath := filepath.Join(p.config.OutputDir, name)
		cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
		hash, err := cmd.Output()
		if err != nil {
			fmt.Printf("  • %s (error getting hash: %v)\n", name, err)
			continue
		}
		fmt.Printf("  • %s (%s)\n", name, strings.TrimSpace(string(hash)))
	}

	fmt.Printf("\n- Existing repositories updated (%d):\n", p.stats.updatedRepos)
	for _, name := range p.stats.updatedRepoNames {
		repoPath := filepath.Join(p.config.OutputDir, name)
		cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
		hash, err := cmd.Output()
		if err != nil {
			fmt.Printf("  • %s (error getting hash: %v)\n", name, err)
			continue
		}
		fmt.Printf("  • %s (%s)\n", name, strings.TrimSpace(string(hash)))
	}

	fmt.Printf("\n- Repositories deleted (%d):\n", p.stats.deletedRepos)
	for _, name := range p.stats.deletedRepoNames {
		fmt.Printf("  • %s (deleted)\n", name)
	}

	fmt.Printf("\n- Repositories skipped (%d):\n", p.stats.skippedRepos)
	for _, name := range p.stats.skippedRepoNames {
		repoPath := filepath.Join(p.config.OutputDir, name)
		cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
		hash, err := cmd.Output()
		if err != nil {
			fmt.Printf("  • %s (error getting hash: %v)\n", name, err)
			continue
		}
		fmt.Printf("  • %s (%s)\n", name, strings.TrimSpace(string(hash)))
	}

	fmt.Printf("\n- Operations failed despite retries (%d):\n", p.stats.retriedFailure)
	for _, name := range p.stats.failedRepoNames {
		repoPath := filepath.Join(p.config.OutputDir, name)
		cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
		hash, err := cmd.Output()
		if err != nil {
			fmt.Printf("  • %s (error getting hash: %v)\n", name, err)
			continue
		}
		fmt.Printf("  • %s (%s)\n", name, strings.TrimSpace(string(hash)))
	}

	fmt.Printf("\n- Clone operations succeeded after retries: %d\n", p.stats.cloneRetrySuccess)
	fmt.Printf("- Fetch operations succeeded after retries: %d\n", p.stats.fetchRetrySuccess)

	fmt.Printf("\nTotal time taken: %s\n", time.Since(p.stats.startTime))
}

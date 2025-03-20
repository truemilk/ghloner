package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/truemilk/ghloner/internal/config"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

type Processor struct {
	client       *github.Client
	config       *config.Config
	printMutex   sync.Mutex
	stats        *ProcessorStats
	repositories map[string]*git.Repository
	repoMutex    sync.Mutex
}

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

func NewProcessor(client *github.Client, cfg *config.Config) *Processor {
	return &Processor{
		client:       client,
		config:       cfg,
		repositories: make(map[string]*git.Repository),
		stats: &ProcessorStats{
			startTime: time.Now(),
		},
	}
}

func (p *Processor) Run(ctx context.Context) error {
	fmt.Printf("Starting processor with %d workers and %d retries...\n", p.config.Workers, p.config.RetryCount)

	allRepos, err := p.listRepositories(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Found %d repositories in organization %s\n", len(allRepos), p.config.OrgName)

	if err := p.saveRepositoryList(allRepos); err != nil {
		return err
	}

	if err := p.cleanupOldRepositories(allRepos); err != nil {
		return err
	}

	if err := p.processRepositories(ctx, allRepos); err != nil {
		return err
	}

	p.printSummary()
	return nil
}

func (p *Processor) listRepositories(ctx context.Context) ([]*github.Repository, error) {
	// Make initial request to get first page and determine total pages
	opt := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	// Get first page and determine total pages
	firstPageRepos, resp, err := p.client.Repositories.ListByOrg(ctx, p.config.OrgName, opt)
	if err != nil {
		return nil, fmt.Errorf("error fetching first page of repositories: %w", err)
	}

	// If only one page, return immediately
	if resp.NextPage == 0 {
		return firstPageRepos, nil
	}

	// Determine total pages
	totalPages := resp.LastPage
	if totalPages == 0 {
		// Calculate based on NextPage if LastPage is not available
		totalPages = resp.NextPage
	}

	fmt.Printf("Found %d pages of repositories, fetching concurrently with %d workers...\n",
		totalPages, p.config.Workers)

	// Create channels for results and errors
	type pageResult struct {
		page  int
		repos []*github.Repository
		err   error
	}
	resultChan := make(chan pageResult, totalPages)

	// Add first page results
	resultChan <- pageResult{page: 1, repos: firstPageRepos}

	// Create worker pool with size limited by config.Workers
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, p.config.Workers)

	// Fetch remaining pages concurrently
	for page := 2; page <= totalPages; page++ {
		// Check if context is cancelled before starting a new goroutine
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			// Continue with the next page
		}

		wg.Add(1)
		semaphore <- struct{}{}

		go func(pageNum int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			// Check context again inside goroutine
			select {
			case <-ctx.Done():
				resultChan <- pageResult{page: pageNum, err: ctx.Err()}
				return
			default:
				// Continue with the fetch
			}

			pageOpt := &github.RepositoryListByOrgOptions{
				ListOptions: github.ListOptions{Page: pageNum, PerPage: 100},
			}

			repos, _, err := p.client.Repositories.ListByOrg(ctx, p.config.OrgName, pageOpt)
			if err != nil {
				resultChan <- pageResult{page: pageNum, err: err}
				return
			}

			fmt.Printf("Fetched page %d of repositories\n", pageNum)
			resultChan <- pageResult{page: pageNum, repos: repos}
		}(page)
	}

	// Close result channel when all workers are done
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect and combine results
	var allRepos []*github.Repository
	allRepos = append(allRepos, firstPageRepos...)

	// Map to store results by page number for proper ordering
	pageMap := make(map[int][]*github.Repository)

	// Process results as they come in
	for result := range resultChan {
		if result.err != nil {
			// If context was cancelled, return that error
			if errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
				return nil, result.err
			}
			return nil, fmt.Errorf("error fetching page %d: %w", result.page, result.err)
		}

		// Store in map by page number
		pageMap[result.page] = result.repos
	}

	// Combine results in correct order
	for page := 2; page <= totalPages; page++ {
		if repos, ok := pageMap[page]; ok {
			allRepos = append(allRepos, repos...)
		}
	}

	fmt.Printf("Successfully fetched all %d pages of repositories\n", totalPages)
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
		httpsURL := fmt.Sprintf("https://github.com/%s/%s.git", p.config.OrgName, *repo.Name)
		if _, err := f.WriteString(fmt.Sprintf("%s - %s\n", *repo.Name, httpsURL)); err != nil {
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
				p.stats.deletedRepoNames = append(p.stats.deletedRepoNames, entry.Name())
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
			semaphore <- struct{}{}
			go func(repo *github.Repository, index int) {
				p.processRepository(&wg, repo, index, len(allRepos))
				<-semaphore
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

// runWithRetry executes a function with retry logic
func (p *Processor) runWithRetry(repoName string, operation string, fn func() error) error {
	for attempt := 1; attempt <= p.config.RetryCount; attempt++ {
		if err := fn(); err != nil {
			// Skip retries if the error is "remote repository is empty"
			if strings.Contains(err.Error(), "remote repository is empty") {
				p.stats.printMutex.Lock()
				p.stats.retriedFailure++
				p.stats.failedRepoNames = append(p.stats.failedRepoNames, repoName)
				p.stats.printMutex.Unlock()
				return fmt.Errorf("error %s %s: %w", operation, repoName, err)
			}

			if attempt == p.config.RetryCount {
				p.stats.printMutex.Lock()
				p.stats.retriedFailure++
				p.stats.failedRepoNames = append(p.stats.failedRepoNames, repoName)
				p.stats.printMutex.Unlock()
				return fmt.Errorf("error %s %s: %w", operation, repoName, err)
			}
			p.printMutex.Lock()
			fmt.Printf("Attempt %d/%d: Error %s %s: %v\nRetrying in 5 seconds...\n",
				attempt, p.config.RetryCount, operation, repoName, err)
			p.printMutex.Unlock()
			time.Sleep(5 * time.Second)
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

// openRepository opens a git repository and caches it
func (p *Processor) openRepository(repoPath string, repoName string) (*git.Repository, error) {
	p.repoMutex.Lock()
	defer p.repoMutex.Unlock()

	if repo, ok := p.repositories[repoName]; ok {
		return repo, nil
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("error opening repository: %w", err)
	}

	p.repositories[repoName] = repo
	return repo, nil
}

// getRepositoryHead gets the current branch and commit hash
func (p *Processor) getRepositoryHead(repo *git.Repository) (string, plumbing.Hash, error) {
	ref, err := repo.Head()
	if err != nil {
		return "", plumbing.ZeroHash, fmt.Errorf("error getting HEAD: %w", err)
	}

	var branchName string
	if ref.Name().IsBranch() {
		branchName = ref.Name().Short()
	} else {
		// Detached HEAD state
		branchName = "HEAD"
	}

	return branchName, ref.Hash(), nil
}

// updateRemoteURL updates the remote URL for a repository
func (p *Processor) updateRemoteURL(repo *git.Repository, repoName string, remoteURL string) error {
	remote, err := repo.Remote("origin")
	if err != nil {
		return fmt.Errorf("error getting remote: %w", err)
	}

	config := remote.Config()
	config.URLs = []string{remoteURL}

	if err := repo.DeleteRemote("origin"); err != nil {
		return fmt.Errorf("error deleting remote: %w", err)
	}

	if _, err := repo.CreateRemote(config); err != nil {
		return fmt.Errorf("error creating remote: %w", err)
	}

	return nil
}

// fetchRepository fetches updates from the remote
func (p *Processor) fetchRepository(repo *git.Repository, repoName string) error {
	return p.runWithRetry(repoName, "fetching updates for", func() error {
		err := repo.Fetch(&git.FetchOptions{
			RemoteName: "origin",
			Auth: &http.BasicAuth{
				Username: "anything_except_an_empty_string",
				Password: p.config.Token,
			},
			Force: true,
		})

		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return fmt.Errorf("error fetching: %w", err)
		}

		return nil
	})
}

// getRemoteHash gets the commit hash of a remote branch
func (p *Processor) getRemoteHash(repo *git.Repository, branchName string) (plumbing.Hash, error) {
	remoteBranchRef := plumbing.NewRemoteReferenceName("origin", branchName)
	remoteRef, err := repo.Reference(remoteBranchRef, true)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("error getting remote reference: %w", err)
	}

	return remoteRef.Hash(), nil
}

// pullRepository pulls updates from the remote
func (p *Processor) pullRepository(repo *git.Repository, repoName string, branchName string) error {
	return p.runWithRetry(repoName, "pulling updates for", func() error {
		w, err := repo.Worktree()
		if err != nil {
			return fmt.Errorf("error getting worktree: %w", err)
		}

		err = w.Pull(&git.PullOptions{
			RemoteName: "origin",
			Auth: &http.BasicAuth{
				Username: "anything_except_an_empty_string",
				Password: p.config.Token,
			},
		})

		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return fmt.Errorf("error pulling: %w", err)
		}

		return nil
	})
}

// getCommitHash gets the commit hash as a string
func (p *Processor) getCommitHash(repo *git.Repository) (string, error) {
	ref, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("error getting HEAD: %w", err)
	}

	return ref.Hash().String(), nil
}

func (p *Processor) processRepository(wg *sync.WaitGroup, repo *github.Repository, index, total int) {
	defer wg.Done()

	// p.printMutex.Lock()
	// fmt.Printf("[%d/%d] ", index+1, total)
	// p.printMutex.Unlock()

	repoPath := filepath.Join(p.config.OutputDir, *repo.Name)
	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", p.config.OrgName, *repo.Name)
	authURL := fmt.Sprintf("https://%s@github.com/%s/%s.git", p.config.Token, p.config.OrgName, *repo.Name)

	wasUpdated := false

	if _, err := os.Stat(repoPath); err == nil {
		// Repository exists, update it
		gitRepo, err := p.openRepository(repoPath, *repo.Name)
		if err != nil {
			fmt.Printf("Error opening repository %s: %v\n", *repo.Name, err)
			return
		}

		// Update remote URL
		if err := p.updateRemoteURL(gitRepo, *repo.Name, authURL); err != nil {
			fmt.Printf("Error updating remote URL for %s: %v\n", *repo.Name, err)
			return
		}

		// Get current branch and hash
		branchName, beforeHash, err := p.getRepositoryHead(gitRepo)
		if err != nil {
			fmt.Printf("Error getting current branch for %s: %v\n", *repo.Name, err)
			return
		}

		// Fetch updates
		fmt.Printf("Fetching updates for %s...\n", *repo.Name)
		if err := p.fetchRepository(gitRepo, *repo.Name); err != nil {
			fmt.Printf("Error fetching updates for %s: %v\n", *repo.Name, err)
			return
		}

		// Get remote hash
		remoteHash, err := p.getRemoteHash(gitRepo, branchName)
		if err != nil {
			fmt.Printf("Error getting remote hash for %s: %v\n", *repo.Name, err)
			return
		}

		// Check if there are changes
		if beforeHash != remoteHash {
			// Pull changes
			if err := p.pullRepository(gitRepo, *repo.Name, branchName); err != nil {
				fmt.Printf("Error pulling updates for %s: %v\n", *repo.Name, err)
				return
			}

			p.stats.printMutex.Lock()
			p.stats.updatedRepos++
			p.stats.updatedRepoNames = append(p.stats.updatedRepoNames, *repo.Name)
			p.stats.printMutex.Unlock()
			fmt.Printf("Updated %s from %s to %s\n", *repo.Name,
				beforeHash.String()[:8],
				remoteHash.String()[:8])
			wasUpdated = true
		} else {
			fmt.Printf("No changes in %s\n", *repo.Name)
		}
	} else if os.IsNotExist(err) {
		// Repository doesn't exist, clone it
		fmt.Printf("Cloning %s...\n", *repo.Name)

		var cloneErr error
		var attemptCount int
		for attemptCount = 1; attemptCount <= p.config.RetryCount; attemptCount++ {
			_, cloneErr = git.PlainClone(repoPath, false, &git.CloneOptions{
				URL: cloneURL,
				Auth: &http.BasicAuth{
					Username: "anything_except_an_empty_string",
					Password: p.config.Token,
				},
			})

			if cloneErr == nil {
				break
			}

			// Skip retries if the error is "remote repository is empty"
			if strings.Contains(cloneErr.Error(), "remote repository is empty") {
				fmt.Printf("Error cloning %s: %v (skipping retries for empty repository)\n", *repo.Name, cloneErr)
				p.stats.printMutex.Lock()
				p.stats.retriedFailure++
				p.stats.failedRepoNames = append(p.stats.failedRepoNames, *repo.Name)
				p.stats.printMutex.Unlock()
				return
			}

			if attemptCount < p.config.RetryCount {
				p.printMutex.Lock()
				fmt.Printf("Attempt %d/%d: Error cloning %s: %v\nRetrying in 5 seconds...\n",
					attemptCount, p.config.RetryCount, *repo.Name, cloneErr)
				p.printMutex.Unlock()
				time.Sleep(5 * time.Second)
			} else {
				fmt.Printf("Error cloning %s after %d attempts: %v\n", *repo.Name, p.config.RetryCount, cloneErr)
				p.stats.printMutex.Lock()
				p.stats.retriedFailure++
				p.stats.failedRepoNames = append(p.stats.failedRepoNames, *repo.Name)
				p.stats.printMutex.Unlock()
				return
			}
		}

		if attemptCount > 1 && cloneErr == nil {
			p.stats.printMutex.Lock()
			p.stats.cloneRetrySuccess++
			p.stats.printMutex.Unlock()
		}

		p.stats.printMutex.Lock()
		p.stats.newRepos++
		p.stats.newRepoNames = append(p.stats.newRepoNames, *repo.Name)
		p.stats.printMutex.Unlock()
		wasUpdated = true
	} else {
		fmt.Printf("Error checking directory %s: %v\n", repoPath, err)
		return
	}

	if p.config.PostSyncCommand != "" && wasUpdated {
		fmt.Printf("Executing post-sync command '%s' for %s...\n", p.config.PostSyncCommand, *repo.Name)
		cmd := exec.Command("sh", "-c", p.config.PostSyncCommand)
		cmd.Dir = repoPath
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("Error executing post-sync command for %s: %v\nOutput: %s\n", *repo.Name, err, output)
		} else {
			fmt.Printf("Post-sync command completed successfully for %s\nOutput: %s\n", *repo.Name, output)
		}
	}
}

func (p *Processor) printSummary() {
	fmt.Printf("\nSummary:\n")

	fmt.Printf("- New repositories cloned (%d):\n", p.stats.newRepos)
	for _, name := range p.stats.newRepoNames {
		repoPath := filepath.Join(p.config.OutputDir, name)
		gitRepo, err := p.openRepository(repoPath, name)
		if err != nil {
			fmt.Printf("  • %s (error opening repository: %v)\n", name, err)
			continue
		}

		hash, err := p.getCommitHash(gitRepo)
		if err != nil {
			fmt.Printf("  • %s (error getting hash: %v)\n", name, err)
			continue
		}
		fmt.Printf("  • %s (%s)\n", name, hash)
	}

	fmt.Printf("\n- Existing repositories updated (%d):\n", p.stats.updatedRepos)
	for _, name := range p.stats.updatedRepoNames {
		repoPath := filepath.Join(p.config.OutputDir, name)
		gitRepo, err := p.openRepository(repoPath, name)
		if err != nil {
			fmt.Printf("  • %s (error opening repository: %v)\n", name, err)
			continue
		}

		hash, err := p.getCommitHash(gitRepo)
		if err != nil {
			fmt.Printf("  • %s (error getting hash: %v)\n", name, err)
			continue
		}
		fmt.Printf("  • %s (%s)\n", name, hash)
	}

	fmt.Printf("\n- Repositories deleted (%d):\n", p.stats.deletedRepos)
	for _, name := range p.stats.deletedRepoNames {
		fmt.Printf("  • %s (deleted)\n", name)
	}

	fmt.Printf("\n- Repositories skipped (%d):\n", p.stats.skippedRepos)
	for _, name := range p.stats.skippedRepoNames {
		repoPath := filepath.Join(p.config.OutputDir, name)
		gitRepo, err := p.openRepository(repoPath, name)
		if err != nil {
			fmt.Printf("  • %s (error opening repository: %v)\n", name, err)
			continue
		}

		hash, err := p.getCommitHash(gitRepo)
		if err != nil {
			fmt.Printf("  • %s (error getting hash: %v)\n", name, err)
			continue
		}
		fmt.Printf("  • %s (%s)\n", name, hash)
	}

	fmt.Printf("\n- Operations failed despite retries (%d):\n", p.stats.retriedFailure)
	for _, name := range p.stats.failedRepoNames {
		repoPath := filepath.Join(p.config.OutputDir, name)
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			fmt.Printf("  • %s (repository does not exist)\n", name)
			continue
		}

		gitRepo, err := p.openRepository(repoPath, name)
		if err != nil {
			fmt.Printf("  • %s (error opening repository: %v)\n", name, err)
			continue
		}

		hash, err := p.getCommitHash(gitRepo)
		if err != nil {
			fmt.Printf("  • %s (error getting hash: %v)\n", name, err)
			continue
		}
		fmt.Printf("  • %s (%s)\n", name, hash)
	}

	fmt.Printf("\n- Clone operations succeeded after retries: %d\n", p.stats.cloneRetrySuccess)
	fmt.Printf("- Fetch operations succeeded after retries: %d\n", p.stats.fetchRetrySuccess)

	fmt.Printf("\nTotal time taken: %s\n", time.Since(p.stats.startTime))
}

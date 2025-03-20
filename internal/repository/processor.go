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
	repositories map[string]*git.Repository
	repoMutex    sync.Mutex
}

func NewProcessor(client *github.Client, cfg *config.Config) *Processor {
	return &Processor{
		client:       client,
		config:       cfg,
		repositories: make(map[string]*git.Repository),
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

	return nil
}

func (p *Processor) listRepositories(ctx context.Context) ([]*github.Repository, error) {
	opt := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	firstPageRepos, resp, err := p.client.Repositories.ListByOrg(ctx, p.config.OrgName, opt)
	if err != nil {
		return nil, fmt.Errorf("error fetching first page of repositories: %w", err)
	}

	if resp.NextPage == 0 {
		return firstPageRepos, nil
	}

	totalPages := resp.LastPage
	if totalPages == 0 {
		totalPages = resp.NextPage
	}

	fmt.Printf("Found %d pages of repositories, fetching concurrently with %d workers...\n",
		totalPages, p.config.Workers)

	type pageResult struct {
		page  int
		repos []*github.Repository
		err   error
	}
	resultChan := make(chan pageResult, totalPages)

	resultChan <- pageResult{page: 1, repos: firstPageRepos}

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, p.config.Workers)

	for page := 2; page <= totalPages; page++ {
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

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var allRepos []*github.Repository
	allRepos = append(allRepos, firstPageRepos...)

	pageMap := make(map[int][]*github.Repository)

	for result := range resultChan {
		if result.err != nil {
			if errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
				return nil, result.err
			}
			return nil, fmt.Errorf("error fetching page %d: %w", result.page, result.err)
		}

		pageMap[result.page] = result.repos
	}

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

func (p *Processor) runWithRetry(repoName string, operation string, fn func() error) error {
	for attempt := 1; attempt <= p.config.RetryCount; attempt++ {
		if err := fn(); err != nil {
			if strings.Contains(err.Error(), "remote repository is empty") {
				return fmt.Errorf("error %s %s: %w", operation, repoName, err)
			}

			if attempt == p.config.RetryCount {
				return fmt.Errorf("error %s %s: %w", operation, repoName, err)
			}
			p.printMutex.Lock()
			fmt.Printf("Attempt %d/%d: Error %s %s: %v\nRetrying in 5 seconds...\n",
				attempt, p.config.RetryCount, operation, repoName, err)
			p.printMutex.Unlock()
			time.Sleep(5 * time.Second)
			continue
		}
		return nil
	}
	return nil
}

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

func (p *Processor) getRepositoryHead(repo *git.Repository) (string, plumbing.Hash, error) {
	ref, err := repo.Head()
	if err != nil {
		return "", plumbing.ZeroHash, fmt.Errorf("error getting HEAD: %w", err)
	}

	var branchName string
	if ref.Name().IsBranch() {
		branchName = ref.Name().Short()
	} else {
		branchName = "HEAD"
	}

	return branchName, ref.Hash(), nil
}

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

func (p *Processor) getRemoteHash(repo *git.Repository, branchName string) (plumbing.Hash, error) {
	remoteBranchRef := plumbing.NewRemoteReferenceName("origin", branchName)
	remoteRef, err := repo.Reference(remoteBranchRef, true)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("error getting remote reference: %w", err)
	}

	return remoteRef.Hash(), nil
}

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

func (p *Processor) getCommitHash(repo *git.Repository) (string, error) {
	ref, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("error getting HEAD: %w", err)
	}

	return ref.Hash().String(), nil
}

func (p *Processor) processRepository(wg *sync.WaitGroup, repo *github.Repository, index, total int) {
	defer wg.Done()

	repoPath := filepath.Join(p.config.OutputDir, *repo.Name)
	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", p.config.OrgName, *repo.Name)
	authURL := fmt.Sprintf("https://%s@github.com/%s/%s.git", p.config.Token, p.config.OrgName, *repo.Name)

	wasUpdated := false

	if _, err := os.Stat(repoPath); err == nil {
		gitRepo, err := p.openRepository(repoPath, *repo.Name)
		if err != nil {
			fmt.Printf("Error opening repository %s: %v\n", *repo.Name, err)
			return
		}

		if err := p.updateRemoteURL(gitRepo, *repo.Name, authURL); err != nil {
			fmt.Printf("Error updating remote URL for %s: %v\n", *repo.Name, err)
			return
		}

		branchName, beforeHash, err := p.getRepositoryHead(gitRepo)
		if err != nil {
			fmt.Printf("Error getting current branch for %s: %v\n", *repo.Name, err)
			return
		}

		// fmt.Printf("Fetching updates for %s...\n", *repo.Name)
		if err := p.fetchRepository(gitRepo, *repo.Name); err != nil {
			fmt.Printf("Error fetching updates for %s: %v\n", *repo.Name, err)
			return
		}

		remoteHash, err := p.getRemoteHash(gitRepo, branchName)
		if err != nil {
			fmt.Printf("Error getting remote hash for %s: %v\n", *repo.Name, err)
			return
		}

		if beforeHash != remoteHash {
			if err := p.pullRepository(gitRepo, *repo.Name, branchName); err != nil {
				fmt.Printf("Error pulling updates for %s: %v\n", *repo.Name, err)
				return
			}

			fmt.Printf("Updated %s from %s to %s\n", *repo.Name,
				beforeHash.String()[:8],
				remoteHash.String()[:8])
			wasUpdated = true
		} else {
			fmt.Printf("No changes in %s\n", *repo.Name)
		}
	} else if os.IsNotExist(err) {
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
				fmt.Printf("Cloned %s\n", *repo.Name)
				break
			}

			if strings.Contains(cloneErr.Error(), "remote repository is empty") {
				fmt.Printf("Error cloning %s: %v (skipping retries for empty repository)\n", *repo.Name, cloneErr)
				return
			}

			if strings.Contains(cloneErr.Error(), "repository not found") ||
				strings.Contains(cloneErr.Error(), "not found") ||
				strings.Contains(cloneErr.Error(), "does not exist") {
				fmt.Printf("Error cloning %s: %v (repository doesn't exist)\n", *repo.Name, cloneErr)
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
				return
			}
		}
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

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

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

type Processor struct {
	client     *github.Client
	config     *config.Config
	printMutex sync.Mutex
	stats      *ProcessorStats
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
		client: client,
		config: cfg,
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

func (p *Processor) processRepository(wg *sync.WaitGroup, repo *github.Repository, index, total int) {
	defer wg.Done()

	p.printMutex.Lock()
	fmt.Printf("[%d/%d] ", index+1, total)
	p.printMutex.Unlock()

	repoPath := filepath.Join(p.config.OutputDir, *repo.Name)
	// cloneURL := fmt.Sprintf("https://%s@github.com/%s/%s.git", p.config.Token, p.config.OrgName, *repo.Name)
	cloneURLsmall := fmt.Sprintf("https://github.com/%s/%s.git", p.config.OrgName, *repo.Name)

	wasUpdated := false

	if _, err := os.Stat(repoPath); err == nil {
		remoteURL := fmt.Sprintf("https://%s@github.com/%s/%s.git", p.config.Token, p.config.OrgName, *repo.Name)
		setURLCmd := exec.Command("git", "-C", repoPath, "remote", "set-url", "origin", remoteURL)
		if err := p.runCommandWithRetry(setURLCmd, *repo.Name, "updating remote URL for"); err != nil {
			fmt.Printf("Error updating remote URL for %s: %v\n", *repo.Name, err)
			return
		}

		fetchCmd := exec.Command("git", "-C", repoPath, "fetch", "--all")
		if err := p.runCommandWithRetry(fetchCmd, *repo.Name, "fetching updates for"); err != nil {
			fmt.Printf("Error fetching updates for %s: %v\n", *repo.Name, err)
			return
		}

		branchCmd := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
		branch, err := branchCmd.Output()
		if err != nil {
			fmt.Printf("Error getting current branch for %s: %v\n", *repo.Name, err)
			return
		}
		branchName := strings.TrimSpace(string(branch))

		beforeCmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
		beforeHash, err := beforeCmd.Output()
		if err != nil {
			fmt.Printf("Error getting current hash for %s: %v\n", *repo.Name, err)
			return
		}

		fmt.Printf("Fetching updates for %s...\n", *repo.Name)
		fetchCmd = exec.Command("git", "-C", repoPath, "fetch", "origin", branchName)
		if err := p.runCommandWithRetry(fetchCmd, *repo.Name, "fetching updates for"); err != nil {
			fmt.Printf("Error fetching updates for %s: %v\n", *repo.Name, err)
			return
		}

		remoteCmd := exec.Command("git", "-C", repoPath, "rev-parse", fmt.Sprintf("origin/%s", branchName))
		remoteHash, err := remoteCmd.Output()
		if err != nil {
			fmt.Printf("Error getting remote hash for %s: %v\n", *repo.Name, err)
			return
		}

		if string(beforeHash) != string(remoteHash) {
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
		fmt.Printf("Cloning %s...\n", *repo.Name)
		// cmd := exec.Command("git", "clone", cloneURL, repoPath)
		// if err := p.runCommandWithRetry(cmd, *repo.Name, "cloning"); err != nil {
		// 	fmt.Printf("Error cloning %s: %v\n", *repo.Name, err)
		// 	return
		// }

		_, err := git.PlainClone(repoPath, false, &git.CloneOptions{
			URL: cloneURLsmall,
			Auth: &http.BasicAuth{
				Username: "anything_except_an_empty_string",
				Password: p.config.Token,
			},
		})

		if err != nil {
			fmt.Printf("Error cloning %s: %v\n", *repo.Name, err)
			return
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

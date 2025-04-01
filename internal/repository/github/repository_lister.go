package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/truemilk/ghloner/internal/config"
)

// RepositoryLister handles listing repositories from GitHub
type RepositoryLister struct {
	client *github.Client
	config *config.Config
}

// NewRepositoryLister creates a new repository lister
func NewRepositoryLister(client *github.Client, cfg *config.Config) *RepositoryLister {
	return &RepositoryLister{
		client: client,
		config: cfg,
	}
}

// ListRepositories fetches all repositories for an organization
func (l *RepositoryLister) ListRepositories(ctx context.Context, orgName string) ([]*github.Repository, error) {
	startTime := time.Now()

	// Fetch first page
	firstPageRepos, resp, err := l.fetchFirstPage(ctx, orgName)
	if err != nil {
		return nil, err
	}

	if resp.NextPage == 0 {
		return firstPageRepos, nil
	}

	// Fetch remaining pages concurrently
	allRepos, err := l.fetchRemainingPages(ctx, orgName, firstPageRepos, resp)
	if err != nil {
		return nil, err
	}

	endTime := time.Now()
	elapsedTime := endTime.Sub(startTime)
	slog.Info("Successfully fetched all pages of repositories", 
		"pages", resp.LastPage, 
		"elapsed_time", elapsedTime)

	return allRepos, nil
}

// fetchFirstPage fetches the first page of repositories
func (l *RepositoryLister) fetchFirstPage(ctx context.Context, orgName string) ([]*github.Repository, *github.Response, error) {
	opt := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	repos, resp, err := l.client.Repositories.ListByOrg(ctx, orgName, opt)
	if err != nil {
		return nil, nil, fmt.Errorf("error fetching first page of repositories: %w", err)
	}

	return repos, resp, nil
}

// fetchRemainingPages fetches the remaining pages concurrently
func (l *RepositoryLister) fetchRemainingPages(
	ctx context.Context, 
	orgName string, 
	firstPageRepos []*github.Repository, 
	resp *github.Response,
) ([]*github.Repository, error) {
	totalPages := resp.LastPage
	if totalPages == 0 {
		totalPages = resp.NextPage
	}

	slog.Info("Found multiple pages of repositories", "pages", totalPages, "workers", l.config.Workers)

	type pageResult struct {
		page  int
		repos []*github.Repository
		err   error
	}
	resultChan := make(chan pageResult, totalPages)
	resultChan <- pageResult{page: 1, repos: firstPageRepos}

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, l.config.Workers)

	// Start workers for remaining pages
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

			repos, _, err := l.client.Repositories.ListByOrg(ctx, orgName, pageOpt)
			if err != nil {
				resultChan <- pageResult{page: pageNum, err: err}
				return
			}

			slog.Debug("Fetched page of repositories", "page", pageNum)
			resultChan <- pageResult{page: pageNum, repos: repos}
		}(page)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
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

	// Ensure pages are added in order
	for page := 2; page <= totalPages; page++ {
		if repos, ok := pageMap[page]; ok {
			allRepos = append(allRepos, repos...)
		}
	}

	return allRepos, nil
}
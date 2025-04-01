package git

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Operations provides common Git operations
type Operations struct {
	retryCount int
}

// NewOperations creates a new Git operations instance
func NewOperations(retryCount int) *Operations {
	return &Operations{
		retryCount: retryCount,
	}
}

// RunWithRetry executes a Git operation with retry logic
func (o *Operations) RunWithRetry(repoName string, operation string, fn func() error) error {
	for attempt := 1; attempt <= o.retryCount; attempt++ {
		if err := fn(); err != nil {
			if strings.Contains(err.Error(), "remote repository is empty") {
				return fmt.Errorf("error %s %s: %w", operation, repoName, err)
			}

			if attempt == o.retryCount {
				return fmt.Errorf("error %s %s: %w", operation, repoName, err)
			}
			
			slog.Warn("Operation failed, retrying",
				"attempt", attempt,
				"maxAttempts", o.retryCount,
				"operation", operation,
				"repository", repoName,
				"error", err)
			
			time.Sleep(5 * time.Second)
			continue
		}
		return nil
	}
	return nil
}

// UpdateRemoteURL updates the remote URL for a repository
func (o *Operations) UpdateRemoteURL(repo *git.Repository, remoteURL string) error {
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

// GetRepositoryHead gets the current HEAD reference of a repository
func (o *Operations) GetRepositoryHead(repo *git.Repository) (string, plumbing.Hash, error) {
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

// FetchRepository fetches updates from the remote repository
func (o *Operations) FetchRepository(repo *git.Repository, repoName, token string) error {
	return o.RunWithRetry(repoName, "fetching updates for", func() error {
		err := repo.Fetch(&git.FetchOptions{
			RemoteName: "origin",
			Auth: &http.BasicAuth{
				Username: "anything_except_an_empty_string",
				Password: token,
			},
			Force: true,
		})

		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return fmt.Errorf("error fetching: %w", err)
		}

		return nil
	})
}

// GetRemoteHash gets the hash of the remote branch
func (o *Operations) GetRemoteHash(repo *git.Repository, branchName string) (plumbing.Hash, error) {
	remoteBranchRef := plumbing.NewRemoteReferenceName("origin", branchName)
	remoteRef, err := repo.Reference(remoteBranchRef, true)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("error getting remote reference: %w", err)
	}

	return remoteRef.Hash(), nil
}

// PullRepository pulls updates from the remote repository
func (o *Operations) PullRepository(repo *git.Repository, repoName, token string) error {
	return o.RunWithRetry(repoName, "pulling updates for", func() error {
		w, err := repo.Worktree()
		if err != nil {
			return fmt.Errorf("error getting worktree: %w", err)
		}

		err = w.Pull(&git.PullOptions{
			RemoteName: "origin",
			Auth: &http.BasicAuth{
				Username: "anything_except_an_empty_string",
				Password: token,
			},
		})

		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return fmt.Errorf("error pulling: %w", err)
		}

		return nil
	})
}

// CloneRepository clones a repository
func (o *Operations) CloneRepository(repoPath, cloneURL, repoName, token string) error {
	_, err := git.PlainClone(repoPath, false, &git.CloneOptions{
		URL: cloneURL,
		Auth: &http.BasicAuth{
			Username: "anything_except_an_empty_string",
			Password: token,
		},
	})
	
	if err != nil {
		if strings.Contains(err.Error(), "remote repository is empty") {
			return fmt.Errorf("repository is empty: %w", err)
		}
		return fmt.Errorf("error cloning repository: %w", err)
	}
	
	return nil
}
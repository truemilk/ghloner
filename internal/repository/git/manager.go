package git

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/go-git/go-git/v5"
	"github.com/truemilk/ghloner/internal/config"
)

// Manager handles Git repository operations
type Manager struct {
	config       *config.Config
	operations   *Operations
	repositories map[string]*git.Repository
	repoMutex    sync.Mutex
	printMutex   sync.Mutex
}

// NewManager creates a new Git manager
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		config:       cfg,
		operations:   NewOperations(cfg.RetryCount),
		repositories: make(map[string]*git.Repository),
	}
}

// ProcessRepository processes a single repository (clone or update)
func (m *Manager) ProcessRepository(repo *github.Repository, orgName, token string) error {
	repoPath := filepath.Join(m.config.OutputDir, *repo.Name)
	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", orgName, *repo.Name)
	authURL := fmt.Sprintf("https://%s@github.com/%s/%s.git", token, orgName, *repo.Name)

	if _, err := os.Stat(repoPath); err == nil {
		return m.updateRepository(repoPath, *repo.Name, authURL, token)
	} else if os.IsNotExist(err) {
		return m.cloneRepository(repoPath, cloneURL, *repo.Name, token)
	} else {
		slog.Error("Failed to check directory", "path", repoPath, "error", err)
		return err
	}
}

// updateRepository updates an existing repository
func (m *Manager) updateRepository(repoPath, repoName, authURL, token string) error {
	gitRepo, err := m.openRepository(repoPath, repoName)
	if err != nil {
		slog.Error("Failed to open repository", "repository", repoName, "error", err)
		return err
	}

	if err := m.operations.UpdateRemoteURL(gitRepo, authURL); err != nil {
		slog.Error("Failed to update remote URL", "repository", repoName, "error", err)
		return err
	}

	branchName, beforeHash, err := m.operations.GetRepositoryHead(gitRepo)
	if err != nil {
		slog.Error("Failed to get current branch", "repository", repoName, "error", err)
		return err
	}

	if err := m.operations.FetchRepository(gitRepo, repoName, token); err != nil {
		slog.Error("Failed to fetch updates", "repository", repoName, "error", err)
		return err
	}

	remoteHash, err := m.operations.GetRemoteHash(gitRepo, branchName)
	if err != nil {
		slog.Error("Failed to get remote hash", "repository", repoName, "error", err)
		return err
	}

	if beforeHash != remoteHash {
		startTime := time.Now()
		if err := m.operations.PullRepository(gitRepo, repoName, token); err != nil {
			slog.Error("Failed to pull updates", "repository", repoName, "error", err)
			return err
		}
		endTime := time.Now()

		slog.Info("Updated repository",
			"repository", repoName,
			"from", beforeHash.String()[:8],
			"to", remoteHash.String()[:8],
			"elapsed_time", endTime.Sub(startTime))
	}

	return nil
}

// cloneRepository clones a new repository
func (m *Manager) cloneRepository(repoPath, cloneURL, repoName, token string) error {
	var cloneErr error
	var attemptCount int
	
	for attemptCount = 1; attemptCount <= m.config.RetryCount; attemptCount++ {
		startTime := time.Now()
		
		cloneErr = m.operations.CloneRepository(repoPath, cloneURL, repoName, token)
		
		endTime := time.Now()

		if cloneErr == nil {
			slog.Info("Cloned repository", "repository", repoName, "elapsed_time", endTime.Sub(startTime))
			break
		}

		if strings.Contains(cloneErr.Error(), "repository is empty") {
			slog.Warn("Did not clone repository (empty)", "repository", repoName, "error", cloneErr)
			return nil
		}

		if strings.Contains(cloneErr.Error(), "repository not found") ||
			strings.Contains(cloneErr.Error(), "not found") ||
			strings.Contains(cloneErr.Error(), "does not exist") {
			slog.Error("Failed to clone repository (not found)", "repository", repoName, "error", cloneErr)
			return cloneErr
		}

		if attemptCount < m.config.RetryCount {
			m.printMutex.Lock()
			slog.Warn("Clone attempt failed, retrying",
				"attempt", attemptCount,
				"maxAttempts", m.config.RetryCount,
				"repository", repoName,
				"error", cloneErr)
			m.printMutex.Unlock()
			time.Sleep(5 * time.Second)
		} else {
			slog.Error("Failed to clone repository after multiple attempts",
				"repository", repoName,
				"attempts", m.config.RetryCount,
				"error", cloneErr)
			return cloneErr
		}
	}
	
	return nil
}

// openRepository opens a Git repository and caches it
func (m *Manager) openRepository(repoPath string, repoName string) (*git.Repository, error) {
	m.repoMutex.Lock()
	defer m.repoMutex.Unlock()

	if repo, ok := m.repositories[repoName]; ok {
		return repo, nil
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("error opening repository: %w", err)
	}

	m.repositories[repoName] = repo
	return repo, nil
}
package storage

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/go-github/v60/github"
	"github.com/truemilk/ghloner/internal/config"
)

// FileManager handles file system operations
type FileManager struct {
	config *config.Config
}

// NewFileManager creates a new file manager
func NewFileManager(cfg *config.Config) *FileManager {
	return &FileManager{
		config: cfg,
	}
}

// SaveRepositoryList saves the list of repositories to a file
func (f *FileManager) SaveRepositoryList(allRepos []*github.Repository, orgName string) error {
	repoListPath := filepath.Join(f.config.OutputDir, "repository_list.txt")
	file, err := os.Create(repoListPath)
	if err != nil {
		return fmt.Errorf("error creating repository list file: %w", err)
	}
	defer file.Close()

	for _, repo := range allRepos {
		httpsURL := fmt.Sprintf("https://github.com/%s/%s.git", orgName, *repo.Name)
		if _, err := file.WriteString(fmt.Sprintf("%s - %s\n", *repo.Name, httpsURL)); err != nil {
			return fmt.Errorf("error writing to repository list file: %w", err)
		}
	}

	slog.Info("Repository list saved", "path", repoListPath)
	return nil
}

// CleanupOldRepositories removes repositories that no longer exist
func (f *FileManager) CleanupOldRepositories(allRepos []*github.Repository) error {
	validRepos := make(map[string]bool)
	for _, repo := range allRepos {
		validRepos[*repo.Name] = true
	}

	entries, err := os.ReadDir(f.config.OutputDir)
	if err != nil {
		return fmt.Errorf("error reading output directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != ".git" {
			if !validRepos[entry.Name()] {
				fullPath := filepath.Join(f.config.OutputDir, entry.Name())
				slog.Info("Removing repository", "name", entry.Name(), "reason", "no longer exists in organization")
				if err := os.RemoveAll(fullPath); err != nil {
					return fmt.Errorf("error removing directory %s: %w", fullPath, err)
				}
			}
		}
	}

	return nil
}
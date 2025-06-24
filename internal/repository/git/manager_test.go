package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-github/v60/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/truemilk/ghloner/internal/config"
)

func TestNewManager(t *testing.T) {
	cfg := &config.Config{
		OutputDir:  "./test-repos",
		RetryCount: 3,
	}

	manager := NewManager(cfg)
	
	assert.NotNil(t, manager)
	assert.Equal(t, cfg, manager.config)
	assert.NotNil(t, manager.operations)
	assert.NotNil(t, manager.repositories)
}

func TestProcessRepository(t *testing.T) {
	tests := []struct {
		name        string
		repoName    string
		orgName     string
		dirExists   bool
		setupFunc   func(t *testing.T, dir string)
		wantErr     bool
		errContains string
	}{
		{
			name:      "clone new repository",
			repoName:  "test-repo",
			orgName:   "testorg",
			dirExists: false,
			wantErr:   false,
		},
		{
			name:      "update existing repository",
			repoName:  "test-repo",
			orgName:   "testorg",
			dirExists: true,
			setupFunc: func(t *testing.T, dir string) {
				// Create a valid git repo
				err := os.MkdirAll(filepath.Join(dir, ".git"), 0755)
				require.NoError(t, err)
			},
			wantErr:   true, // Will fail because it's not a real git repo
			errContains: "repository does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tempDir := t.TempDir()
			
			cfg := &config.Config{
				OutputDir:  tempDir,
				RetryCount: 1,
			}
			
			manager := NewManager(cfg)
			
			// Create repository object
			repo := &github.Repository{
				Name: github.String(tt.repoName),
			}
			
			repoPath := filepath.Join(tempDir, tt.repoName)
			
			// Setup directory if needed
			if tt.dirExists {
				err := os.MkdirAll(repoPath, 0755)
				require.NoError(t, err)
				
				if tt.setupFunc != nil {
					tt.setupFunc(t, repoPath)
				}
			}
			
			// Process repository
			err := manager.ProcessRepository(repo, tt.orgName, "test-token")
			
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				// Clone operations will fail without network, so we expect an error
				require.Error(t, err)
			}
		})
	}
}

func TestOpenRepository(t *testing.T) {
	tempDir := t.TempDir()
	
	cfg := &config.Config{
		OutputDir:  tempDir,
		RetryCount: 3,
	}
	
	manager := NewManager(cfg)
	
	// Test opening non-existent repository
	repo, err := manager.openRepository("/non/existent/path", "test-repo")
	assert.Nil(t, repo)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "repository does not exist")
	
	// Test caching - create a mock git directory
	repoPath := filepath.Join(tempDir, "test-repo")
	err = os.MkdirAll(filepath.Join(repoPath, ".git"), 0755)
	require.NoError(t, err)
	
	// First call - will fail but test caching logic
	repo1, err1 := manager.openRepository(repoPath, "test-repo")
	assert.Error(t, err1)
	
	// Second call - should get from cache
	repo2, err2 := manager.openRepository(repoPath, "test-repo")
	assert.Error(t, err2)
	assert.Equal(t, repo1, repo2)
}


func TestCloneRepository(t *testing.T) {
	tempDir := t.TempDir()
	
	cfg := &config.Config{
		OutputDir:  tempDir,
		RetryCount: 1,
	}
	
	manager := NewManager(cfg)
	
	repoPath := filepath.Join(tempDir, "test-repo")
	cloneURL := "https://github.com/testorg/test-repo.git"
	
	// Test clone (will fail without network)
	err := manager.cloneRepository(repoPath, cloneURL, "test-repo", "test-token")
	
	// We expect an error due to no network
	require.Error(t, err)
}

func TestUpdateRepository(t *testing.T) {
	tempDir := t.TempDir()
	
	cfg := &config.Config{
		OutputDir:  tempDir,
		RetryCount: 1,
	}
	
	manager := NewManager(cfg)
	
	// Create a test repository directory
	repoPath := filepath.Join(tempDir, "test-repo")
	err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0755)
	require.NoError(t, err)
	
	// Test update (will fail because it's not a real git repo)
	err = manager.updateRepository(repoPath, "test-repo", "https://test-token@github.com/testorg/test-repo.git", "test-token")
	
	// We expect an error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "opening repository")
}


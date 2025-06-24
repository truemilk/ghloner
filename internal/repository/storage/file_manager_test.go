package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/truemilk/ghloner/internal/config"
	"github.com/truemilk/ghloner/test/fixtures"
)

func TestNewFileManager(t *testing.T) {
	cfg := &config.Config{
		OutputDir: "/test/output",
	}

	fm := NewFileManager(cfg)

	assert.NotNil(t, fm)
	assert.Equal(t, cfg, fm.config)
}

func TestSaveRepositoryList(t *testing.T) {
	tests := []struct {
		name      string
		repoCount int
		orgName   string
		wantErr   bool
	}{
		{
			name:      "save empty list",
			repoCount: 0,
			orgName:   "testorg",
			wantErr:   false,
		},
		{
			name:      "save single repository",
			repoCount: 1,
			orgName:   "testorg",
			wantErr:   false,
		},
		{
			name:      "save multiple repositories",
			repoCount: 5,
			orgName:   "testorg",
			wantErr:   false,
		},
		{
			name:      "save with special characters in org name",
			repoCount: 2,
			orgName:   "test-org_123",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tempDir := t.TempDir()
			cfg := &config.Config{
				OutputDir: tempDir,
			}
			fm := NewFileManager(cfg)

			// Create test repositories
			repos := fixtures.CreateTestRepositories(tt.repoCount)

			// Save repository list
			err := fm.SaveRepositoryList(repos, tt.orgName)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				// Verify file was created
				expectedFile := filepath.Join(tempDir, "repository_list.txt")
				assert.FileExists(t, expectedFile)

				// Verify file contents
				data, err := os.ReadFile(expectedFile)
				require.NoError(t, err)

				// Count lines (each repo is one line)
				lines := string(data)
				if tt.repoCount == 0 {
					assert.Empty(t, lines)
				} else {
					// Split by newline and count non-empty lines
					lineCount := 0
					for _, line := range strings.Split(lines, "\n") {
						if strings.TrimSpace(line) != "" {
							lineCount++
							// Verify line format
							assert.Contains(t, line, " - https://github.com/"+tt.orgName+"/")
						}
					}
					assert.Equal(t, tt.repoCount, lineCount)
				}
			}
		})
	}
}

func TestSaveRepositoryList_FilePermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Cannot test permission errors as root")
	}

	// Create temp directory
	tempDir := t.TempDir()
	
	// Make directory read-only
	err := os.Chmod(tempDir, 0444)
	require.NoError(t, err)
	defer os.Chmod(tempDir, 0755) // Restore permissions for cleanup

	cfg := &config.Config{
		OutputDir: tempDir,
	}
	fm := NewFileManager(cfg)

	repos := fixtures.CreateTestRepositories(1)

	// Should fail due to permission error
	err = fm.SaveRepositoryList(repos, "testorg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error creating repository list file")
}

func TestCleanupOldRepositories(t *testing.T) {
	// Create temp directory structure
	tempDir := t.TempDir()
	cfg := &config.Config{
		OutputDir: tempDir,
	}
	fm := NewFileManager(cfg)

	// Create some test directories
	currentRepos := []string{"repo1", "repo2", "repo3"}
	oldRepos := []string{"old-repo1", "old-repo2"}
	
	// Create current repo directories
	for _, repo := range currentRepos {
		err := os.MkdirAll(filepath.Join(tempDir, repo), 0755)
		require.NoError(t, err)
	}
	
	// Create old repo directories
	for _, repo := range oldRepos {
		err := os.MkdirAll(filepath.Join(tempDir, repo), 0755)
		require.NoError(t, err)
		
		// Create a file in the old repo
		testFile := filepath.Join(tempDir, repo, "test.txt")
		err = os.WriteFile(testFile, []byte("test"), 0644)
		require.NoError(t, err)
	}
	
	// Create non-directory file (should be ignored)
	err := os.WriteFile(filepath.Join(tempDir, "README.md"), []byte("readme"), 0644)
	require.NoError(t, err)

	// Create repository objects for current repos
	repos := make([]*github.Repository, len(currentRepos))
	for i, name := range currentRepos {
		repos[i] = &github.Repository{
			Name: github.String(name),
		}
	}

	// Cleanup old repositories
	err = fm.CleanupOldRepositories(repos)
	require.NoError(t, err)
	
	// Verify current repos still exist
	for _, repo := range currentRepos {
		assert.DirExists(t, filepath.Join(tempDir, repo))
	}
	
	// Verify old repos were deleted
	for _, repo := range oldRepos {
		assert.NoDirExists(t, filepath.Join(tempDir, repo))
	}
	
	// Verify non-directory file still exists
	assert.FileExists(t, filepath.Join(tempDir, "README.md"))
}

func TestCleanupOldRepositories_EmptyDirectory(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		OutputDir: tempDir,
	}
	fm := NewFileManager(cfg)

	// No repositories to keep
	repos := []*github.Repository{}

	// Should not crash on empty directory
	err := fm.CleanupOldRepositories(repos)
	assert.NoError(t, err)
}

func TestCleanupOldRepositories_PermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Cannot test permission errors as root")
	}

	tempDir := t.TempDir()
	cfg := &config.Config{
		OutputDir: tempDir,
	}
	fm := NewFileManager(cfg)

	// Create a directory to delete
	oldRepo := filepath.Join(tempDir, "old-repo")
	err := os.MkdirAll(oldRepo, 0755)
	require.NoError(t, err)
	
	// Create a file in it
	testFile := filepath.Join(oldRepo, "test.txt")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)
	
	// Make the parent directory read-only to prevent deletion
	err = os.Chmod(tempDir, 0555)
	require.NoError(t, err)
	defer os.Chmod(tempDir, 0755) // Restore permissions

	// Try to cleanup (should get error)
	repos := []*github.Repository{}
	err = fm.CleanupOldRepositories(repos)
	
	// Should get permission error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestSaveRepositoryList_TextFormat(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		OutputDir: tempDir,
	}
	fm := NewFileManager(cfg)

	// Create repos with various field types
	repos := []*github.Repository{
		{
			ID:          github.Int64(123),
			Name:        github.String("test-repo"),
			FullName:    github.String("org/test-repo"),
			Description: github.String("A test repository"),
			Private:     github.Bool(false),
			Fork:        github.Bool(false),
			CreatedAt:   &github.Timestamp{Time: time.Now()},
			UpdatedAt:   &github.Timestamp{Time: time.Now()},
			Size:        github.Int(1024),
			Language:    github.String("Go"),
			HasIssues:   github.Bool(true),
		},
		{
			ID:       github.Int64(456),
			Name:     github.String("another-repo"),
			FullName: github.String("org/another-repo"),
			Private:  github.Bool(true),
			Fork:     github.Bool(true),
		},
	}

	// Save and verify
	err := fm.SaveRepositoryList(repos, "testorg")
	require.NoError(t, err)

	// Read back and verify
	data, err := os.ReadFile(filepath.Join(tempDir, "repository_list.txt"))
	require.NoError(t, err)

	content := string(data)
	
	// Verify content
	assert.Contains(t, content, "test-repo - https://github.com/testorg/test-repo.git")
	assert.Contains(t, content, "another-repo - https://github.com/testorg/another-repo.git")
	
	// Verify line count
	lines := strings.Split(strings.TrimSpace(content), "\n")
	assert.Len(t, lines, 2)
}
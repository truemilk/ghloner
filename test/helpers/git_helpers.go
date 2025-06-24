package helpers

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

// CreateTestRepo creates a test Git repository with initial commit
func CreateTestRepo(t *testing.T, path string) *git.Repository {
	t.Helper()

	// Create directory
	err := os.MkdirAll(path, 0755)
	require.NoError(t, err)

	// Initialize repository
	repo, err := git.PlainInit(path, false)
	require.NoError(t, err)

	// Create initial file
	testFile := filepath.Join(path, "README.md")
	err = os.WriteFile(testFile, []byte("# Test Repository\n"), 0644)
	require.NoError(t, err)

	// Add and commit
	w, err := repo.Worktree()
	require.NoError(t, err)

	_, err = w.Add("README.md")
	require.NoError(t, err)

	_, err = w.Commit("Initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@example.com",
		},
	})
	require.NoError(t, err)

	return repo
}

// CreateBareRepo creates a bare Git repository
func CreateBareRepo(t *testing.T, path string) *git.Repository {
	t.Helper()

	// Create directory
	err := os.MkdirAll(path, 0755)
	require.NoError(t, err)

	// Initialize bare repository
	repo, err := git.PlainInit(path, true)
	require.NoError(t, err)

	return repo
}

// SimulateNonFastForward simulates a non-fast-forward scenario in a repository
func SimulateNonFastForward(t *testing.T, remotePath, localPath string) {
	t.Helper()

	// Create remote repository with initial commit
	remoteRepo := CreateTestRepo(t, remotePath)

	// Clone to local
	localRepo, err := git.PlainClone(localPath, false, &git.CloneOptions{
		URL: remotePath,
	})
	require.NoError(t, err)

	// Make a commit in remote
	remoteFile := filepath.Join(remotePath, "remote.txt")
	err = os.WriteFile(remoteFile, []byte("Remote change\n"), 0644)
	require.NoError(t, err)

	remoteW, err := remoteRepo.Worktree()
	require.NoError(t, err)

	_, err = remoteW.Add("remote.txt")
	require.NoError(t, err)

	_, err = remoteW.Commit("Remote commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Remote User",
			Email: "remote@example.com",
		},
	})
	require.NoError(t, err)

	// Make a different commit in local
	localFile := filepath.Join(localPath, "local.txt")
	err = os.WriteFile(localFile, []byte("Local change\n"), 0644)
	require.NoError(t, err)

	localW, err := localRepo.Worktree()
	require.NoError(t, err)

	_, err = localW.Add("local.txt")
	require.NoError(t, err)

	_, err = localW.Commit("Local commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Local User",
			Email: "local@example.com",
		},
	})
	require.NoError(t, err)

	// Force push local to remote to create non-fast-forward
	err = localRepo.Push(&git.PushOptions{
		RemoteName: "origin",
		Force:      true,
	})
	require.NoError(t, err)
}

// AddRemote adds a remote to a repository
func AddRemote(t *testing.T, repo *git.Repository, name, url string) {
	t.Helper()

	_, err := repo.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: []string{url},
	})
	require.NoError(t, err)
}

// CreateTempDir creates a temporary directory for testing
func CreateTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "ghloner-test-*")
	require.NoError(t, err)

	t.Cleanup(func() {
		os.RemoveAll(dir)
	})

	return dir
}

// AssertFileExists asserts that a file exists
func AssertFileExists(t *testing.T, path string) {
	t.Helper()

	_, err := os.Stat(path)
	require.NoError(t, err, fmt.Sprintf("file %s should exist", path))
}

// AssertFileNotExists asserts that a file does not exist
func AssertFileNotExists(t *testing.T, path string) {
	t.Helper()

	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err), fmt.Sprintf("file %s should not exist", path))
}
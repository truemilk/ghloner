# Handling "non-fast-forward update" Git Error

## Problem Statement

When updating a Git repository, we sometimes encounter a "non-fast-forward update" error. This error occurs when there are conflicting changes between the local and remote repositories that can't be automatically merged. Currently, the application doesn't handle this error specifically, which can lead to failed repository updates.

## Solution

The solution is to detect this specific error and implement a recovery mechanism that:
1. Deletes the repository folder entirely
2. Clones the repository from scratch
3. Logs these operations clearly for transparency

## Implementation Details

### 1. Modify the Git Operations Module

Update the `PullRepository` method in `internal/repository/git/operations.go` to detect and return a specific error type for non-fast-forward updates.

```go
// NonFastForwardError represents a non-fast-forward update error
type NonFastForwardError struct {
    RepoName string
    Err      error
}

// Error implements the error interface
func (e *NonFastForwardError) Error() string {
    return fmt.Sprintf("non-fast-forward update error for %s: %v", e.RepoName, e.Err)
}

// IsNonFastForwardError checks if an error is a non-fast-forward error
func IsNonFastForwardError(err error) bool {
    return err != nil && strings.Contains(err.Error(), "non-fast-forward update")
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

        if err != nil {
            if errors.Is(err, git.NoErrAlreadyUpToDate) {
                return nil
            }
            
            if IsNonFastForwardError(err) {
                return &NonFastForwardError{
                    RepoName: repoName,
                    Err:      err,
                }
            }
            
            return fmt.Errorf("error pulling: %w", err)
        }

        return nil
    })
}
```

### 2. Modify the Git Manager Module

Update the `updateRepository` method in `internal/repository/git/manager.go` to handle the specific error:

```go
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
        err := m.operations.PullRepository(gitRepo, repoName, token)
        
        // Check for non-fast-forward error
        var nonFastForwardErr *NonFastForwardError
        if errors.As(err, &nonFastForwardErr) {
            slog.Warn("Encountered non-fast-forward update error, will delete and re-clone repository",
                "repository", repoName,
                "error", err)
            
            // Close the repository to release any locks
            m.repoMutex.Lock()
            delete(m.repositories, repoName)
            m.repoMutex.Unlock()
            
            // Delete the repository folder
            slog.Info("Deleting repository folder", "repository", repoName, "path", repoPath)
            if err := os.RemoveAll(repoPath); err != nil {
                slog.Error("Failed to delete repository folder", "repository", repoName, "path", repoPath, "error", err)
                return fmt.Errorf("error deleting repository folder: %w", err)
            }
            
            // Clone the repository from scratch
            cloneURL := strings.Replace(authURL, fmt.Sprintf("%s@", token), "", 1)
            slog.Info("Re-cloning repository", "repository", repoName, "url", cloneURL)
            
            cloneStartTime := time.Now()
            if err := m.cloneRepository(repoPath, cloneURL, repoName, token); err != nil {
                slog.Error("Failed to re-clone repository", "repository", repoName, "error", err)
                return err
            }
            cloneEndTime := time.Now()
            
            slog.Info("Successfully re-cloned repository after non-fast-forward error",
                "repository", repoName,
                "elapsed_time", cloneEndTime.Sub(cloneStartTime))
                
            return nil
        } else if err != nil {
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
```

## Logging Details

The implementation includes comprehensive logging:

1. **When a non-fast-forward error is detected**:
   ```
   WARN Encountered non-fast-forward update error, will delete and re-clone repository repository=example-repo error="non-fast-forward update error for example-repo: non-fast-forward update"
   ```

2. **When the repository folder is deleted**:
   ```
   INFO Deleting repository folder repository=example-repo path=/path/to/example-repo
   ```

3. **When the repository is re-cloned**:
   ```
   INFO Re-cloning repository repository=example-repo url=https://github.com/org/example-repo.git
   ```

4. **When the re-cloning is successful**:
   ```
   INFO Successfully re-cloned repository after non-fast-forward error repository=example-repo elapsed_time=2.5s
   ```

## Testing Strategy

1. **Manual Testing**:
   - Create a test repository with conflicting changes
   - Run the application and verify it handles the error correctly
   - Check logs to ensure operations are properly recorded

2. **Integration Testing**:
   - Add tests that simulate the non-fast-forward error
   - Verify the application responds correctly

## Benefits of This Approach

1. **Robustness**: The application will handle non-fast-forward errors gracefully
2. **Transparency**: Detailed logging will make it clear what's happening
3. **Simplicity**: Deleting and re-cloning is a straightforward solution
4. **Minimal Changes**: We're only modifying existing code, not adding new components
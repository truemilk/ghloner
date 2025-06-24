package git

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsNonFastForwardError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "non-fast-forward error",
			err:      errors.New("failed to push: non-fast-forward update"),
			expected: true,
		},
		{
			name:     "contains non-fast-forward in message",
			err:      errors.New("error: non-fast-forward update rejected"),
			expected: true,
		},
		{
			name:     "other error",
			err:      errors.New("connection timeout"),
			expected: false,
		},
		{
			name:     "NonFastForwardError type",
			err:      &NonFastForwardError{RepoName: "test-repo", Err: errors.New("push failed")},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsNonFastForwardError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNonFastForwardError_Error(t *testing.T) {
	err := &NonFastForwardError{
		RepoName: "test-repo",
		Err:      errors.New("push failed"),
	}

	expected := "non-fast-forward update error for test-repo: push failed"
	assert.Equal(t, expected, err.Error())
}

func TestRunWithRetry(t *testing.T) {
	tests := []struct {
		name        string
		retryCount  int
		operation   string
		repoName    string
		errors      []error
		wantErr     bool
		wantAttempts int
		errContains string
	}{
		{
			name:       "success on first attempt",
			retryCount: 3,
			operation:  "clone",
			repoName:   "test-repo",
			errors:     []error{nil},
			wantErr:    false,
			wantAttempts: 1,
		},
		{
			name:       "success after retry",
			retryCount: 3,
			operation:  "pull",
			repoName:   "test-repo",
			errors:     []error{errors.New("network error"), nil},
			wantErr:    false,
			wantAttempts: 2,
		},
		{
			name:       "all attempts fail",
			retryCount: 3,
			operation:  "fetch",
			repoName:   "test-repo",
			errors:     []error{errors.New("error 1"), errors.New("error 2"), errors.New("error 3")},
			wantErr:    true,
			wantAttempts: 3,
			errContains: "error 3",
		},
		{
			name:       "empty repository error - no retry",
			retryCount: 3,
			operation:  "clone",
			repoName:   "test-repo",
			errors:     []error{errors.New("remote repository is empty")},
			wantErr:    true,
			wantAttempts: 1,
			errContains: "remote repository is empty",
		},
		{
			name:       "non-fast-forward error",
			retryCount: 3,
			operation:  "pull",
			repoName:   "test-repo",
			errors:     []error{
				errors.New("non-fast-forward update rejected"),
				errors.New("non-fast-forward update rejected"),
				errors.New("non-fast-forward update rejected"),
			},
			wantErr:    true,
			wantAttempts: 3,
			errContains: "non-fast-forward",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops := NewOperations(tt.retryCount)
			attempts := 0
			
			err := ops.RunWithRetry(tt.repoName, tt.operation, func() error {
				if attempts < len(tt.errors) {
					err := tt.errors[attempts]
					attempts++
					return err
				}
				return errors.New("unexpected call")
			})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
			
			assert.Equal(t, tt.wantAttempts, attempts)
		})
	}
}

func TestRunWithRetry_BackoffTiming(t *testing.T) {
	ops := NewOperations(3)
	attempts := 0
	startTimes := []time.Time{}

	err := ops.RunWithRetry("test-repo", "operation", func() error {
		startTimes = append(startTimes, time.Now())
		attempts++
		if attempts < 3 {
			return errors.New("retry me")
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 3, attempts)

	// Check that there was a delay between attempts
	// The delays should be approximately 5s each
	if len(startTimes) >= 2 {
		delay1 := startTimes[1].Sub(startTimes[0])
		assert.True(t, delay1 >= 4900*time.Millisecond && delay1 <= 5100*time.Millisecond,
			"First retry delay should be ~5s, got %v", delay1)
	}

	if len(startTimes) >= 3 {
		delay2 := startTimes[2].Sub(startTimes[1])
		assert.True(t, delay2 >= 4900*time.Millisecond && delay2 <= 5100*time.Millisecond,
			"Second retry delay should be ~5s, got %v", delay2)
	}
}
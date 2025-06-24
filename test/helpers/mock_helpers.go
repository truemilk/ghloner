package helpers

import (
	"context"
	"os"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/google/go-github/v60/github"
	"github.com/stretchr/testify/mock"
)

// MockGitRepository is a mock implementation of git.Repository
type MockGitRepository struct {
	mock.Mock
}

func (m *MockGitRepository) PlainClone(path string, isBare bool, o *git.CloneOptions) (*git.Repository, error) {
	args := m.Called(path, isBare, o)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*git.Repository), args.Error(1)
}

func (m *MockGitRepository) PlainOpen(path string) (*git.Repository, error) {
	args := m.Called(path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*git.Repository), args.Error(1)
}

func (m *MockGitRepository) Worktree() (*git.Worktree, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*git.Worktree), args.Error(1)
}

func (m *MockGitRepository) Head() (*plumbing.Reference, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*plumbing.Reference), args.Error(1)
}

// MockWorktree is a mock implementation of git.Worktree
type MockWorktree struct {
	mock.Mock
}

func (m *MockWorktree) Pull(o *git.PullOptions) error {
	args := m.Called(o)
	return args.Error(0)
}

// MockGitHubClient is a mock implementation of GitHub client interface
type MockGitHubClient struct {
	mock.Mock
}

func (m *MockGitHubClient) ListByOrg(ctx context.Context, org string, opts *github.RepositoryListByOrgOptions) ([]*github.Repository, *github.Response, error) {
	args := m.Called(ctx, org, opts)
	if args.Get(0) == nil {
		return nil, args.Get(1).(*github.Response), args.Error(2)
	}
	return args.Get(0).([]*github.Repository), args.Get(1).(*github.Response), args.Error(2)
}

// MockFileSystem is a mock implementation of file system operations
type MockFileSystem struct {
	mock.Mock
}

func (m *MockFileSystem) Create(name string) (*os.File, error) {
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*os.File), args.Error(1)
}

func (m *MockFileSystem) Remove(name string) error {
	args := m.Called(name)
	return args.Error(0)
}

func (m *MockFileSystem) RemoveAll(path string) error {
	args := m.Called(path)
	return args.Error(0)
}

func (m *MockFileSystem) Stat(name string) (os.FileInfo, error) {
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(os.FileInfo), args.Error(1)
}

func (m *MockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	args := m.Called(path, perm)
	return args.Error(0)
}

// MockFileInfo is a mock implementation of os.FileInfo
type MockFileInfo struct {
	mock.Mock
}

func (m *MockFileInfo) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockFileInfo) Size() int64 {
	args := m.Called()
	return args.Get(0).(int64)
}

func (m *MockFileInfo) Mode() os.FileMode {
	args := m.Called()
	return args.Get(0).(os.FileMode)
}

func (m *MockFileInfo) ModTime() time.Time {
	args := m.Called()
	return args.Get(0).(time.Time)
}

func (m *MockFileInfo) IsDir() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockFileInfo) Sys() interface{} {
	args := m.Called()
	return args.Get(0)
}
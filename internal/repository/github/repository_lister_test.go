package github

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/go-github/v60/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/truemilk/ghloner/internal/config"
	"github.com/truemilk/ghloner/test/fixtures"
)

func TestNewRepositoryLister(t *testing.T) {
	client := &github.Client{}
	cfg := &config.Config{
		Workers: 5,
	}

	lister := NewRepositoryLister(client, cfg)

	assert.NotNil(t, lister)
	assert.Equal(t, client, lister.client)
	assert.Equal(t, cfg, lister.config)
}

func TestListRepositories_ClientConfiguration(t *testing.T) {
	// Test that the lister properly uses the client configuration
	client := github.NewClient(nil)
	cfg := &config.Config{
		Workers: 10,
	}
	
	lister := NewRepositoryLister(client, cfg)
	
	// Verify configuration is stored
	assert.Equal(t, 10, lister.config.Workers)
}

func TestFetchRemainingPages_ContextCancellation(t *testing.T) {
	// Create context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())
	orgName := "testorg"

	// Create test data
	firstPageRepos := fixtures.CreateTestRepositories(2)
	resp := &github.Response{
		NextPage: 2,
		LastPage: 5,
		Response: &http.Response{StatusCode: 200},
	}

	client := &github.Client{}
	cfg := &config.Config{Workers: 2}
	lister := NewRepositoryLister(client, cfg)

	// Cancel context before execution
	cancel()

	// Execute
	allRepos, err := lister.fetchRemainingPages(ctx, orgName, firstPageRepos, resp)

	// Should get context cancelled error
	require.Error(t, err)
	assert.Nil(t, allRepos)
}
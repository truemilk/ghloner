package fixtures

import (
	"net/http"

	"github.com/google/go-github/v60/github"
)

// CreateTestRepository creates a test GitHub repository
func CreateTestRepository(name, cloneURL string) *github.Repository {
	fullName := "testorg/" + name
	return &github.Repository{
		ID:       github.Int64(123),
		Name:     github.String(name),
		FullName: github.String(fullName),
		CloneURL: github.String(cloneURL),
		SSHURL:   github.String("git@github.com:" + fullName + ".git"),
		Private:  github.Bool(false),
		Fork:     github.Bool(false),
		Size:     github.Int(100),
		Language: github.String("Go"),
		DefaultBranch: github.String("main"),
	}
}

// CreateTestRepositories creates a list of test repositories
func CreateTestRepositories(count int) []*github.Repository {
	repos := make([]*github.Repository, count)
	for i := 0; i < count; i++ {
		name := "test-repo-" + string(rune('a'+i))
		cloneURL := "https://github.com/testorg/" + name + ".git"
		repos[i] = CreateTestRepository(name, cloneURL)
		repos[i].ID = github.Int64(int64(i + 1))
	}
	return repos
}

// CreateGitHubResponse creates a test GitHub API response
func CreateGitHubResponse(page, lastPage int) *github.Response {
	resp := &github.Response{
		Response: &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
		},
	}
	
	if lastPage > 1 {
		resp.FirstPage = 1
		resp.LastPage = lastPage
		resp.NextPage = page + 1
		if page > 1 {
			resp.PrevPage = page - 1
		}
		if page >= lastPage {
			resp.NextPage = 0
		}
	}
	
	return resp
}

// ErrorResponse creates an error response
func ErrorResponse(statusCode int, message string) error {
	return &github.ErrorResponse{
		Response: &http.Response{
			StatusCode: statusCode,
		},
		Message: message,
	}
}
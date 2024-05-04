package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"time"
)

type PR struct {
	Title     string    `json:"title"`
	HtmlURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
}

// Returns all open PRs for a repo, with the most recent PRs first
func ListPRsForRepo(owner, repo, token string) ([]PR, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls", owner, repo)
	prs, err := list[PR](url, token)
	if err != nil {
		return []PR{}, fmt.Errorf("Failed to list pull requests: %s", err.Error())
	}
	slices.SortFunc(prs, func(a, b PR) int {
		return -1 * a.CreatedAt.Compare(b.CreatedAt)
	})
	return prs, nil
}

type Issue struct {
	Title       string `json:"title"`
	HtmlURL     string `json:"html_url"`
	PullRequest struct {
		URL string `json:"url"`
	} `json:"pull_request"`
	CreatedAt time.Time `json:"created_at"`
}

// Returns all open issues for a repo, with the most recent issues first
func ListIssuesForRepo(owner, repo, token string) ([]Issue, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues", owner, repo)
	issues, err := list[Issue](url, token)
	if err != nil {
		return []Issue{}, fmt.Errorf("Failed to list issues: %s", err.Error())
	}
	var filteredIssues []Issue
	for _, issue := range issues {
		// The issues endpoint returns pull requests as well, see
		// https://docs.github.com/en/rest/issues/issues?apiVersion=2022-11-28#list-repository-issues
		if issue.PullRequest.URL == "" {
			filteredIssues = append(filteredIssues, issue)
		}
	}
	slices.SortFunc(issues, func(a, b Issue) int {
		return -1 * a.CreatedAt.Compare(b.CreatedAt)
	})
	return filteredIssues, nil
}

var nextPagePattern = regexp.MustCompile(`<([\S]+)>; rel="next"`)

// Extracts the url to the next page from the link header
// Returns the empty string if not found
func getNextPage(linkHeader string) string {
	match := nextPagePattern.FindStringSubmatch(linkHeader)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func list[T PR | Issue](url, token string) ([]T, error) {
	currentPage := url
	var allOutput []T
	for currentPage != "" {
		req, err := http.NewRequest("GET", currentPage, nil)
		if err != nil {
			return []T{}, fmt.Errorf("Could not create GET request: %s", err.Error())
		}
		if token != "" {
			req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return []T{}, fmt.Errorf("Failed to make request: %s", err.Error())
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return []T{}, fmt.Errorf("Got non-200 status code: %s", resp.Status)
		}
		var output []T
		if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
			return []T{}, fmt.Errorf("Could not parse response: %s", err.Error())
		}
		allOutput = append(allOutput, output...)
		currentPage = getNextPage(resp.Header.Get("Link"))
	}
	return allOutput, nil
}
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

// GitHubSummary fetches recent activity from watched repos.
// Classified as READ_ONLY.
type GitHubSummary struct {
	token      string
	watchRepos []string
}

// NewGitHubSummary creates a GitHubSummary tool.
func NewGitHubSummary(token string, watchRepos []string) *GitHubSummary {
	return &GitHubSummary{token: token, watchRepos: watchRepos}
}

type githubSummaryParams struct {
	// Optional override for repos to fetch — defaults to configured watch_repos.
	Repos []string `json:"repos"`
}

func (g *GitHubSummary) Name() string        { return "github_summary" }
func (g *GitHubSummary) Description() string { return "Fetch recent GitHub activity for watched repos." }
func (g *GitHubSummary) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"repos": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Repos to fetch (owner/repo). Defaults to configured watch_repos.",
			},
		},
	}
}

// wrapExternal sanitizes and wraps external content to defend against prompt injection.
func wrapExternal(content string) string {
	// Close-tag sanitization: prevent external content from escaping its wrapper.
	safe := strings.ReplaceAll(content, "</external_content>", "[/external_content]")
	return "<external_content>\n" + safe + "\n</external_content>"
}

func (g *GitHubSummary) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var p githubSummaryParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("github_summary: invalid params: %w", err)
	}

	repos := p.Repos
	if len(repos) == 0 {
		repos = g.watchRepos
	}
	if len(repos) == 0 {
		return "", fmt.Errorf("github_summary: no repos configured; set watch_repos in config.toml")
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: g.token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	var parts []string
	for _, repo := range repos {
		summary, err := fetchRepoSummary(ctx, client, repo)
		if err != nil {
			parts = append(parts, fmt.Sprintf("## %s\nError: %v\n", repo, err))
			continue
		}
		parts = append(parts, summary)
	}

	combined := strings.Join(parts, "\n---\n")
	return wrapExternal(combined), nil
}

func fetchRepoSummary(ctx context.Context, client *github.Client, repoSpec string) (string, error) {
	parts := strings.SplitN(repoSpec, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid repo spec: %s (expected owner/repo)", repoSpec)
	}
	owner, repo := parts[0], parts[1]

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## %s/%s\n\n", owner, repo))

	// Open PRs
	prs, _, err := client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		State: "open",
		ListOptions: github.ListOptions{PerPage: 10},
	})
	if err != nil {
		return "", fmt.Errorf("listing PRs: %w", err)
	}
	if len(prs) > 0 {
		sb.WriteString(fmt.Sprintf("**Open PRs (%d):**\n", len(prs)))
		for _, pr := range prs {
			sb.WriteString(fmt.Sprintf("- #%d: %s (@%s)\n",
				pr.GetNumber(), pr.GetTitle(), pr.GetUser().GetLogin()))
		}
		sb.WriteString("\n")
	}

	// Recent issues
	issues, _, err := client.Issues.ListByRepo(ctx, owner, repo, &github.IssueListByRepoOptions{
		State: "open",
		ListOptions: github.ListOptions{PerPage: 10},
	})
	if err != nil {
		return "", fmt.Errorf("listing issues: %w", err)
	}
	// Filter out PRs from issues list (GitHub API returns both).
	var realIssues []*github.Issue
	for _, iss := range issues {
		if iss.PullRequestLinks == nil {
			realIssues = append(realIssues, iss)
		}
	}
	if len(realIssues) > 0 {
		sb.WriteString(fmt.Sprintf("**Open Issues (%d):**\n", len(realIssues)))
		for _, iss := range realIssues {
			sb.WriteString(fmt.Sprintf("- #%d: %s (@%s)\n",
				iss.GetNumber(), iss.GetTitle(), iss.GetUser().GetLogin()))
		}
	}

	return sb.String(), nil
}

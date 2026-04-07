package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c4xx/kai/internal/config"
)

func TestExtractRelevance(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantBody  string
		wantScore int
	}{
		{
			name:      "score at end",
			input:     "# Briefing\n\nSome content.\nKAI_RELEVANCE: 7",
			wantBody:  "# Briefing\n\nSome content.",
			wantScore: 7,
		},
		{
			name:      "score with trailing newline",
			input:     "Content here\nKAI_RELEVANCE: 3\n",
			wantBody:  "Content here",
			wantScore: 3,
		},
		{
			name:      "no score line returns unchanged",
			input:     "# Briefing\n\nNo score here.",
			wantBody:  "# Briefing\n\nNo score here.",
			wantScore: 0,
		},
		{
			name:      "score 10",
			input:     "Urgent items\nKAI_RELEVANCE: 10",
			wantBody:  "Urgent items",
			wantScore: 10,
		},
		{
			name:      "empty input",
			input:     "",
			wantBody:  "",
			wantScore: 0,
		},
		{
			name:      "score mid-text still stripped (scans backwards)",
			input:     "Line1\nKAI_RELEVANCE: 5\nLine3",
			wantBody:  "Line1",
			wantScore: 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, score := extractRelevance(tt.input)
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
			if score != tt.wantScore {
				t.Errorf("score = %d, want %d", score, tt.wantScore)
			}
		})
	}
}

func TestBuildFTSQuery(t *testing.T) {
	tests := []struct {
		name       string
		watchRepos []string
		wantTerms  []string // all must appear in result
	}{
		{
			name:       "single repo",
			watchRepos: []string{"alice/myapp"},
			wantTerms:  []string{"alice", "myapp"},
		},
		{
			name:       "multiple repos deduplicates owner",
			watchRepos: []string{"alice/api", "alice/web"},
			wantTerms:  []string{"alice", "api", "web"},
		},
		{
			name:       "empty",
			watchRepos: nil,
			wantTerms:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildFTSQuery(tt.watchRepos)
			for _, term := range tt.wantTerms {
				found := false
				// terms are joined with " OR "
				for _, part := range splitOR(got) {
					if part == term {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("term %q not found in FTS query %q", term, got)
				}
			}
			// check no duplicates
			seen := map[string]bool{}
			for _, part := range splitOR(got) {
				if seen[part] {
					t.Errorf("duplicate term %q in FTS query %q", part, got)
				}
				seen[part] = true
			}
		})
	}
}

func splitOR(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i <= len(s)-4; i++ {
		if s[i:i+4] == " OR " {
			out = append(out, s[start:i])
			start = i + 4
		}
	}
	out = append(out, s[start:])
	return out
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{100000, "100,000"},
		{1234567, "1,234,567"},
		{-5, "0"}, // negative clamped to 0
	}
	for _, tt := range tests {
		got := formatTokens(tt.n)
		if got != tt.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestDeliverBriefing_NoBriefingFeedback(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:          dir,
		BriefingFeedback: false,
		Limits:           config.LimitsConfig{DailyTokenBudget: 100000},
	}
	text := "# Morning Briefing\n\nAll looks good.\nKAI_RELEVANCE: 6"
	runID := "abcd1234efgh5678"
	ts := time.Date(2026, 4, 3, 9, 0, 0, 0, time.UTC).Unix()

	if err := deliverBriefing(cfg, text, runID, ts, 500); err != nil {
		t.Fatalf("deliverBriefing: %v", err)
	}

	path := filepath.Join(dir, "briefings", "2026-04-03-abcd1234.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading briefing file: %v", err)
	}
	got := string(data)

	// KAI_RELEVANCE line must be stripped
	if contains(got, "KAI_RELEVANCE") {
		t.Error("KAI_RELEVANCE line should be stripped from briefing file")
	}
	// Briefing Stats footer must NOT appear (feedback disabled)
	if contains(got, "Briefing Stats") {
		t.Error("Briefing Stats should not appear when briefing_feedback=false")
	}
	if !contains(got, "All looks good.") {
		t.Error("briefing body should be present")
	}
}

func TestDeliverBriefing_WithBriefingFeedback(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:          dir,
		BriefingFeedback: true,
		Limits:           config.LimitsConfig{DailyTokenBudget: 100000},
	}
	text := "# Briefing\n\nStuff.\nKAI_RELEVANCE: 8"
	runID := "zzzz9999aaaa1111"
	ts := time.Date(2026, 4, 3, 9, 0, 0, 0, time.UTC).Unix()

	if err := deliverBriefing(cfg, text, runID, ts, 3412); err != nil {
		t.Fatalf("deliverBriefing: %v", err)
	}

	path := filepath.Join(dir, "briefings", "2026-04-03-zzzz9999.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading briefing file: %v", err)
	}
	got := string(data)

	if !contains(got, "Briefing Stats") {
		t.Error("Briefing Stats footer should appear when briefing_feedback=true")
	}
	if !contains(got, "3,412") {
		t.Error("tokens used should appear formatted")
	}
	if !contains(got, "8/10") {
		t.Error("relevance score should appear")
	}
	if contains(got, "KAI_RELEVANCE") {
		t.Error("KAI_RELEVANCE line should be stripped")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

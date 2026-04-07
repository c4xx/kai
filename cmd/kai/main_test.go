package main

import (
	"testing"
	"time"
)

func TestParseCronFromTime(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"9:00 AM", "0 9 * * *"},
		{"9:00 am", "0 9 * * *"},
		{"9:30 AM", "30 9 * * *"},
		{"12:00 PM", "0 12 * * *"},
		{"1:00 PM", "0 13 * * *"},
		{"6:30 PM", "30 18 * * *"},
		{"08:00", "0 8 * * *"},
		{"23:45", "45 23 * * *"},
		{"bad", "0 9 * * *"}, // fallback
	}
	for _, tt := range tests {
		got := parseCronFromTime(tt.input)
		if got != tt.want {
			t.Errorf("parseCronFromTime(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "30m"},
		{90 * time.Minute, "1h 30m"},
		{25 * time.Hour, "25h 0m"},
		{0, "0m"},
	}
	for _, tt := range tests {
		got := formatAge(tt.d)
		if got != tt.want {
			t.Errorf("formatAge(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"exact", 5, "exact"},
	}
	for _, tt := range tests {
		got := truncate(tt.s, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
		}
	}
}

func TestBlastRadiusColor(t *testing.T) {
	tests := []struct {
		br   string
		want string
	}{
		{"READ_ONLY", greenColor},
		{"IDEMPOTENT_WRITE", greenColor},
		{"STATE_CHANGE", yellowColor},
		{"DESTRUCTIVE", redColor},
		{"UNKNOWN", ""},
	}
	for _, tt := range tests {
		got := blastRadiusColor(tt.br)
		if got != tt.want {
			t.Errorf("blastRadiusColor(%q) = %q, want %q", tt.br, got, tt.want)
		}
	}
}

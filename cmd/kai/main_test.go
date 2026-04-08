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

func TestParseStandupLine(t *testing.T) {
	tests := []struct {
		line      string
		wantField string
		wantValue string
		wantOK    bool
	}{
		{"done: finished login page", "done", "finished login page", true},
		{"Done: finished login page", "done", "finished login page", true},
		{"昨日: completed auth", "done", "completed auth", true},
		{"today: starting auth module", "today", "starting auth module", true},
		{"Today: something", "today", "something", true},
		{"今日: working on tests", "today", "working on tests", true},
		{"blocked: waiting for API docs", "blocked", "waiting for API docs", true},
		{"Blocked: nothing", "blocked", "nothing", true},
		{"卡点: api not ready", "blocked", "api not ready", true},
		{"unrecognized line", "", "", false},
		{"random text here", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range tests {
		field, value, ok := parseStandupLine(tc.line)
		if ok != tc.wantOK {
			t.Errorf("parseStandupLine(%q): ok=%v, want %v", tc.line, ok, tc.wantOK)
			continue
		}
		if ok {
			if field != tc.wantField {
				t.Errorf("parseStandupLine(%q): field=%q, want %q", tc.line, field, tc.wantField)
			}
			if value != tc.wantValue {
				t.Errorf("parseStandupLine(%q): value=%q, want %q", tc.line, value, tc.wantValue)
			}
		}
	}
}

func TestValidateStandupDate(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

	// Empty → today.
	got, err := validateStandupDate("")
	if err != nil || got != today {
		t.Errorf("validateStandupDate(''): got=%q err=%v, want=%q", got, err, today)
	}

	// Valid past date.
	got, err = validateStandupDate(yesterday)
	if err != nil || got != yesterday {
		t.Errorf("validateStandupDate(%q): got=%q err=%v", yesterday, got, err)
	}

	// Future date: must error.
	_, err = validateStandupDate(tomorrow)
	if err == nil {
		t.Errorf("validateStandupDate(%q): expected error for future date", tomorrow)
	}

	// Invalid format.
	_, err = validateStandupDate("07-04-2026")
	if err == nil {
		t.Error("expected error for invalid date format")
	}
}

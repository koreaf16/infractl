package tui

import (
	"regexp"
	"testing"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]|\x1b\][^\x07]*\x07|\x1b\\`)

func TestBuildRoundedBorderTitleLineKeepsTitleAndCorners(t *testing.T) {
	line := buildRoundedBorderTitleLine(40, "Shell")
	if line == "" {
		t.Fatal("expected a rendered title line")
	}
	plain := stripANSI(line)
	if got := string([]rune(plain)[0]); got != "╭" {
		t.Fatalf("top line should start with rounded corner, got %q", got)
	}
	if got := string([]rune(plain)[len([]rune(plain))-1]); got != "╮" {
		t.Fatalf("top line should end with rounded corner, got %q", got)
	}
	if !containsVisibleText(plain, "Shell") {
		t.Fatalf("expected title text in top line: %q", line)
	}
}

func TestVisibleShellBoxUsesLatestCompletedShellWhenIdle(t *testing.T) {
	m := AppModel{}
	m.history.Add(toolHistoryEntry{
		toolName:   "shell_exec",
		shellLines: []string{"line 1", "line 2"},
	})

	title, lines, ok := m.visibleShellBox()
	if !ok {
		t.Fatal("expected a visible shell box")
	}
	if title != "Shell" {
		t.Fatalf("title = %q, want %q", title, "Shell")
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
}

func TestVisibleShellBoxPrefersRunningShellOverHistory(t *testing.T) {
	m := AppModel{busy: true}
	m.history.Add(toolHistoryEntry{
		toolName:   "shell_exec",
		shellLines: []string{"old"},
	})
	m.activeTools.Add("t1", "shell_exec", "localhost", nil)
	m.activeTools.AppendOutput("t1", "live")

	title, lines, ok := m.visibleShellBox()
	if !ok {
		t.Fatal("expected a visible shell box")
	}
	if title != "Shell" {
		t.Fatalf("title = %q, want %q", title, "Shell")
	}
	if len(lines) != 1 || lines[0] != "live" {
		t.Fatalf("lines = %#v, want live output", lines)
	}
}

func containsVisibleText(s, needle string) bool {
	return len(needle) == 0 || regexp.MustCompile(regexp.QuoteMeta(needle)).FindStringIndex(s) != nil
}

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

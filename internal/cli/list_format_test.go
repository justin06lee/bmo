package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/justin06lee/bmo/internal/bmo"
)

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		stamp string
		want  string
	}{
		{"2026-08-06T11:59:30Z", "just now"},
		{"2026-08-06T11:15:00Z", "45m ago"},
		{"2026-08-06T03:00:00Z", "9h ago"},
		{"2026-08-01T12:00:00Z", "5d ago"},
		{"2026-06-01T12:00:00Z", "2026-06-01"},
		{"not-a-time", "not-a-time"},
	}
	for _, c := range cases {
		if got := relativeTime(c.stamp, now); got != c.want {
			t.Errorf("relativeTime(%q) = %q, want %q", c.stamp, got, c.want)
		}
	}
}

func TestPrintSkillListGroupsAndFormats(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	entries := []bmo.SkillMeta{
		{
			Name: "bmo", Scope: bmo.ScopeGlobal, Source: "bmo",
			SourceType:    string(bmo.SourceEmbedded),
			InstalledPath: "/home/u/.claude/skills/bmo",
			UpdatedAt:     "2026-08-06T11:00:00Z",
			Description:   strings.Repeat("x", 100),
		},
		{
			Name: "chrome", Scope: bmo.ScopeProject, Source: "justin06lee/chrome.md",
			SourceRef:     "main",
			SourceType:    string(bmo.SourceGitHub),
			InstalledPath: "/proj/.claude/skills/chrome",
			UpdatedAt:     "2026-08-06T11:59:50Z",
			Description:   "Drive Chrome",
			Agents:        []string{"a.md", "b.md"},
		},
	}
	var buf bytes.Buffer
	printSkillList(&buf, entries, "/proj", now)
	out := buf.String()

	for _, want := range []string{
		"Global", "Project",
		".claude/skills",             // project dir shortened relative to cwd
		"bmo (embedded)",             // embedded source labeled
		"justin06lee/chrome.md@main", // resolved ref appended
		"1h ago", "just now",
		"2", // agent count
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, strings.Repeat("x", 60)) {
		t.Errorf("expected long description to be truncated:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("expected no ANSI codes when writing to a buffer:\n%s", out)
	}
	if strings.Index(out, "Global") > strings.Index(out, "Project") {
		t.Errorf("expected global group before project group:\n%s", out)
	}
}

func TestPrintSkillListEmpty(t *testing.T) {
	var buf bytes.Buffer
	printSkillList(&buf, nil, "", time.Now())
	if !strings.Contains(buf.String(), "No skills installed") {
		t.Errorf("expected empty-state message, got %q", buf.String())
	}
}

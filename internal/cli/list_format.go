package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/justin06lee/bmo/internal/bmo"
)

const descriptionWidth = 48

// printSkillList renders tracked skills grouped by scope: one bold header per
// scope carrying the shared skills directory, then aligned rows of the
// per-skill facts. now is injected so tests are deterministic.
func printSkillList(w io.Writer, entries []bmo.SkillMeta, cwd string, now time.Time) {
	if len(entries) == 0 {
		fmt.Fprintln(w, "No skills installed.")
		fmt.Fprintln(w, "Try: bmo add owner/repo")
		return
	}
	bold, dim, reset := "", "", ""
	if useColor(w) {
		bold, dim, reset = "\x1b[1m", "\x1b[2m", "\x1b[0m"
	}
	first := true
	for _, scope := range []bmo.Scope{bmo.ScopeGlobal, bmo.ScopeProject} {
		var group []bmo.SkillMeta
		for _, entry := range entries {
			if entry.Scope == scope {
				group = append(group, entry)
			}
		}
		if len(group) == 0 {
			continue
		}
		if !first {
			fmt.Fprintln(w)
		}
		first = false
		fmt.Fprintf(w, "%s%s%s  %s%s%s\n", bold, scopeTitle(scope), reset, dim, shortenPath(filepath.Dir(group[0].InstalledPath), cwd), reset)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  NAME\tSOURCE\tAGENTS\tUPDATED\tDESCRIPTION")
		for _, entry := range group {
			agents := ""
			if n := len(entry.Agents); n > 0 {
				agents = fmt.Sprintf("%d", n)
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
				entry.Name,
				sourceLabel(entry),
				agents,
				relativeTime(entry.UpdatedAt, now),
				truncate(entry.Description, descriptionWidth),
			)
		}
		tw.Flush()
	}
}

func scopeTitle(scope bmo.Scope) string {
	if scope == bmo.ScopeProject {
		return "Project"
	}
	return "Global"
}

// sourceLabel renders the origin, appending the pinned/resolved ref when it
// adds information the raw source string doesn't already carry.
func sourceLabel(entry bmo.SkillMeta) string {
	src := entry.Source
	if entry.SourceType == string(bmo.SourceEmbedded) {
		return src + " (embedded)"
	}
	if entry.SourceRef != "" && !strings.Contains(src, "@") {
		src += "@" + entry.SourceRef
	}
	return src
}

// shortenPath makes an absolute path friendlier: paths under cwd become
// relative, and a $HOME prefix collapses to ~.
func shortenPath(path, cwd string) string {
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return path
}

// relativeTime renders an RFC3339 timestamp as a compact age ("3h ago"),
// falling back to the date for anything older than two weeks and to the raw
// string when unparsable.
func relativeTime(stamp string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return stamp
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

func truncate(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[:width-1]) + "…"
}

// useColor reports whether w is an interactive terminal that should get ANSI
// styling. NO_COLOR (https://no-color.org) and dumb terminals opt out.
func useColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

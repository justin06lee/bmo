package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/justin06lee/bmo/internal/bmo"
	"github.com/spf13/cobra"
)

func TestShouldBootstrap(t *testing.T) {
	cases := []struct {
		name    string
		cmdName string
		args    []string
		want    bool
	}{
		{"init is skipped", "init", nil, false},
		{"add embedded by name is skipped", "add", []string{bmo.EmbeddedSkillName}, false},
		{"add embedded self is skipped", "add", []string{"self"}, false},
		{"add embedded with keyword is skipped", "add", []string{"here", bmo.EmbeddedSkillName}, false},
		{"add other source bootstraps", "add", []string{"owner/repo"}, true},
		{"add with no args bootstraps", "add", nil, true},
		{"add with extra args bootstraps", "add", []string{bmo.EmbeddedSkillName, "extra"}, true},
		{"list bootstraps", "list", nil, true},
		{"doctor bootstraps", "doctor", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: tc.cmdName}
			if got := shouldBootstrap(cmd, tc.args); got != tc.want {
				t.Fatalf("shouldBootstrap(%q, %v) = %v, want %v", tc.cmdName, tc.args, got, tc.want)
			}
		})
	}
}

func TestSplitScopeKeyword(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		wantRest    []string
		wantKeyword string
		wantErr     bool
	}{
		{"no keyword", []string{"owner/repo"}, []string{"owner/repo"}, "", false},
		{"here before source", []string{"here", "owner/repo"}, []string{"owner/repo"}, "here", false},
		{"everywhere after source", []string{"owner/repo", "everywhere"}, []string{"owner/repo"}, "everywhere", false},
		{"keyword only", []string{"here"}, nil, "here", false},
		{"empty args", nil, nil, "", false},
		{"two keywords is an error", []string{"here", "everywhere"}, nil, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest, keyword, err := splitScopeKeyword(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("splitScopeKeyword(%v) = nil error, want error", tc.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitScopeKeyword(%v) unexpected error: %v", tc.args, err)
			}
			if keyword != tc.wantKeyword {
				t.Fatalf("keyword = %q, want %q", keyword, tc.wantKeyword)
			}
			if len(rest) != len(tc.wantRest) {
				t.Fatalf("rest = %v, want %v", rest, tc.wantRest)
			}
			for i := range rest {
				if rest[i] != tc.wantRest[i] {
					t.Fatalf("rest = %v, want %v", rest, tc.wantRest)
				}
			}
		})
	}
}

func TestKeywordScope(t *testing.T) {
	cases := []struct {
		name    string
		keyword string
		opts    *options
		want    bmo.Scope
	}{
		{"here means project", "here", &options{}, bmo.ScopeProject},
		{"everywhere means global", "everywhere", &options{}, bmo.ScopeGlobal},
		{"default is global", "", &options{}, bmo.ScopeGlobal},
		{"project flag without keyword", "", &options{project: true}, bmo.ScopeProject},
		{"keyword wins over default", "here", &options{}, bmo.ScopeProject},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := keywordScope(tc.keyword, tc.opts); got != tc.want {
				t.Fatalf("keywordScope(%q, %+v) = %v, want %v", tc.keyword, tc.opts, got, tc.want)
			}
		})
	}
}

// isolateHome points the global dir and bootstrap marker at a temp dir and
// registers a valid embedded bmo skill for the duration of the test.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Ensure the global skills dir falls back to HOME/.claude/skills.
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	skill := []byte("---\nname: bmo\ndescription: Use when the user wants to install Claude Code skills with the bmo CLI.\n---\n\n# bmo\n")
	bmo.SetEmbeddedFS(fstest.MapFS{
		"SKILL.md": &fstest.MapFile{Data: skill},
	})
	t.Cleanup(func() { bmo.SetEmbeddedFS(nil) })
	return home
}

func TestBootstrapBmoSkillInstallsAndMarks(t *testing.T) {
	home := isolateHome(t)

	cmd := &cobra.Command{Use: "list"}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	bootstrapBmoSkill(cmd)

	marker := filepath.Join(home, ".bmo", ".bootstrapped")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected bootstrap marker at %s: %v", marker, err)
	}

	installed := filepath.Join(home, ".claude", "skills", bmo.EmbeddedSkillName, "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("expected installed skill at %s: %v", installed, err)
	}

	if !bmoSkillTracked(home) {
		t.Fatalf("expected bmo skill to be tracked in metadata")
	}

	if stderr.Len() == 0 {
		t.Fatalf("expected a bootstrap message on stderr")
	}
}

func TestBootstrapBmoSkillIdempotent(t *testing.T) {
	home := isolateHome(t)

	first := &cobra.Command{Use: "list"}
	var firstErr bytes.Buffer
	first.SetErr(&firstErr)
	bootstrapBmoSkill(first)
	if firstErr.Len() == 0 {
		t.Fatalf("expected first run to print a bootstrap message")
	}

	// Remove the installed skill to prove the second run does not reinstall.
	if err := os.RemoveAll(filepath.Join(home, ".claude", "skills", bmo.EmbeddedSkillName)); err != nil {
		t.Fatal(err)
	}

	second := &cobra.Command{Use: "list"}
	var secondErr bytes.Buffer
	second.SetErr(&secondErr)
	bootstrapBmoSkill(second)

	if secondErr.Len() != 0 {
		t.Fatalf("expected second run to be a no-op, got stderr: %q", secondErr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", bmo.EmbeddedSkillName, "SKILL.md")); err == nil {
		t.Fatalf("did not expect skill to be reinstalled once the marker exists")
	}
}

func TestBootstrapBmoSkillSkipsWhenTracked(t *testing.T) {
	home := isolateHome(t)

	// Pre-record the bmo skill in global metadata, without a marker file.
	metaPath, err := bmo.GlobalMetadataPath()
	if err != nil {
		t.Fatal(err)
	}
	meta := bmo.EmptyMetadata()
	meta.Skills[bmo.EmbeddedSkillName] = bmo.SkillMeta{Name: bmo.EmbeddedSkillName}
	if err := bmo.WriteMetadata(metaPath, meta); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{Use: "list"}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	bootstrapBmoSkill(cmd)

	if stderr.Len() != 0 {
		t.Fatalf("expected no install message when already tracked, got: %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", bmo.EmbeddedSkillName, "SKILL.md")); err == nil {
		t.Fatalf("did not expect an install when the skill is already tracked")
	}
	// The marker should still be written so future runs short-circuit.
	if _, err := os.Stat(filepath.Join(home, ".bmo", ".bootstrapped")); err != nil {
		t.Fatalf("expected marker to be written even when install is skipped: %v", err)
	}
}

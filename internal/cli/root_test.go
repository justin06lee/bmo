package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
		{"harness list is informational", "harnesses", nil, false},
		{"help is skipped", "help", nil, false},
		{"completion is skipped", "completion", nil, false},
		{"__complete is skipped", "__complete", nil, false},
		{"__completeNoDesc is skipped", "__completeNoDesc", nil, false},
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

func TestShouldBootstrapCompletionSubcommands(t *testing.T) {
	completion := &cobra.Command{Use: "completion"}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		sub := &cobra.Command{Use: shell}
		completion.AddCommand(sub)
		if shouldBootstrap(sub, nil) {
			t.Fatalf("shouldBootstrap(completion %s) = true, want false", shell)
		}
	}
}

func TestSplitKeywords(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		minArgs     int
		wantRest    []string
		wantScope   string
		wantHarness string
		wantErr     bool
	}{
		{name: "no keyword", args: []string{"owner/repo"}, wantRest: []string{"owner/repo"}},
		{name: "here before source", args: []string{"here", "owner/repo"}, wantRest: []string{"owner/repo"}, wantScope: "here"},
		{name: "everywhere after source", args: []string{"owner/repo", "everywhere"}, wantRest: []string{"owner/repo"}, wantScope: "everywhere"},
		{name: "keyword only", args: []string{"here"}, wantScope: "here"},
		{name: "empty args", args: nil},
		{name: "harness after a required name", args: []string{"demo", "codex"}, minArgs: 1, wantRest: []string{"demo"}, wantHarness: "codex"},
		{name: "chatgpt alias after a required name", args: []string{"demo", "chatgpt"}, minArgs: 1, wantRest: []string{"demo"}, wantHarness: "chatgpt"},
		{name: "harness before a required name", args: []string{"codex", "demo"}, minArgs: 1, wantRest: []string{"demo"}, wantHarness: "codex"},
		// Stripping the token would leave the command without its required
		// argument, so it is the skill's name.
		{name: "lone harness name is the required arg", args: []string{"codex"}, minArgs: 1, wantRest: []string{"codex"}},
		{name: "lone harness name is a harness when nothing is required", args: []string{"codex"}, wantHarness: "codex"},
		{name: "leftmost harness name fills the required arg", args: []string{"codex", "cursor"}, minArgs: 1, wantRest: []string{"codex"}, wantHarness: "cursor"},
		// A required positional wins even over "everyone", so a skill (or
		// source folder) literally named everyone stays addressable.
		{name: "everyone fills a starving required arg", args: []string{"everyone"}, minArgs: 1, wantRest: []string{"everyone"}},
		{name: "everyone stays a keyword beside a real arg", args: []string{"demo", "everyone"}, minArgs: 1, wantRest: []string{"demo"}, wantHarness: "everyone"},
		{name: "cased tokens match like --harness", args: []string{"demo", "Codex", "HERE"}, minArgs: 1, wantRest: []string{"demo"}, wantScope: "here", wantHarness: "codex"},
		{name: "universe is a location keyword", args: []string{"demo", "universe"}, minArgs: 1, wantRest: []string{"demo"}, wantScope: "universe"},
		{name: "universe matches case-insensitively", args: []string{"demo", "UNIVERSE"}, minArgs: 1, wantRest: []string{"demo"}, wantScope: "universe"},
		{name: "universe beside a harness", args: []string{"demo", "universe", "codex"}, minArgs: 1, wantRest: []string{"demo"}, wantScope: "universe", wantHarness: "codex"},
		{name: "two locations is an error", args: []string{"here", "everywhere"}, wantErr: true},
		{name: "universe beside another location is an error", args: []string{"universe", "everywhere"}, wantErr: true},
		{name: "two harnesses is an error", args: []string{"owner/repo", "codex", "gemini"}, wantErr: true},
		{name: "two harnesses is an error even with one demoted", args: []string{"codex", "cursor", "amp"}, minArgs: 1, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest, scope, harness, err := splitKeywords(tc.args, tc.minArgs)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("splitKeywords(%v, %d) = nil error, want error", tc.args, tc.minArgs)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitKeywords(%v, %d) unexpected error: %v", tc.args, tc.minArgs, err)
			}
			if scope != tc.wantScope || harness != tc.wantHarness {
				t.Fatalf("scope, harness = %q, %q, want %q, %q", scope, harness, tc.wantScope, tc.wantHarness)
			}
			if strings.Join(rest, ",") != strings.Join(tc.wantRest, ",") {
				t.Fatalf("rest = %v, want %v", rest, tc.wantRest)
			}
		})
	}
}

func TestSplitHarnessKeywordsRejectsEveryone(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		minArgs int
	}{
		{"list-style command", []string{"everyone"}, 0},
		{"name-taking command", []string{"demo", "everyone"}, 1},
		{"everyone beside a location", []string{"here", "everyone"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := splitHarnessKeywords(tc.args, tc.minArgs)
			if err == nil || !strings.Contains(err.Error(), "bmo add, bmo remove, bmo update, and bmo share") {
				t.Fatalf("splitHarnessKeywords(%v, %d) error = %v, want the everyone explanation", tc.args, tc.minArgs, err)
			}
		})
	}
}

func TestSplitAddKeywordsMatchesSplitKeywords(t *testing.T) {
	// `bmo add` requires the source, so harness-shaped tokens demote to it
	// exactly like remove's skill name: `bmo add codex` installs ./codex.
	for _, args := range [][]string{{"owner/repo", "codex"}, {"everyone"}, {"codex"}, {"here", "owner/repo"}, {"codex", "gemini"}} {
		rest, scope, harness, err := splitAddKeywords(args)
		wantRest, wantScope, wantHarness, wantErr := splitKeywords(args, 1)
		if err != wantErr || scope != wantScope || harness != wantHarness || strings.Join(rest, ",") != strings.Join(wantRest, ",") {
			t.Fatalf("splitAddKeywords(%v) = %v, %q, %q, %v", args, rest, scope, harness, err)
		}
	}
	rest, _, harness, err := splitAddKeywords([]string{"codex"})
	if err != nil || harness != "" || strings.Join(rest, ",") != "codex" {
		t.Fatalf("splitAddKeywords([codex]) = %v, %q, %v; want the token kept as the source", rest, harness, err)
	}
}

func TestSplitAddKeywords(t *testing.T) {
	cases := []struct {
		name, scope, harness string
		args, rest           []string
		wantErr              bool
	}{
		{"positional harness", "", "codex", []string{"owner/repo", "codex"}, []string{"owner/repo"}, false},
		{"everyone and project", "here", "everyone", []string{"everyone", "owner/repo", "here"}, []string{"owner/repo"}, false},
		{"source only", "", "", []string{"owner/repo"}, []string{"owner/repo"}, false},
		{"two harnesses", "", "", []string{"owner/repo", "codex", "gemini"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest, scope, harness, err := splitAddKeywords(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil || scope != tc.scope || harness != tc.harness || strings.Join(rest, ",") != strings.Join(tc.rest, ",") {
				t.Fatalf("splitAddKeywords(%v) = %v, %q, %q, %v", tc.args, rest, scope, harness, err)
			}
		})
	}
}

func TestSplitAddKeywordsAcceptsEveryOrdering(t *testing.T) {
	orders := [][]string{
		{"owner/repo", "everywhere", "everyone"},
		{"owner/repo", "everyone", "everywhere"},
		{"everywhere", "owner/repo", "everyone"},
		{"everywhere", "everyone", "owner/repo"},
		{"everyone", "owner/repo", "everywhere"},
		{"everyone", "everywhere", "owner/repo"},
	}
	for _, args := range orders {
		rest, scope, harness, err := splitAddKeywords(args)
		if err != nil || strings.Join(rest, ",") != "owner/repo" || scope != "everywhere" || harness != "everyone" {
			t.Fatalf("splitAddKeywords(%v) = %v, %q, %q, %v", args, rest, scope, harness, err)
		}
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
		{"global flag without keyword", "", &options{global: true}, bmo.ScopeGlobal},
		{"here wins over global flag", "here", &options{global: true}, bmo.ScopeProject},
		{"everywhere wins over project flag", "everywhere", &options{project: true}, bmo.ScopeGlobal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := keywordScope(tc.keyword, tc.opts); got != tc.want {
				t.Fatalf("keywordScope(%q, %+v) = %v, want %v", tc.keyword, tc.opts, got, tc.want)
			}
		})
	}
}

func TestListEntriesSorted(t *testing.T) {
	isolateHome(t)
	cwd := t.TempDir()

	globalPath, err := bmo.GlobalMetadataPath()
	if err != nil {
		t.Fatal(err)
	}
	global := bmo.EmptyMetadata()
	global.Skills["zeta"] = bmo.SkillMeta{Name: "zeta", Scope: bmo.ScopeGlobal}
	global.Skills["alpha"] = bmo.SkillMeta{Name: "alpha", Scope: bmo.ScopeGlobal}
	if err := bmo.WriteMetadata(globalPath, global); err != nil {
		t.Fatal(err)
	}
	project := bmo.EmptyMetadata()
	project.Skills["beta"] = bmo.SkillMeta{Name: "beta", Scope: bmo.ScopeProject}
	if err := bmo.WriteMetadata(bmo.ProjectMetadataPath(cwd), project); err != nil {
		t.Fatal(err)
	}

	entries, err := listEntries(cwd, &options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "zeta", "beta"}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, name := range want {
		if entries[i].Name != name {
			t.Fatalf("entries[%d].Name = %q, want %q", i, entries[i].Name, name)
		}
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

	skill := []byte("---\nname: bmo\ndescription: Use when the user wants to install portable coding-agent skills with the bmo CLI.\n---\n\n# bmo\n")
	bmo.SetEmbeddedFS(fstest.MapFS{
		"SKILL.md": &fstest.MapFile{Data: skill},
	})
	t.Cleanup(func() { bmo.SetEmbeddedFS(nil) })
	return home
}

// bootstrapForTest runs the first-run install exactly as PersistentPreRun does.
func bootstrapForTest(cmd *cobra.Command) {
	bootstrapBmoSkillForOptions(cmd, nil, &options{})
}

// bmoSkillTrackedGlobally reports whether global Claude metadata records the
// bundled skill.
func bmoSkillTrackedGlobally(t *testing.T) bool {
	t.Helper()
	metaPath, err := bmo.GlobalMetadataPath()
	if err != nil {
		t.Fatal(err)
	}
	meta, err := bmo.ReadMetadata(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := meta.Skills[bmo.EmbeddedSkillName]
	return ok
}

func TestBootstrapBmoSkillInstallsAndMarks(t *testing.T) {
	home := isolateHome(t)

	cmd := &cobra.Command{Use: "list"}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	bootstrapForTest(cmd)

	marker := filepath.Join(home, ".bmo", ".bootstrapped")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected bootstrap marker at %s: %v", marker, err)
	}

	installed := filepath.Join(home, ".claude", "skills", bmo.EmbeddedSkillName, "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("expected installed skill at %s: %v", installed, err)
	}

	if !bmoSkillTrackedGlobally(t) {
		t.Fatalf("expected bmo skill to be tracked in metadata")
	}

	if stderr.Len() == 0 {
		t.Fatalf("expected a bootstrap message on stderr")
	}
}

func TestInitInstallsBundledSkillForCodex(t *testing.T) {
	home := isolateHome(t)
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--global", "--harness", "codex"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(home, ".agents", "skills", bmo.EmbeddedSkillName, "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("expected Codex skill at %s: %v", installed, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".bmo", ".bootstrapped-codex")); err != nil {
		t.Fatalf("expected Codex-specific bootstrap marker: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", bmo.EmbeddedSkillName)); !os.IsNotExist(err) {
		t.Fatalf("Codex init should not install into Claude: %v", err)
	}
}

func TestInitAcceptsChatGPTAlias(t *testing.T) {
	home := isolateHome(t)
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "chatgpt"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(home, ".agents", "skills", bmo.EmbeddedSkillName, "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("expected ChatGPT alias to install at the shared Codex path: %v", err)
	}
}

func TestChatGPTAliasUsesChatGPTInvocationHint(t *testing.T) {
	home := isolateHome(t)
	source := t.TempDir()
	writeSourceSkill(t, source, "alpha")

	out, err := runBmo(t, home, "add", source, "chatgpt", "--yes")
	if err != nil {
		t.Fatalf("ChatGPT install failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Harness: chatgpt") || !strings.Contains(out, "@alpha") {
		t.Fatalf("ChatGPT alias did not retain its surface-specific guidance:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".bmo", "codex-skills.json")); err != nil {
		t.Fatalf("ChatGPT alias did not use canonical Codex metadata: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".bmo", "chatgpt-skills.json")); !os.IsNotExist(err) {
		t.Fatalf("ChatGPT alias created split metadata: %v", err)
	}
}

func TestDryRunDoesNotBootstrapOrWrite(t *testing.T) {
	home := isolateHome(t)
	source := t.TempDir()
	writeSourceSkill(t, source, "alpha")

	out, err := runBmo(t, home, "add", source, "codex", "--dry-run", "--yes")
	if err != nil {
		t.Fatalf("dry run failed: %v\n%s", err, out)
	}
	for _, path := range []string{
		filepath.Join(home, ".bmo", ".bootstrapped-codex"),
		filepath.Join(home, ".bmo", "codex-skills.json"),
		filepath.Join(home, ".agents", "skills", bmo.EmbeddedSkillName),
		filepath.Join(home, ".agents", "skills", "alpha"),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("--dry-run wrote %s: %v\n%s", path, statErr, out)
		}
	}
}

func TestHarnessAwareCommandsAcceptPositionalHarness(t *testing.T) {
	home := isolateHome(t)
	t.Chdir(t.TempDir()) // doctor probes the project dir; keep it out of the repo

	if out, err := runBmo(t, home, "init", "codex"); err != nil {
		t.Fatalf("bmo init codex failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", bmo.EmbeddedSkillName, "SKILL.md")); err != nil {
		t.Fatalf("expected the positional harness to install for Codex: %v", err)
	}

	out, err := runBmo(t, home, "list", "everywhere", "codex", "--json")
	if err != nil {
		t.Fatalf("bmo list everywhere codex failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, bmo.EmbeddedSkillName) {
		t.Fatalf("expected Codex metadata in the listing:\n%s", out)
	}

	out, err = runBmo(t, home, "doctor", "codex")
	if err != nil {
		t.Fatalf("bmo doctor codex failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, filepath.Join(home, ".agents", "skills")) {
		t.Fatalf("expected doctor to report Codex paths:\n%s", out)
	}
	if strings.Contains(out, filepath.Join(home, ".claude", "skills")) {
		t.Fatalf("doctor answered for Claude on a Codex run:\n%s", out)
	}
}

func TestRemoveReadsALoneHarnessNameAsTheSkill(t *testing.T) {
	home := isolateHome(t)
	t.Chdir(t.TempDir())
	src := t.TempDir()
	writeSourceSkill(t, src, "codex")

	if out, err := runBmo(t, home, "add", src, "--yes"); err != nil {
		t.Fatalf("add failed: %v\n%s", err, out)
	}
	out, err := runBmo(t, home, "remove", "codex", "--yes")
	if err != nil {
		t.Fatalf("bmo remove codex failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed codex") {
		t.Fatalf("expected the skill named codex to be removed:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "codex")); !os.IsNotExist(err) {
		t.Fatalf("expected the Claude install to be gone: %v", err)
	}
}

func TestRemoveAcceptsPositionalHarnessBesideTheSkillName(t *testing.T) {
	home := isolateHome(t)
	t.Chdir(t.TempDir())
	src := t.TempDir()
	writeSourceSkill(t, src, "demo")

	if out, err := runBmo(t, home, "add", src, "codex", "--yes"); err != nil {
		t.Fatalf("add failed: %v\n%s", err, out)
	}
	out, err := runBmo(t, home, "remove", "demo", "codex", "--yes")
	if err != nil {
		t.Fatalf("bmo remove demo codex failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "demo")); !os.IsNotExist(err) {
		t.Fatalf("expected the Codex install to be gone: %v", err)
	}
}

func TestAddEveryoneWarnsAboutExecutableFiles(t *testing.T) {
	home := isolateHome(t)
	t.Chdir(t.TempDir())
	t.Setenv("PATH", t.TempDir()) // detect harnesses by config dir alone
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")
	if err := os.WriteFile(filepath.Join(src, "skills", "alpha", "setup.sh"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runBmo(t, home, "add", src, "everyone", "--dry-run")
	if err != nil {
		t.Fatalf("add everyone failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "These skills include executable-looking files:") ||
		!strings.Contains(out, "- alpha") ||
		!strings.Contains(out, "Skills may include executable code. Review third-party skills before use.") {
		t.Fatalf("expected the everyone preview to warn about executable files:\n%s", out)
	}
	if !strings.Contains(out, filepath.Join(home, ".agents", "skills")) {
		t.Fatalf("expected the destination summary to survive:\n%s", out)
	}
}

func TestHarnessAwareCommandsRejectBadArguments(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"doctor rejects a stray argument", []string{"doctor", "bogus-arg"}, "unknown command"},
		{"list rejects a stray argument", []string{"list", "bogus-arg"}, "unknown command"},
		{"init rejects everyone", []string{"init", "everyone"}, "bmo add, bmo remove, bmo update, and bmo share"},
		{"list rejects everyone", []string{"list", "everyone"}, "bmo add, bmo remove, bmo update, and bmo share"},
		// remove requires a name, so "everyone" is read as the skill to remove.
		{"remove reads everyone as the skill name", []string{"remove", "everyone"}, "not tracked"},
		{"doctor rejects everyone", []string{"doctor", "everyone"}, "bmo add, bmo remove, bmo update, and bmo share"},
		{"update everyone rejects --harness", []string{"update", "everyone", "--harness", "codex"}, "already covers every harness"},
		{"share everyone rejects --harness", []string{"share", "everyone", "--harness", "codex"}, "already covers every harness"},
		{"positional harness beside --harness", []string{"init", "codex", "--harness", "gemini"}, "cannot be combined"},
		{"positional harness beside --skills-dir", []string{"list", "codex", "--skills-dir", "sk"}, "cannot be combined"},
		{"list rejects both scope flags", []string{"list", "--project", "--global"}, "none of the others"},
		{"update rejects both scope flags", []string{"update", "--project", "--global"}, "none of the others"},
		{"remove rejects both scope flags", []string{"remove", "demo", "--project", "--global"}, "none of the others"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := isolateHome(t)
			t.Chdir(t.TempDir())
			out, err := runBmo(t, home, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("bmo %s error = %v, want one containing %q\n%s", strings.Join(tc.args, " "), err, tc.want, out)
			}
		})
	}
}

func TestHarnessesCommandListsCodexAndCustomEscapeHatch(t *testing.T) {
	isolateHome(t)
	cmd := NewRootCommand()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"harnesses"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "codex") || !strings.Contains(got, ".agents/skills") || !strings.Contains(got, "bmo add SOURCE everyone") || !strings.Contains(got, "--skills-dir PATH") {
		t.Fatalf("unexpected harness list:\n%s", got)
	}
	// The table is rendered through tabwriter, so no raw tabs survive.
	if strings.Contains(got, "\t") {
		t.Fatalf("expected an aligned table without raw tabs:\n%s", got)
	}
}

func TestBootstrapBmoSkillIdempotent(t *testing.T) {
	home := isolateHome(t)

	first := &cobra.Command{Use: "list"}
	var firstErr bytes.Buffer
	first.SetErr(&firstErr)
	bootstrapForTest(first)
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
	bootstrapForTest(second)

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
	bootstrapForTest(cmd)

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

func TestUpdateSkipsUnchangedAndUpdatesChanged(t *testing.T) {
	isolateHome(t)
	cwd := t.TempDir()

	srcDir := filepath.Join(t.TempDir(), "demo")
	skillMD := filepath.Join(srcDir, "SKILL.md")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillMD, []byte("---\nname: demo\ndescription: d\n---\nv1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	src, err := bmo.ParseSource(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	skill, err := bmo.ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	installed, err := bmo.InstallSkill(bmo.InstallOptions{Scope: bmo.ScopeGlobal, CWD: cwd, Source: src, Skill: skill})
	if err != nil {
		t.Fatal(err)
	}

	runUpdate := func() string {
		t.Helper()
		out := &bytes.Buffer{}
		cmd := &cobra.Command{}
		cmd.SetOut(out)
		cache := map[string]sourceResolution{}
		defer func() {
			for _, res := range cache {
				cleanupResolved(res.resolved)
			}
		}()
		if err := updateScope(cmd, cwd, bmo.ScopeGlobal, nil, &options{all: true}, cache); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	if got := runUpdate(); got != "demo is up to date\n" {
		t.Fatalf("expected unchanged skill to be skipped, got %q", got)
	}

	if err := os.WriteFile(skillMD, []byte("---\nname: demo\ndescription: d\n---\nv2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runUpdate(); got != "Updated demo\n" {
		t.Fatalf("expected changed skill to be updated, got %q", got)
	}

	data, err := os.ReadFile(filepath.Join(installed.InstalledPath, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("v2")) {
		t.Fatalf("expected installed copy to carry the new content, got %q", data)
	}

	if got := runUpdate(); got != "demo is up to date\n" {
		t.Fatalf("expected second update after refresh to be a no-op, got %q", got)
	}
}

func TestUpdateEverywhereReachesRegisteredProjects(t *testing.T) {
	isolateHome(t)
	cwd := t.TempDir() // where the command runs; not itself a bmo project

	// Source skill, installed into a separate project directory.
	srcDir := filepath.Join(t.TempDir(), "demo")
	skillMD := filepath.Join(srcDir, "SKILL.md")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillMD, []byte("---\nname: demo\ndescription: d\n---\nv1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	src, err := bmo.ParseSource(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	skill, err := bmo.ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bmo.InstallSkill(bmo.InstallOptions{Scope: bmo.ScopeProject, CWD: project, Source: src, Skill: skill}); err != nil {
		t.Fatal(err)
	}

	// Installing into a project scope must have recorded the project.
	projects, err := bmo.RegisteredProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0] != project {
		t.Fatalf("expected registry to hold %q, got %v", project, projects)
	}

	runEverywhere := func() string {
		t.Helper()
		out := &bytes.Buffer{}
		cmd := &cobra.Command{}
		cmd.SetOut(out)
		cache := map[string]sourceResolution{}
		defer func() {
			for _, res := range cache {
				cleanupResolved(res.resolved)
			}
		}()
		if err := updateEverywhere(cmd, cwd, nil, &options{all: true}, cache); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	out := runEverywhere()
	if !strings.Contains(out, project+" (claude):") || !strings.Contains(out, "demo is up to date") {
		t.Fatalf("expected registered project to be visited, got %q", out)
	}

	// Change the source: everywhere must update the project install.
	if err := os.WriteFile(skillMD, []byte("---\nname: demo\ndescription: d\n---\nv2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = runEverywhere()
	if !strings.Contains(out, "Updated demo") {
		t.Fatalf("expected changed skill in registered project to update, got %q", out)
	}
	data, err := os.ReadFile(filepath.Join(project, ".claude", "skills", "demo", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("v2")) {
		t.Fatalf("expected project install to carry new content, got %q", data)
	}
}

func TestUpdateEverywhereSkipsMissingProjects(t *testing.T) {
	isolateHome(t)
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bmo.RecordProject(gone); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	if err := updateEverywhere(cmd, t.TempDir(), nil, &options{all: true}, map[string]sourceResolution{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Skipping "+gone) {
		t.Fatalf("expected missing project to be skipped with a note, got %q", out.String())
	}
}

// TestUpdateEverywhereEveryoneSweepsEveryHarnessAndProject covers the widest
// update bmo performs: every harness, in every project the registry knows,
// from a directory that is none of them.
func TestUpdateEverywhereEveryoneSweepsEveryHarnessAndProject(t *testing.T) {
	home := isolateHome(t)
	claudeProject := t.TempDir()
	codexProject := t.TempDir()
	installIntoProject(t, claudeProject, "claude", "demo")
	installIntoProject(t, codexProject, "codex", "other")
	t.Chdir(t.TempDir())

	out, err := runBmo(t, home, "update", "everywhere", "everyone")
	if err != nil {
		t.Fatalf("bmo update everywhere everyone: %v\n%s", err, out)
	}
	if !strings.Contains(out, claudeProject+" (claude):") || !strings.Contains(out, "demo is up to date") {
		t.Fatalf("expected the claude project visited, got %q", out)
	}
	if !strings.Contains(out, codexProject+" (codex):") || !strings.Contains(out, "other is up to date") {
		t.Fatalf("expected the codex project visited, got %q", out)
	}
}

// TestUpdateEveryoneCoversEveryHarnessInThisDirectory is the narrower form:
// no "everywhere", so it stays with the destinations the current directory
// resolves to — but still visits every harness rather than just Claude.
func TestUpdateEveryoneCoversEveryHarnessInThisDirectory(t *testing.T) {
	home := isolateHome(t)
	project := t.TempDir()
	installIntoProject(t, project, "claude", "demo")
	installIntoProject(t, project, "codex", "other")
	elsewhere := t.TempDir()
	installIntoProject(t, elsewhere, "claude", "not-here")
	t.Chdir(project)

	out, err := runBmo(t, home, "update", "here", "everyone")
	if err != nil {
		t.Fatalf("bmo update here everyone: %v\n%s", err, out)
	}
	if !strings.Contains(out, "demo is up to date") || !strings.Contains(out, "other is up to date") {
		t.Fatalf("expected both harnesses in this project, got %q", out)
	}
	if strings.Contains(out, "not-here") {
		t.Fatalf(`expected "here" to stay in this project, got %q`, out)
	}
}

func TestUpdateEveryoneRejectsASingleDestination(t *testing.T) {
	for _, flag := range [][]string{{"--skills-dir", "sk"}, {"--harness", "codex"}} {
		t.Run(flag[0], func(t *testing.T) {
			home := isolateHome(t)
			t.Chdir(t.TempDir())
			args := append([]string{"update", "everyone"}, flag...)
			out, err := runBmo(t, home, args...)
			if err == nil || !strings.Contains(err.Error(), "already covers every harness") {
				t.Fatalf("bmo update everyone %s error = %v, want a refusal\n%s", flag[0], err, out)
			}
		})
	}
}

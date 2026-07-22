package bmo

import (
	"os"
	"path/filepath"
	"testing"
)

func writeIgnore(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, IgnoreFileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFileAt(t *testing.T, dir, rel, content string) {
	t.Helper()
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIgnoreMatching(t *testing.T) {
	dir := t.TempDir()
	writeIgnore(t, dir, `
# comments and blank lines are skipped

tests/
*.gif
/build
docs/**/*.png
*.svg
!keep.svg
`)
	ignore, err := LoadIgnore(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		rel   string
		isDir bool
		want  bool
	}{
		{rel: "SKILL.md", want: false},
		{rel: "tests", isDir: true, want: true},
		{rel: "tests/test_thing.py", want: true},
		{rel: "nested/tests/deep.py", want: true},    // bare name matches at any depth
		{rel: "tests.md", want: false},               // not the same path segment
		{rel: "demo.gif", want: true},                // glob on basename
		{rel: "screenshots/demo.gif", want: true},    // at any depth
		{rel: "build", isDir: true, want: true},      // anchored to the root
		{rel: "sub/build", isDir: true, want: false}, /* anchored, so not here */
		{rel: "docs/a/b/diagram.png", want: true},    // ** spans segments
		{rel: "docs/diagram.png", want: true},        // ** also spans zero segments
		{rel: "assets/cover.svg", want: true},
		{rel: "keep.svg", want: false},        // later negation wins
		{rel: "assets/keep.svg", want: false}, // negation matches basename too
	}
	for _, tc := range cases {
		if got := ignore.Match(tc.rel, tc.isDir); got != tc.want {
			t.Errorf("Match(%q, isDir=%v) = %v, want %v", tc.rel, tc.isDir, got, tc.want)
		}
	}
}

func TestIgnoreCannotExcludeSkillMd(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "demo")
	writeIgnore(t, dir, "*.md\n")
	ignore, err := LoadIgnore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ignore.Match("SKILL.md", false) {
		t.Fatal("SKILL.md must never be ignorable")
	}
	// Other markdown in the skill still obeys the rule.
	if !ignore.Match("references/notes.md", false) {
		t.Fatal("expected references/notes.md to be ignored")
	}
	if _, err := ValidateSkill(dir, ""); err != nil {
		t.Fatalf("a skill that ignores *.md must still validate: %v", err)
	}
}

func TestIgnoreNegationCannotRescueIgnoredParent(t *testing.T) {
	dir := t.TempDir()
	// Matches git: once a directory is excluded, nothing inside comes back.
	writeIgnore(t, dir, "vendor/\n!vendor/keep.txt\n")
	ignore, err := LoadIgnore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ignore.Match("vendor/keep.txt", false) {
		t.Fatal("a file under an ignored directory must stay ignored")
	}
}

func TestIgnoreRejectsEmptyPattern(t *testing.T) {
	dir := t.TempDir()
	writeIgnore(t, dir, "docs/\n!\n")
	if _, err := LoadIgnore(dir); err == nil {
		t.Fatal("expected an error for an empty pattern")
	}
}

func TestMissingIgnoreFileMatchesNothing(t *testing.T) {
	dir := t.TempDir()
	ignore, err := LoadIgnore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ignore.Match("anything.txt", false) || ignore.Len() != 0 {
		t.Fatal("expected an absent .bmoignore to be inert")
	}
}

func TestInstallSkipsIgnoredPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	writeIgnore(t, srcDir, "tests/\n*.gif\n")
	writeFileAt(t, srcDir, "references/guide.md", "keep me")
	writeFileAt(t, srcDir, "tests/test_demo.py", "drop me")
	writeFileAt(t, srcDir, "screenshots/demo.gif", "drop me")
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if skill.IgnoreRules != 2 {
		t.Fatalf("expected 2 ignore rules, got %d", skill.IgnoreRules)
	}
	// SKILL.md, .bmoignore, references/guide.md
	if skill.FileCount != 3 {
		t.Fatalf("expected 3 counted files, got %d", skill.FileCount)
	}
	meta, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"SKILL.md", "references/guide.md"} {
		if _, err := os.Stat(filepath.Join(meta.InstalledPath, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s installed: %v", rel, err)
		}
	}
	for _, rel := range []string{"tests/test_demo.py", "screenshots/demo.gif"} {
		if _, err := os.Stat(filepath.Join(meta.InstalledPath, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("expected %s excluded, got %v", rel, err)
		}
	}
	// The rules ship with the skill so the installed tree explains itself and
	// hashing sees the same inputs on both sides.
	if _, err := os.Stat(filepath.Join(meta.InstalledPath, IgnoreFileName)); err != nil {
		t.Fatalf("expected .bmoignore to be installed: %v", err)
	}
}

func TestHashIgnoresExcludedFiles(t *testing.T) {
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	writeIgnore(t, srcDir, "tests/\n")
	writeFileAt(t, srcDir, "tests/test_demo.py", "first")
	before, err := HashDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	writeFileAt(t, srcDir, "tests/test_demo.py", "changed")
	writeFileAt(t, srcDir, "tests/test_more.py", "added")
	after, err := HashDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("excluded files must not affect the content hash")
	}
	writeFileAt(t, srcDir, "references/guide.md", "included")
	changed, err := HashDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	if changed == after {
		t.Fatal("an included file must affect the content hash")
	}
}

// An installed skill and its source must hash identically, or `bmo update`
// would reinstall on every run.
func TestInstalledCopyHashesEqualToSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	writeIgnore(t, srcDir, "tests/\n*.gif\n")
	writeFileAt(t, srcDir, "references/guide.md", "keep me")
	writeFileAt(t, srcDir, "tests/test_demo.py", "drop me")
	writeFileAt(t, srcDir, "demo.gif", "drop me")
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	sourceHash, err := HashDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	installedHash, err := HashDir(meta.InstalledPath)
	if err != nil {
		t.Fatal(err)
	}
	if sourceHash != installedHash {
		t.Fatal("source and installed hashes must match so update stays a no-op")
	}
}

func TestIgnoredAgentIsNotInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	writeAgent(t, srcDir, "alpha.md", agentDoc("alpha", "Kept specialist."))
	writeAgent(t, srcDir, "draft.md", agentDoc("draft", "Work in progress."))
	writeIgnore(t, srcDir, "agents/draft.md\n")
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if names := AgentNames(skill.Agents); len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("expected only alpha to be discovered, got %v", names)
	}
	meta, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	agentsDir, err := AgentsDir(ScopeGlobal, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "draft.md")); !os.IsNotExist(err) {
		t.Fatal("an ignored agent must not be installed")
	}
	if len(meta.Agents) != 1 {
		t.Fatalf("expected one tracked agent, got %v", meta.Agents)
	}
	// Doctor must not report the ignored agent as missing.
	if warnings := doctorMessages(RunDoctor(cwd), DoctorWarning); len(warnings) > 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
}

func TestDiscoverSkillsHonorsRepoIgnore(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "real"), "real")
	writeSkill(t, filepath.Join(root, "examples", "sample"), "sample")
	skills, err := DiscoverSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills before ignoring, got %d", len(skills))
	}
	writeIgnore(t, root, "examples/\n")
	skills, err = DiscoverSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Name != "real" {
		t.Fatalf("expected only the real skill, got %v", skills)
	}
}

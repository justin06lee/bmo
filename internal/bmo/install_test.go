package bmo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallToFakeClaudeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(meta.InstalledPath, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallRefusesOverwriteUnlessForce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	opts := InstallOptions{Scope: ScopeGlobal, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd}
	if _, err := InstallSkill(opts); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkill(opts); err == nil {
		t.Fatal("expected overwrite error")
	}
	opts.Force = true
	if _, err := InstallSkill(opts); err != nil {
		t.Fatal(err)
	}
}

// A dry run reports what would happen and writes nothing — including over an
// existing install, where the real no-force command would fail. The CLI's
// preview deliberately lets --dry-run through its own already-installed check.
func TestDryRunOverExistingInstallReportsWithoutWriting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	opts := InstallOptions{Scope: ScopeGlobal, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd}
	installed, err := InstallSkill(opts)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(installed.InstalledPath, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	opts.DryRun = true
	meta, err := InstallSkill(opts)
	if err != nil {
		t.Fatalf("dry run over an existing install must report, not fail: %v", err)
	}
	if meta.InstalledPath != installed.InstalledPath {
		t.Fatalf("dry run reported the wrong destination: %q", meta.InstalledPath)
	}
	after, err := os.ReadFile(filepath.Join(installed.InstalledPath, "SKILL.md"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("dry run must not touch the installed copy: %v", err)
	}
	// The real command still refuses without --force and succeeds with it.
	opts.DryRun = false
	if _, err := InstallSkill(opts); err == nil {
		t.Fatal("expected overwrite error without --force")
	}
	opts.Force = true
	if _, err := InstallSkill(opts); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveInstalledSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := removeSkillGlobal(t, "demo", cwd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(meta.InstalledPath); !os.IsNotExist(err) {
		t.Fatalf("expected removed path, got %v", err)
	}
}

// removeSkillGlobal removes a skill from the default Claude global target, the
// way the CLI resolves it.
func removeSkillGlobal(t *testing.T, name, cwd string) (SkillMeta, error) {
	t.Helper()
	target, err := ResolveTarget("", ScopeGlobal, cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	return RemoveSkillFromTarget(name, target)
}

// A removal bmo refuses must change nothing: the guard used to run after the
// skill directory had already been deleted, leaving metadata that pointed at
// files the user could no longer restore.
func TestRemoveRejectedByAgentsGuardKeepsSkillAndMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	target, err := ResolveTarget("codex", ScopeGlobal, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(target.SkillsDir, "demo")
	writeSkill(t, installed, "demo")
	meta := EmptyMetadata()
	meta.Skills["demo"] = SkillMeta{
		Name:          "demo",
		Scope:         ScopeGlobal,
		Harness:       HarnessCodex,
		InstalledPath: installed,
		Agents:        []string{"worker.md"},
	}
	if err := WriteMetadata(target.MetadataPath, meta); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveSkillFromTarget("demo", target); err == nil {
		t.Fatal("expected the agents guard to reject the removal")
	}
	if _, err := os.Stat(filepath.Join(installed, "SKILL.md")); err != nil {
		t.Fatalf("installed skill must survive a rejected removal: %v", err)
	}
	after, err := ReadMetadata(target.MetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.Skills["demo"]; !ok {
		t.Fatal("metadata entry must survive a rejected removal")
	}
}

// A target with no agents destination has an empty AgentsDir, so pruning stale
// subagents there would resolve tracked filenames against the process working
// directory and delete unrelated files.
func TestInstallDoesNotPruneStaleAgentsWithoutAgentsSupport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	workdir := t.TempDir()
	bystander := filepath.Join(workdir, "worker.md")
	if err := os.WriteFile(bystander, []byte("not bmo's file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workdir)

	srcDir := t.TempDir()
	writeSkill(t, srcDir, "portable")
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	target, err := ResolveTarget("codex", ScopeGlobal, workdir, "")
	if err != nil {
		t.Fatal(err)
	}
	opts := InstallOptions{
		Scope: ScopeGlobal, Target: target, CWD: workdir,
		Source: Source{Raw: srcDir, Type: SourceLocal}, Skill: skill,
	}
	if _, err := InstallSkill(opts); err != nil {
		t.Fatal(err)
	}
	// Stand in for metadata written before the harness abstraction, or edited by
	// hand: it claims a subagent this harness cannot own.
	meta, err := ReadMetadata(target.MetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := meta.Skills["portable"]
	entry.Agents = []string{"worker.md"}
	meta.Skills["portable"] = entry
	if err := WriteMetadata(target.MetadataPath, meta); err != nil {
		t.Fatal(err)
	}

	opts.Force = true
	if _, err := InstallSkill(opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bystander); err != nil {
		t.Fatalf("install deleted an unrelated file in the working directory: %v", err)
	}
}

func TestWithinDir(t *testing.T) {
	cases := []struct {
		name    string
		parent  string
		target  string
		wantErr bool
	}{
		{name: "inside", parent: "/a/b", target: "/a/b/c", wantErr: false},
		{name: "equal", parent: "/a/b", target: "/a/b", wantErr: false},
		{name: "outside", parent: "/a/b", target: "/a/c", wantErr: true},
		{name: "sibling-prefix", parent: "/a/b", target: "/a/bc", wantErr: true},
		{name: "empty target", parent: "/a/b", target: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := withinDir(tc.parent, tc.target)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for parent=%q target=%q, got nil", tc.parent, tc.target)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for parent=%q target=%q: %v", tc.parent, tc.target, err)
			}
		})
	}
}

// A project moved after installation leaves metadata pointing outside the
// destination bmo now resolves. The copy really sitting in the skills
// directory is the one to delete — the recorded path is never touched.
func TestRemoveDeletesTheRelocatedCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	project := t.TempDir()
	target, err := ResolveTarget("", ScopeProject, project, "")
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(target.SkillsDir, "demo")
	writeSkill(t, installed, "demo")
	// Somewhere else entirely: where the project used to live.
	stale := filepath.Join(t.TempDir(), "old", "skills", "demo")
	writeSkill(t, stale, "demo")

	meta := EmptyMetadata()
	meta.Skills["demo"] = SkillMeta{Name: "demo", Scope: ScopeProject, InstalledPath: stale}
	if err := WriteMetadata(target.MetadataPath, meta); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveSkillFromTarget("demo", target); err != nil {
		t.Fatalf("relocated removal failed: %v", err)
	}
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("expected the copy inside the skills directory to be deleted, got %v", err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("the recorded path is outside the skills directory and must survive: %v", err)
	}
	after, err := ReadMetadata(target.MetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.Skills["demo"]; ok {
		t.Fatal("expected the metadata entry to be dropped")
	}
}

// With nothing left inside the skills directory, the entry is the only thing
// still calling the skill installed. It is untracked, and nothing outside the
// managed directory is deleted to get there.
func TestRemoveUntracksAnEntryWithNoCopyLeft(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	target, err := ResolveTarget("", ScopeGlobal, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "elsewhere")
	writeSkill(t, outside, "demo")
	meta := EmptyMetadata()
	meta.Skills["demo"] = SkillMeta{Name: "demo", Scope: ScopeGlobal, InstalledPath: outside}
	if err := WriteMetadata(target.MetadataPath, meta); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveSkillFromTarget("demo", target); err != nil {
		t.Fatalf("stale-entry removal failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "SKILL.md")); err != nil {
		t.Fatalf("files outside the skills directory must never be deleted: %v", err)
	}
	after, err := ReadMetadata(target.MetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.Skills["demo"]; ok {
		t.Fatal("expected the stale entry to be dropped")
	}
}

// A symlink in the skills directory is not a skill bmo installed, and
// following it would reach outside the managed directory after all.
func TestTrackedSkillPathRefusesASymlinkedRelocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	target, err := ResolveTarget("", ScopeGlobal, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(t.TempDir(), "real")
	writeSkill(t, real, "demo")
	if err := os.MkdirAll(target.SkillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(target.SkillsDir, "demo")); err != nil {
		t.Fatal(err)
	}
	entry := SkillMeta{Name: "demo", InstalledPath: filepath.Join(t.TempDir(), "gone", "demo")}
	if path, ok := TrackedSkillPath("demo", entry, target); ok {
		t.Fatalf("TrackedSkillPath followed a symlink to %q", path)
	}
}

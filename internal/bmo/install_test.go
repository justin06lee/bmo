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

func TestDryRunReportsAlreadyInstalledConflict(t *testing.T) {
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
	opts.DryRun = true
	if _, err := InstallSkill(opts); err == nil {
		t.Fatal("expected dry-run overwrite error")
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
	if _, err := RemoveSkill("demo", ScopeGlobal, cwd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(meta.InstalledPath); !os.IsNotExist(err) {
		t.Fatalf("expected removed path, got %v", err)
	}
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

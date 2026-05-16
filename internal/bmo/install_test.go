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

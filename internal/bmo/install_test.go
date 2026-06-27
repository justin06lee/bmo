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

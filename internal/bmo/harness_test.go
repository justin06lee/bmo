package bmo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarnessTargetsResolveDocumentedPaths(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	cases := []struct {
		name          string
		projectSuffix string
		globalSuffix  string
	}{
		{"claude", ".claude/skills", ".claude/skills"},
		{"codex", ".agents/skills", ".agents/skills"},
		{"cursor", ".cursor/skills", ".cursor/skills"},
		{"gemini", ".gemini/skills", ".gemini/skills"},
		{"copilot", ".github/skills", ".copilot/skills"},
		{"windsurf", ".windsurf/skills", ".codeium/windsurf/skills"},
		{"opencode", ".opencode/skills", ".config/opencode/skills"},
		{"amp", ".agents/skills", ".config/agents/skills"},
		{"cline", ".cline/skills", ".cline/skills"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectTarget, err := ResolveTarget(tc.name, ScopeProject, project, "")
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(project, filepath.FromSlash(tc.projectSuffix)); projectTarget.SkillsDir != want {
				t.Fatalf("project skills dir = %q, want %q", projectTarget.SkillsDir, want)
			}
			globalTarget, err := ResolveTarget(tc.name, ScopeGlobal, project, "")
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(home, filepath.FromSlash(tc.globalSuffix)); globalTarget.SkillsDir != want {
				t.Fatalf("global skills dir = %q, want %q", globalTarget.SkillsDir, want)
			}
		})
	}
}

func TestResolveCustomTarget(t *testing.T) {
	project := t.TempDir()
	target, err := ResolveTarget("", ScopeProject, project, "tools/my-agent/skills")
	if err != nil {
		t.Fatal(err)
	}
	if target.Harness != HarnessCustom {
		t.Fatalf("harness = %q, want custom", target.Harness)
	}
	wantDir := filepath.Join(project, "tools", "my-agent", "skills")
	if target.SkillsDir != wantDir {
		t.Fatalf("skills dir = %q, want %q", target.SkillsDir, wantDir)
	}
	if target.MetadataPath != filepath.Join(project, "tools", "my-agent", "bmo-lock.json") {
		t.Fatalf("unexpected metadata path: %q", target.MetadataPath)
	}
	if _, err := ResolveTarget("codex", ScopeProject, project, wantDir); err == nil {
		t.Fatal("expected --harness with --skills-dir to fail")
	}
}

func TestDetectedHarnessesUsesExecutablesAndConfigDirectories(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	if err := os.MkdirAll(filepath.Join(home, ".gemini"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	detected := DetectedHarnesses()
	if len(detected) != 2 || detected[0].Name != HarnessCodex || detected[1].Name != HarnessGemini {
		t.Fatalf("detected harnesses = %+v, want codex then gemini", detected)
	}
}

func TestInstallForCodexDoesNotExportClaudeAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	skillDir := filepath.Join(t.TempDir(), "portable")
	if err := os.MkdirAll(filepath.Join(skillDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: portable\ndescription: Portable test skill.\n---\n\nUse this skill.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "agents", "worker.md"), []byte("---\nname: worker\ndescription: Worker.\n---\n\nWork.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, err := ValidateSkill(skillDir, "")
	if err != nil {
		t.Fatal(err)
	}
	target, err := ResolveTarget("codex", ScopeGlobal, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := InstallSkill(InstallOptions{
		Scope: ScopeGlobal, Target: target, CWD: t.TempDir(),
		Source: Source{Raw: skillDir, Type: SourceLocal}, Skill: skill,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Agents) != 0 {
		t.Fatalf("Codex metadata should not claim Claude agents: %v", meta.Agents)
	}
	if meta.Harness != HarnessCodex {
		t.Fatalf("metadata harness = %q, want codex", meta.Harness)
	}
	if !strings.Contains(meta.InstalledPath, filepath.Join(".agents", "skills", "portable")) {
		t.Fatalf("installed path is not Codex-compatible: %s", meta.InstalledPath)
	}
	if _, err := os.Stat(filepath.Join(meta.InstalledPath, "agents", "worker.md")); err != nil {
		t.Fatalf("bundled agent should remain available as a skill resource: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "agents", "worker.md")); !os.IsNotExist(err) {
		t.Fatalf("Claude agent definition was unexpectedly exported: %v", err)
	}
}

func TestPortableHarnessRequiresDeclaredMatchingName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	target, err := ResolveTarget("codex", ScopeGlobal, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}

	missing := filepath.Join(t.TempDir(), "missing-name")
	if err := os.MkdirAll(missing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missing, "SKILL.md"), []byte("---\ndescription: Missing declared name.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, err := ValidateSkill(missing, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Target: target, Source: Source{Raw: missing, Type: SourceLocal}, Skill: skill}); err == nil || !strings.Contains(err.Error(), "explicit name") {
		t.Fatalf("missing portable name error = %v", err)
	}

	declared := filepath.Join(t.TempDir(), "declared")
	if err := os.MkdirAll(declared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(declared, "SKILL.md"), []byte("---\nname: declared\ndescription: Declared name.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, err = ValidateSkill(declared, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Target: target, Name: "renamed", Source: Source{Raw: declared, Type: SourceLocal}, Skill: skill}); err == nil || !strings.Contains(err.Error(), "match the installed folder") {
		t.Fatalf("renamed portable skill error = %v", err)
	}
}

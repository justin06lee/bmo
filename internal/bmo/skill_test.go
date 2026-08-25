package bmo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverSkillAtRoot(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "root-skill")
	skills, err := DiscoverSkills(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Name != "root-skill" {
		t.Fatalf("unexpected skills: %+v", skills)
	}
}

func TestDiscoverNestedSkills(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "one"), "one")
	writeSkill(t, filepath.Join(dir, "nested", "two"), "two")
	writeSkill(t, filepath.Join(dir, "node_modules", "ignored"), "ignored")
	skills, err := DiscoverSkills(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d: %+v", len(skills), skills)
	}
}

func TestValidateValidSkill(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "valid-skill")
	skill, err := ValidateSkill(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Description == "" || skill.FileCount != 1 {
		t.Fatalf("unexpected skill: %+v", skill)
	}
}

func TestValidateRejectsMissingSkillMD(t *testing.T) {
	_, err := ValidateSkill(t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateRejectsInvalidSkillName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: Bad_Name\ndescription: nope\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ValidateSkill(dir, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

// The base validator keeps the historical Claude grammar so skills installed
// before the portable presets existed keep validating (and updating); the
// strict Agent Skills grammar is enforced per portable target.
func TestValidateSkillNameKeepsLegacyClaudeGrammar(t *testing.T) {
	for _, name := range []string{"UPPER", "under_score", "spa ce", ""} {
		if err := ValidateSkillName(name); err == nil {
			t.Fatalf("ValidateSkillName(%q) = nil, want error", name)
		}
	}
	for _, name := range []string{"a", "portable-skill", "skill-2", "-leading", "trailing-", "two--hyphens"} {
		if err := ValidateSkillName(name); err != nil {
			t.Fatalf("ValidateSkillName(%q) unexpected error: %v", name, err)
		}
	}
}

func TestPortableTargetsRequireSingleHyphenNames(t *testing.T) {
	codex := Target{Harness: HarnessCodex}
	for _, name := range []string{"-leading", "trailing-", "two--hyphens"} {
		skill := Skill{Name: name, DeclaredName: name, Description: "d"}
		if err := ValidateSkillForTarget(skill, codex); err == nil || !strings.Contains(err.Error(), "single hyphens") {
			t.Fatalf("ValidateSkillForTarget(%q, codex) = %v, want single-hyphen error", name, err)
		}
	}
	ok := Skill{Name: "portable-skill", DeclaredName: "portable-skill", Description: "d"}
	if err := ValidateSkillForTarget(ok, codex); err != nil {
		t.Fatalf("unexpected portable validation error: %v", err)
	}
	// Claude — zero-value harness included — retains the legacy grammar.
	legacy := Skill{Name: "two--hyphens", Description: "d"}
	for _, target := range []Target{{Harness: HarnessClaude}, {}} {
		if err := ValidateSkillForTarget(legacy, target); err != nil {
			t.Fatalf("legacy name rejected for %+v: %v", target, err)
		}
	}
}

func TestValidateWarnsOnExecutableLookingFiles(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "scripted")
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('x')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, err := ValidateSkill(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(skill.ExecutableFiles) != 1 {
		t.Fatalf("expected executable warning: %+v", skill)
	}
}

func writeSkill(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: A useful skill.\n---\n# Skill\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

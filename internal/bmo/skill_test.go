package bmo

import (
	"os"
	"path/filepath"
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

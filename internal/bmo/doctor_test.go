package bmo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorDoesNotCreateMissingDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	globalSkills, err := GlobalSkillsDir()
	if err != nil {
		t.Fatal(err)
	}
	projectSkills := ProjectSkillsDir(cwd)
	checks := RunDoctor(cwd)
	for _, dir := range []string{globalSkills, projectSkills} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("expected doctor not to create %s, got %v", dir, err)
		}
	}
	var found bool
	for _, check := range checks {
		if check.Status == DoctorOK && strings.Contains(check.Message, "does not exist yet (created on first install)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected missing-dir OK check: %+v", checks)
	}
}

func TestDoctorFindsBrokenMetadataEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	metaPath, err := GlobalMetadataPath()
	if err != nil {
		t.Fatal(err)
	}
	meta := EmptyMetadata()
	meta.Skills["broken"] = SkillMeta{Name: "broken", InstalledPath: filepath.Join(home, "missing"), Scope: ScopeGlobal}
	if err := WriteMetadata(metaPath, meta); err != nil {
		t.Fatal(err)
	}
	checks := RunDoctor(t.TempDir())
	var found bool
	for _, check := range checks {
		if check.Status == DoctorWarning && strings.Contains(check.Message, "broken") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected broken metadata warning: %+v", checks)
	}
}

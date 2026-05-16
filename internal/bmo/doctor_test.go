package bmo

import (
	"path/filepath"
	"strings"
	"testing"
)

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

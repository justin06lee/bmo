package bmo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Doctor exists to diagnose broken environments, so it must degrade to an
// ERROR check — not abort — when the home directory is unresolvable, and the
// cwd-based project diagnostics must still run.
func TestDoctorDegradesWithoutHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	checks, err := RunDoctorForHarness(t.TempDir(), "")
	if err != nil {
		t.Fatalf("doctor must not abort on a broken environment: %v", err)
	}
	var globalError, projectChecked bool
	for _, check := range checks {
		if check.Status == DoctorError && strings.Contains(check.Message, "Global destination") {
			globalError = true
		}
		if strings.Contains(check.Message, "Project skills dir") {
			projectChecked = true
		}
	}
	if !globalError || !projectChecked {
		t.Fatalf("expected a global ERROR plus project diagnostics, got %+v", checks)
	}
}

// runDoctorClaude runs the default-harness doctor the CLI uses.
func runDoctorClaude(t *testing.T, cwd string) []DoctorCheck {
	t.Helper()
	checks, err := RunDoctorForHarness(cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	return checks
}

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
	checks := runDoctorClaude(t, cwd)
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
	checks := runDoctorClaude(t, t.TempDir())
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

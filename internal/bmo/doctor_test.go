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
	globalSkills, err := ClaudeGlobalSkillsDir()
	if err != nil {
		t.Fatal(err)
	}
	projectSkills := ClaudeProjectSkillsDir(cwd)
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
	metaPath, err := ClaudeGlobalMetadataPath()
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

// The relaxed Claude validation is a real difference in behavior, not just a
// nicety: a skill it accepts can be refused the first time it is shared. Doctor
// has to name those skills, or the exemption stays invisible until then.
func TestDoctorReportsSkillsOnlyClaudeAccepts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	cwd := t.TempDir()

	skillsDir, err := ClaudeGlobalSkillsDir()
	if err != nil {
		t.Fatal(err)
	}
	// A folder name that no portable harness accepts, installed under Claude's
	// looser rules with no frontmatter name at all.
	installed := filepath.Join(skillsDir, "Legacy_Skill")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "SKILL.md"),
		[]byte("---\ndescription: A skill from before the portable rules.\n---\n\nDo the thing.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	metaPath, err := ClaudeGlobalMetadataPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := Metadata{Skills: map[string]SkillMeta{
		"Legacy_Skill": {Name: "Legacy_Skill", InstalledPath: installed},
	}}
	if err := WriteMetadata(metaPath, meta); err != nil {
		t.Fatal(err)
	}

	checks, err := RunDoctorForHarness(cwd, "claude")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, check := range checks {
		if check.Status == DoctorWarning && strings.Contains(check.Message, "Legacy_Skill") &&
			strings.Contains(check.Message, "no other harness will accept it") {
			found = true
		}
	}
	if !found {
		t.Fatalf("doctor did not report the unportable skill: %+v", checks)
	}

	// A portable harness enforces the rules on install, so it has nothing to
	// report and must not warn about skills it could never have accepted.
	for _, check := range mustDoctor(t, cwd, "grok") {
		if strings.Contains(check.Message, "no other harness will accept it") {
			t.Fatalf("a strict harness should not report portability warnings: %s", check.Message)
		}
	}
}

func mustDoctor(t *testing.T, cwd, harness string) []DoctorCheck {
	t.Helper()
	checks, err := RunDoctorForHarness(cwd, harness)
	if err != nil {
		t.Fatal(err)
	}
	return checks
}

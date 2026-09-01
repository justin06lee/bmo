package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justin06lee/bmo/internal/bmo"
)

// runBmo executes the real root command against a throwaway Claude config dir
// and returns its combined output.
func runBmo(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("BMO_APPLICATIONS_DIRS", t.TempDir())
	cmd := NewRootCommand()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func writeSourceSkill(t *testing.T, root, name string, agents ...string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: The " + name + " skill.\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, agent := range agents {
		agentDir := filepath.Join(dir, "agents")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		doc := "---\nname: " + agent + "\ndescription: The " + agent + " specialist.\n---\nBody\n"
		if err := os.WriteFile(filepath.Join(agentDir, agent+".md"), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAddAllInstallsEverySkillAndItsAgents(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha", "alpha-worker", "alpha-helper")
	writeSourceSkill(t, src, "beta")
	writeSourceSkill(t, src, "gamma")

	// Addressed by absolute path: a relative source would depend on the test
	// process's working directory.
	out, err := runBmo(t, home, "add", src, "--all", "--yes")
	if err != nil {
		t.Fatalf("add --all failed: %v\n%s", err, out)
	}
	skillsDir := filepath.Join(home, ".claude", "skills")
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if _, err := os.Stat(filepath.Join(skillsDir, name, "SKILL.md")); err != nil {
			t.Fatalf("expected %s installed: %v", name, err)
		}
	}
	agentsDir := filepath.Join(home, ".claude", "agents")
	for _, agent := range []string{"alpha-worker.md", "alpha-helper.md"} {
		if _, err := os.Stat(filepath.Join(agentsDir, agent)); err != nil {
			t.Fatalf("expected %s installed: %v", agent, err)
		}
	}
	if !strings.Contains(out, "Installed 3 skills") || !strings.Contains(out, "Installed 2 subagents") {
		t.Fatalf("expected a batch summary, got:\n%s", out)
	}
	// Every skill is tracked individually, so each stays independently
	// updatable and removable.
	meta, err := bmo.ReadMetadata(filepath.Join(home, ".bmo", "skills.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if _, ok := meta.Skills[name]; !ok {
			t.Fatalf("expected %s tracked in metadata", name)
		}
	}
}

func TestAddAllForCodexKeepsSameNamedAgentResourcesIsolated(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha", "worker")
	writeSourceSkill(t, src, "beta", "worker")

	out, err := runBmo(t, home, "add", src, "--all", "--yes", "--harness", "codex")
	if err != nil {
		t.Fatalf("portable add --all failed: %v\n%s", err, out)
	}
	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Stat(filepath.Join(home, ".agents", "skills", name, "agents", "worker.md")); err != nil {
			t.Fatalf("expected %s's isolated agent resource: %v", name, err)
		}
	}
}

func TestAddAcceptsPositionalHarness(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")

	out, err := runBmo(t, home, "add", src, "codex", "--yes")
	if err != nil {
		t.Fatalf("positional harness install failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "alpha", "SKILL.md")); err != nil {
		t.Fatalf("expected Codex install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "alpha")); !os.IsNotExist(err) {
		t.Fatalf("positional Codex target unexpectedly installed alpha for Claude: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "bmo")); !os.IsNotExist(err) {
		t.Fatalf("positional Codex target unexpectedly bootstrapped bmo for Claude: %v", err)
	}
}

func TestAddAcceptsExplicitGlobalFlagInAnyPosition(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")

	out, err := runBmo(t, home, "add", "--global", "codex", src, "--yes")
	if err != nil {
		t.Fatalf("explicit global install failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "alpha", "SKILL.md")); err != nil {
		t.Fatalf("expected global Codex install: %v", err)
	}
}

func TestAddRejectsConflictingScopeFlags(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")

	out, err := runBmo(t, home, "add", src, "--project", "--global", "--yes")
	if err == nil || !strings.Contains(err.Error(), "none of the others") {
		t.Fatalf("expected mutually exclusive scope error, got %v\n%s", err, out)
	}
}

func TestAddAllForPortableHarnessValidatesBeforeWriting(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")
	missing := filepath.Join(src, "skills", "missing-name")
	if err := os.MkdirAll(missing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missing, "SKILL.md"), []byte("---\ndescription: Missing portable name.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runBmo(t, home, "add", src, "--all", "--yes", "--harness", "codex")
	if err == nil || !strings.Contains(err.Error(), "explicit name") {
		t.Fatalf("expected portable validation error, got %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".agents", "skills", "alpha")); !os.IsNotExist(statErr) {
		t.Fatalf("batch validation wrote alpha before failing: %v", statErr)
	}
}

func TestAddEveryoneInstallsToDetectedHarnesses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	for _, dir := range []string{".codex", ".gemini", ".config/amp"} {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")
	writeSourceSkill(t, src, "beta")

	out, err := runBmo(t, home, "add", "everywhere", src, "everyone", "--all", "--yes")
	if err != nil {
		t.Fatalf("add everyone failed: %v\n%s", err, out)
	}
	for _, dir := range []string{".agents/skills", ".gemini/skills", ".config/agents/skills"} {
		for _, name := range []string{"alpha", "beta"} {
			if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(dir), name, "SKILL.md")); err != nil {
				t.Fatalf("expected everyone install of %s in %s: %v\n%s", name, dir, err, out)
			}
		}
	}
	if !strings.Contains(out, "3 detected harness(es)") || !strings.Contains(out, "Installed 6 skill copies") {
		t.Fatalf("unexpected everyone summary:\n%s", out)
	}
}

func TestAddHereEveryoneInstallsOnlyInsideProject(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	for _, dir := range []string{".codex", ".gemini"} {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")
	t.Chdir(project)

	out, err := runBmo(t, home, "add", "here", src, "everyone", "--yes")
	if err != nil {
		t.Fatalf("add here everyone failed: %v\n%s", err, out)
	}
	for _, dir := range []string{".agents/skills", ".gemini/skills"} {
		if _, err := os.Stat(filepath.Join(project, filepath.FromSlash(dir), "alpha", "SKILL.md")); err != nil {
			t.Fatalf("expected project install in %s: %v\n%s", dir, err, out)
		}
		if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(dir), "alpha")); !os.IsNotExist(err) {
			t.Fatalf("here install unexpectedly wrote global %s: %v", dir, err)
		}
	}
}

func TestDetectedProjectTargetsDeduplicateSharedAgentsDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("BMO_APPLICATIONS_DIRS", t.TempDir())
	for _, dir := range []string{".codex", ".config/amp"} {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	targets, err := detectedTargets(bmo.ScopeProject, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || strings.Join(targets[0].harnesses, ",") != "codex,amp" {
		t.Fatalf("shared project targets were not deduplicated: %+v", targets)
	}
}

func TestAddAllRefusesDuplicateNames(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")
	// A second folder resolving to the same name, as happens when a repo
	// mirrors a skill under extensions/.
	mirror := filepath.Join(src, "extensions", "pack", "skills", "alpha")
	if err := os.MkdirAll(mirror, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: alpha\ndescription: A mirrored alpha skill.\n---\n# alpha\n"
	if err := os.WriteFile(filepath.Join(mirror, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runBmo(t, home, "add", src, "--all", "--yes")
	if err == nil {
		t.Fatalf("expected duplicate names to fail, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "duplicate skill names") || !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("expected an error naming the duplicate, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".claude", "skills", "alpha")); !os.IsNotExist(statErr) {
		t.Fatal("nothing should be installed when the batch is rejected")
	}
}

func TestAddAllRefusesExistingInstallWithoutForce(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")
	writeSourceSkill(t, src, "beta")

	if out, err := runBmo(t, home, "add", filepath.Join(src, "skills", "alpha"), "--yes"); err != nil {
		t.Fatalf("first install failed: %v\n%s", err, out)
	}
	out, err := runBmo(t, home, "add", src, "--all", "--yes")
	if err == nil {
		t.Fatalf("expected the batch to refuse an existing install, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("expected an already-installed error, got %v", err)
	}
	// The precheck runs before any writing, so the untouched skill must not
	// have been installed on the way to discovering the conflict.
	if _, statErr := os.Stat(filepath.Join(home, ".claude", "skills", "beta")); !os.IsNotExist(statErr) {
		t.Fatal("a rejected batch must not install anything")
	}
	if out, err := runBmo(t, home, "add", src, "--all", "--yes", "--force"); err != nil {
		t.Fatalf("--force should allow the batch: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "beta", "SKILL.md")); err != nil {
		t.Fatalf("expected beta installed after --force: %v", err)
	}
}

func TestAddAllRefusesTwoSkillsClaimingOneAgent(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha", "shared")
	writeSourceSkill(t, src, "beta", "shared")

	out, err := runBmo(t, home, "add", src, "--all", "--yes")
	if err == nil {
		t.Fatalf("expected a subagent collision to fail, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "shipped by both") {
		t.Fatalf("expected a collision error, got %v", err)
	}
}

func TestAddAllRejectsNameOverride(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")

	_, err := runBmo(t, home, "add", src, "--all", "--name", "other", "--yes")
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with --all") {
		t.Fatalf("expected --name and --all to be mutually exclusive, got %v", err)
	}
}

func TestAddAllDryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha", "alpha-worker")
	writeSourceSkill(t, src, "beta")

	out, err := runBmo(t, home, "add", src, "--all", "--dry-run")
	if err != nil {
		t.Fatalf("dry run failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "would install 2 skills") {
		t.Fatalf("expected a dry-run summary, got:\n%s", out)
	}
	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Stat(filepath.Join(home, ".claude", "skills", name)); !os.IsNotExist(err) {
			t.Fatalf("dry run must not install %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "agents", "alpha-worker.md")); !os.IsNotExist(err) {
		t.Fatal("dry run must not install subagents")
	}
}

func TestAddAllReportsInvalidSkill(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")
	broken := filepath.Join(src, "skills", "broken")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "SKILL.md"), []byte("no frontmatter here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := runBmo(t, home, "add", src, "--all", "--yes")
	if err == nil || !strings.Contains(err.Error(), "failed validation") {
		t.Fatalf("expected a validation error for the whole batch, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".claude", "skills", "alpha")); !os.IsNotExist(statErr) {
		t.Fatal("a batch with an invalid skill must install nothing")
	}
}

func TestUpdateReportsFailuresWithoutStrandingOtherSkills(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")
	writeSourceSkill(t, src, "zeta")

	if out, err := runBmo(t, home, "add", filepath.Join(src, "skills", "alpha"), "--yes"); err != nil {
		t.Fatalf("install alpha: %v\n%s", err, out)
	}
	if out, err := runBmo(t, home, "add", filepath.Join(src, "skills", "zeta"), "--yes"); err != nil {
		t.Fatalf("install zeta: %v\n%s", err, out)
	}

	// Break the source that sorts first; the later skill must still be updated.
	if err := os.RemoveAll(filepath.Join(src, "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	zeta := filepath.Join(src, "skills", "zeta", "SKILL.md")
	if err := os.WriteFile(zeta, []byte("---\nname: zeta\ndescription: The zeta skill.\n---\n# zeta v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runBmo(t, home, "update")
	if err == nil {
		t.Fatalf("expected the broken skill to be reported as an error:\n%s", out)
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("expected the failure to name alpha, got %v", err)
	}
	if !strings.Contains(out, "Updated zeta") {
		t.Fatalf("expected zeta to update despite alpha failing:\n%s\n%v", out, err)
	}
	installed, readErr := os.ReadFile(filepath.Join(home, ".claude", "skills", "zeta", "SKILL.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(installed), "zeta v2") {
		t.Fatalf("expected zeta to carry the new content, got %q", installed)
	}
}

func TestDoctorRejectsConflictingScopeFlags(t *testing.T) {
	out, err := runBmo(t, t.TempDir(), "doctor", "--project", "--global")
	if err == nil || !strings.Contains(err.Error(), "none of the others") {
		t.Fatalf("expected mutually exclusive scope error, got %v\n%s", err, out)
	}
}

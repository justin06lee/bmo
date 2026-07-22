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

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justin06lee/bmo/internal/bmo"
)

// installIntoProject installs a skill into one project's harness destination
// the way a user would, then forgets the registry entry it created. Scout's
// job is to rediscover it, so the tests must start from an empty registry.
func installIntoProject(t *testing.T, project, harness, name string) {
	t.Helper()
	source := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: The " + name + " skill.\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := bmo.ResolveTarget(harness, bmo.ScopeProject, project, "")
	if err != nil {
		t.Fatal(err)
	}
	src, err := bmo.ParseSource(source)
	if err != nil {
		t.Fatal(err)
	}
	skill, err := bmo.ValidateSkill(source, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bmo.InstallSkill(bmo.InstallOptions{Target: target, CWD: project, Source: src, Skill: skill}); err != nil {
		t.Fatal(err)
	}
}

func forgetRegistry(t *testing.T) {
	t.Helper()
	path, err := bmo.ProjectRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}

func registeredProjects(t *testing.T) []string {
	t.Helper()
	projects, err := bmo.RegisteredProjects()
	if err != nil {
		t.Fatal(err)
	}
	return projects
}

func TestScoutRecordsProjectsForUpdateEverywhere(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	claudeProject := filepath.Join(root, "repo-a")
	codexProject := filepath.Join(root, "nested", "repo-b")
	buried := filepath.Join(root, "repo-a", "node_modules", "pkg")
	for _, dir := range []string{claudeProject, codexProject, buried} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	installIntoProject(t, claudeProject, "claude", "demo")
	installIntoProject(t, codexProject, "codex", "other")
	installIntoProject(t, buried, "claude", "demo")
	forgetRegistry(t)

	out, err := runBmo(t, home, "scout", root)
	if err != nil {
		t.Fatalf("bmo scout: %v\n%s", err, out)
	}
	if !strings.Contains(out, claudeProject) || !strings.Contains(out, codexProject) {
		t.Fatalf("expected both projects reported, got %q", out)
	}
	if strings.Contains(out, buried) {
		t.Fatalf("expected the node_modules install to stay out of the sweep, got %q", out)
	}
	projects := registeredProjects(t)
	if len(projects) != 2 {
		t.Fatalf("expected 2 recorded projects, got %v", projects)
	}
	// The point of recording them: an update sweep now reaches both without
	// being run inside either.
	out, err = runBmo(t, home, "update", "everywhere")
	if err != nil {
		t.Fatalf("bmo update everywhere: %v\n%s", err, out)
	}
	if !strings.Contains(out, "demo is up to date") || !strings.Contains(out, "other is up to date") {
		t.Fatalf("expected the scouted projects to be visited, got %q", out)
	}
}

func TestScoutDryRunLeavesTheRegistryAlone(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	project := filepath.Join(root, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	installIntoProject(t, project, "claude", "demo")
	forgetRegistry(t)

	out, err := runBmo(t, home, "scout", root, "--dry-run")
	if err != nil {
		t.Fatalf("bmo scout --dry-run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "would record") || !strings.Contains(out, "registry was not changed") {
		t.Fatalf("expected a preview, got %q", out)
	}
	if projects := registeredProjects(t); len(projects) != 0 {
		t.Fatalf("expected an untouched registry, got %v", projects)
	}
}

func TestScoutPruneForgetsVanishedProjects(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bmo.RecordProject(gone); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	// Without --prune the stale entry survives: forgetting a project is
	// opt-in, because an unmounted drive looks exactly like a deleted repo.
	out, err := runBmo(t, home, "scout", root)
	if err != nil {
		t.Fatalf("bmo scout: %v\n%s", err, out)
	}
	if projects := registeredProjects(t); len(projects) != 1 {
		t.Fatalf("expected the stale entry to survive a plain scout, got %v", projects)
	}
	out, err = runBmo(t, home, "scout", root, "--prune")
	if err != nil {
		t.Fatalf("bmo scout --prune: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Forgot "+gone) {
		t.Fatalf("expected the pruned project to be named, got %q", out)
	}
	if projects := registeredProjects(t); len(projects) != 0 {
		t.Fatalf("expected the registry to be empty, got %v", projects)
	}
}

func TestScoutJSONReportsFindingsAndRecords(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	project := filepath.Join(root, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	installIntoProject(t, project, "codex", "demo")
	forgetRegistry(t)

	out, err := runBmo(t, home, "scout", root, "--json")
	if err != nil {
		t.Fatalf("bmo scout --json: %v\n%s", err, out)
	}
	// runBmo merges stderr, where the one-time bootstrap notice lands.
	body := out[strings.Index(out, "{"):]
	var report scoutReport
	if err := json.Unmarshal([]byte(body), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", body, err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Dir != project {
		t.Fatalf("findings = %+v, want one for %s", report.Findings, project)
	}
	if len(report.Recorded) != 1 || report.Recorded[0] != project {
		t.Fatalf("recorded = %v, want %s", report.Recorded, project)
	}
	if report.Total != 1 {
		t.Fatalf("total registered = %d, want 1", report.Total)
	}
	if got := report.Findings[0].Skills; len(got) != 1 || got[0] != "demo" {
		t.Fatalf("skills = %v, want [demo]", got)
	}
}

func TestScoutReportsAnEmptyTree(t *testing.T) {
	home := isolateHome(t)
	out, err := runBmo(t, home, "scout", t.TempDir())
	if err != nil {
		t.Fatalf("bmo scout: %v\n%s", err, out)
	}
	if !strings.Contains(out, "No bmo installs found") {
		t.Fatalf("expected an empty-tree report, got %q", out)
	}
}

func TestScoutRejectsANegativeDepth(t *testing.T) {
	home := isolateHome(t)
	out, err := runBmo(t, home, "scout", "--depth", "-1")
	if err == nil || !strings.Contains(err.Error(), "cannot be negative") {
		t.Fatalf("bmo scout --depth -1 error = %v, want a negative-depth error\n%s", err, out)
	}
}

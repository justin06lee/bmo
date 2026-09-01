package bmo

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// installTrackedSkill writes a project-scope install of name for one harness
// directly through the real installer, so a scout test exercises the same
// on-disk layout a user would produce.
func installTrackedSkill(t *testing.T, project, harness, name string) {
	t.Helper()
	source := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: The " + name + " skill.\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := ResolveTarget(harness, ScopeProject, project, "")
	if err != nil {
		t.Fatal(err)
	}
	src, err := ParseSource(source)
	if err != nil {
		t.Fatal(err)
	}
	skill, err := ValidateSkill(source, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkill(InstallOptions{Target: target, CWD: project, Source: src, Skill: skill}); err != nil {
		t.Fatal(err)
	}
}

func findingFor(t *testing.T, result ScoutResult, dir string) ScoutFinding {
	t.Helper()
	for _, finding := range result.Findings {
		if finding.Dir == dir {
			return finding
		}
	}
	t.Fatalf("expected a finding for %s, got %+v", dir, result.Findings)
	return ScoutFinding{}
}

func TestScoutFindsProjectsForEveryHarness(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()

	// One project per built-in preset, so a new harness cannot be added
	// without scout learning to see it.
	dirs := map[string]string{}
	for _, info := range Harnesses() {
		project := filepath.Join(root, string(info.Name)+"-project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		dirs[string(info.Name)] = project
		installTrackedSkill(t, project, string(info.Name), "demo")
	}

	result, err := Scout(ScoutOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	for name, project := range dirs {
		finding := findingFor(t, result, project)
		if !slices.Contains(finding.Harnesses, name) {
			t.Fatalf("expected %s among %v for %s", name, finding.Harnesses, project)
		}
		if !slices.Contains(finding.Skills, "demo") {
			t.Fatalf("expected the demo skill in %v for %s", finding.Skills, project)
		}
	}
}

func TestScoutCreditsSharedConfigDirectoryToEveryHarness(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	project := filepath.Join(root, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	// Codex and Amp share a project's .agents directory, so one lock file
	// belongs to both.
	installTrackedSkill(t, project, "codex", "demo")

	result, err := Scout(ScoutOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	finding := findingFor(t, result, project)
	if !slices.Contains(finding.Harnesses, "codex") || !slices.Contains(finding.Harnesses, "amp") {
		t.Fatalf("expected codex and amp to share the finding, got %v", finding.Harnesses)
	}
	if len(finding.LockFiles) != 1 {
		t.Fatalf("expected one shared lock file, got %v", finding.LockFiles)
	}
}

func TestScoutNamesEachSkillOncePerProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	project := filepath.Join(root, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	installTrackedSkill(t, project, "claude", "demo")
	installTrackedSkill(t, project, "codex", "demo")

	result, err := Scout(ScoutOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	finding := findingFor(t, result, project)
	if len(finding.Skills) != 1 || finding.Skills[0] != "demo" {
		t.Fatalf("expected one deduplicated skill name, got %v", finding.Skills)
	}
	if len(finding.LockFiles) != 2 {
		t.Fatalf("expected both harness lock files, got %v", finding.LockFiles)
	}
}

func TestScoutSkipsDependencyBuildAndHiddenTrees(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	buried := map[string]string{}
	for _, parent := range []string{"node_modules", ".venv", "target", ".git", ".secret"} {
		project := filepath.Join(root, parent, "pkg")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		buried[parent] = project
		installTrackedSkill(t, project, "claude", "demo")
	}
	visible := filepath.Join(root, "repo")
	if err := os.MkdirAll(visible, 0o755); err != nil {
		t.Fatal(err)
	}
	installTrackedSkill(t, visible, "claude", "demo")

	result, err := Scout(ScoutOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Dir != visible {
		t.Fatalf("expected only %s, got %+v", visible, result.Findings)
	}

	// --hidden reaches unrelated dot-directories, and only those: dependency
	// and build trees stay pruned because walking them is never worth it.
	result, err = Scout(ScoutOptions{Root: root, IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	dirs := result.ProjectDirs()
	if !slices.Contains(dirs, buried[".secret"]) {
		t.Fatalf("expected --hidden to reach %s, got %v", buried[".secret"], dirs)
	}
	for _, pruned := range []string{"node_modules", ".venv", "target", ".git"} {
		if slices.Contains(dirs, buried[pruned]) {
			t.Fatalf("expected %s to stay pruned even with --hidden, got %v", pruned, dirs)
		}
	}
}

func TestScoutHonorsDepthLimit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	shallow := filepath.Join(root, "near")
	deep := filepath.Join(root, "a", "b", "far")
	for _, dir := range []string{shallow, deep} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		installTrackedSkill(t, dir, "claude", "demo")
	}

	// A project one directory below the root is inside a --depth 1 budget
	// even though its own .claude folder sits a level deeper.
	result, err := Scout(ScoutOptions{Root: root, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if dirs := result.ProjectDirs(); len(dirs) != 1 || dirs[0] != shallow {
		t.Fatalf("expected only %s at depth 1, got %v", shallow, dirs)
	}
	result, err = Scout(ScoutOptions{Root: root, MaxDepth: 3})
	if err != nil {
		t.Fatal(err)
	}
	if dirs := result.ProjectDirs(); len(dirs) != 2 {
		t.Fatalf("expected both projects at depth 3, got %v", dirs)
	}
}

func TestScoutFindsARootThatIsItselfAProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	installTrackedSkill(t, root, "claude", "demo")

	result, err := Scout(ScoutOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if dirs := result.ProjectDirs(); len(dirs) != 1 || dirs[0] != root {
		t.Fatalf("expected the root itself, got %v", dirs)
	}
}

func TestScoutIgnoresLockFilesTrackingNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	project := filepath.Join(root, "repo", ".claude")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadata(filepath.Join(project, ProjectLockFileName), EmptyMetadata()); err != nil {
		t.Fatal(err)
	}

	result, err := Scout(ScoutOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected an empty lock file to be ignored, got %+v", result.Findings)
	}
}

func TestScoutReportsUnparseableLockFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	config := filepath.Join(root, "repo", ".claude")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(config, ProjectLockFileName)
	if err := os.WriteFile(lock, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Scout(ScoutOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings for broken metadata, got %+v", result.Findings)
	}
	if len(result.Invalid) != 1 || result.Invalid[0] != lock {
		t.Fatalf("expected %s reported as invalid, got %v", lock, result.Invalid)
	}
}

func TestScoutMarksAlreadyRegisteredProjects(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	project := filepath.Join(root, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	// Installing records the project, so a later sweep must report it as
	// already known rather than as a new discovery.
	installTrackedSkill(t, project, "claude", "demo")

	result, err := Scout(ScoutOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if finding := findingFor(t, result, project); !finding.Registered {
		t.Fatalf("expected %s to be marked as registered", project)
	}
}

func TestScoutDoesNotFollowSymlinkedDirectories(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	real := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	installTrackedSkill(t, real, "claude", "demo")
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	result, err := Scout(ScoutOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected a symlinked tree to stay outside the sweep, got %+v", result.Findings)
	}
}

func TestScoutRejectsANonDirectoryRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Scout(ScoutOptions{Root: file}); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("Scout(file) error = %v, want a not-a-directory error", err)
	}
}

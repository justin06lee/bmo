package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justin06lee/bmo/internal/bmo"
)

// installedSkillExists reports whether a project's harness destination still
// holds the skill directory.
func installedSkillExists(t *testing.T, dir, harness, name string, scope bmo.Scope) bool {
	t.Helper()
	target, err := bmo.ResolveTarget(harness, scope, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target.SkillsDir, name)); err != nil {
		return false
	}
	return metadataHasSkill(target.MetadataPath, name)
}

// onlyRegisterProjects rewrites the project registry so a sweep's reach can be
// pinned exactly, including projects bmo installed into but has since lost
// track of.
func onlyRegisterProjects(t *testing.T, dirs ...string) {
	t.Helper()
	path, err := bmo.ProjectRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"version": 1, "projects": dirs})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The widest removal bmo performs: every harness, in every project the
// registry knows, from a directory that is none of them.
func TestRemoveEverywhereSweepsEveryHarnessAndProject(t *testing.T) {
	home := isolateHome(t)
	claudeProject := t.TempDir()
	codexProject := t.TempDir()
	installIntoProject(t, claudeProject, "claude", "demo")
	installIntoProject(t, codexProject, "codex", "demo")
	// A skill of another name in the same project must be left alone.
	installIntoProject(t, codexProject, "codex", "keeper")
	t.Chdir(t.TempDir())

	src := t.TempDir()
	writeSourceSkill(t, src, "demo")
	if out, err := runBmo(t, home, "add", filepath.Join(src, "skills", "demo"), "--yes"); err != nil {
		t.Fatalf("global install failed: %v\n%s", err, out)
	}

	out, err := runBmo(t, home, "remove", "demo", "everywhere", "--yes")
	if err != nil {
		t.Fatalf("bmo remove demo everywhere: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Global (claude)") ||
		!strings.Contains(out, claudeProject+" (claude)") ||
		!strings.Contains(out, codexProject+" (codex)") {
		t.Fatalf("expected every destination to be reported:\n%s", out)
	}
	if !strings.Contains(out, "Removed 3 copies of demo.") {
		t.Fatalf("expected a three-copy summary:\n%s", out)
	}
	if installedSkillExists(t, claudeProject, "claude", "demo", bmo.ScopeProject) {
		t.Fatal("claude project copy survived the sweep")
	}
	if installedSkillExists(t, codexProject, "codex", "demo", bmo.ScopeProject) {
		t.Fatal("codex project copy survived the sweep")
	}
	if installedSkillExists(t, t.TempDir(), "claude", "demo", bmo.ScopeGlobal) {
		t.Fatal("global copy survived the sweep")
	}
	if !installedSkillExists(t, codexProject, "codex", "keeper", bmo.ScopeProject) {
		t.Fatal("the sweep removed a skill it was not asked about")
	}
}

// The sweep must reach a project it has never been run inside, and must not
// fail just because some locations do not have the skill.
func TestRemoveEverywhereReachesRegisteredProjectsOnly(t *testing.T) {
	home := isolateHome(t)
	project := t.TempDir()
	installIntoProject(t, project, "claude", "alpha")
	unregistered := t.TempDir()
	installIntoProject(t, unregistered, "claude", "alpha")
	onlyRegisterProjects(t, project)
	t.Chdir(t.TempDir())

	out, err := runBmo(t, home, "remove", "alpha", "everywhere", "--yes")
	if err != nil {
		t.Fatalf("bmo remove alpha everywhere: %v\n%s", err, out)
	}
	if installedSkillExists(t, project, "claude", "alpha", bmo.ScopeProject) {
		t.Fatalf("registered project copy survived:\n%s", out)
	}
	if !installedSkillExists(t, unregistered, "claude", "alpha", bmo.ScopeProject) {
		t.Fatalf("an unregistered project was swept; only the registry is in reach:\n%s", out)
	}
	if !strings.Contains(out, "Removed 1 copy of alpha.") {
		t.Fatalf("expected a single-copy summary:\n%s", out)
	}
}

// Registered directories that no longer exist are reported, not fatal.
func TestRemoveEverywhereSkipsMissingProjects(t *testing.T) {
	home := isolateHome(t)
	project := t.TempDir()
	installIntoProject(t, project, "claude", "alpha")
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
	t.Chdir(t.TempDir())

	out, err := runBmo(t, home, "remove", "alpha", "everywhere", "--yes")
	if err != nil {
		t.Fatalf("bmo remove alpha everywhere: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Skipping "+gone) {
		t.Fatalf("expected the missing project to be noted:\n%s", out)
	}
}

// "everyone" is the narrower fan-out: every harness, but only where the
// current directory reaches.
func TestRemoveEveryoneStaysWithThisDirectory(t *testing.T) {
	home := isolateHome(t)
	project := t.TempDir()
	installIntoProject(t, project, "claude", "demo")
	installIntoProject(t, project, "codex", "demo")
	elsewhere := t.TempDir()
	installIntoProject(t, elsewhere, "claude", "demo")
	t.Chdir(project)

	out, err := runBmo(t, home, "remove", "demo", "here", "everyone", "--yes")
	if err != nil {
		t.Fatalf("bmo remove demo here everyone: %v\n%s", err, out)
	}
	if installedSkillExists(t, project, "claude", "demo", bmo.ScopeProject) ||
		installedSkillExists(t, project, "codex", "demo", bmo.ScopeProject) {
		t.Fatalf("expected both harnesses in this project to be cleared:\n%s", out)
	}
	if !installedSkillExists(t, elsewhere, "claude", "demo", bmo.ScopeProject) {
		t.Fatalf(`expected "here" to stay in this project:\n%s`, out)
	}
}

// A sweep that finds nothing says so in the language of its reach, rather
// than blaming one scope the user never named.
func TestRemoveEverywhereReportsNothingTracked(t *testing.T) {
	home := isolateHome(t)
	t.Chdir(t.TempDir())

	out, err := runBmo(t, home, "remove", "ghost", "everywhere", "--yes")
	if err == nil || !strings.Contains(err.Error(), "not tracked by bmo anywhere: ghost") {
		t.Fatalf("bmo remove ghost everywhere error = %v, want an anywhere-scoped refusal\n%s", err, out)
	}
}

// The single-destination refusal points at the sweep that would have found it.
func TestRemoveOneSuggestsEverywhere(t *testing.T) {
	home := isolateHome(t)
	project := t.TempDir()
	installIntoProject(t, project, "claude", "alpha")
	t.Chdir(t.TempDir())

	out, err := runBmo(t, home, "remove", "alpha", "--yes")
	if err == nil || !strings.Contains(err.Error(), "bmo remove alpha everywhere") {
		t.Fatalf("bmo remove alpha error = %v, want a pointer to the sweep\n%s", err, out)
	}
}

// Declining the confirmation must leave every copy in place.
func TestRemoveEverywhereCancelKeepsEveryCopy(t *testing.T) {
	home := isolateHome(t)
	project := t.TempDir()
	installIntoProject(t, project, "claude", "demo")
	t.Chdir(t.TempDir())

	out, err := runBmo(t, home, "remove", "demo", "everywhere")
	if err == nil || !strings.Contains(err.Error(), "remove cancelled") {
		t.Fatalf("bmo remove demo everywhere error = %v, want the cancellation\n%s", err, out)
	}
	if !strings.Contains(out, "Remove demo from 1 place:") {
		t.Fatalf("expected the preview to name the destination:\n%s", out)
	}
	if !installedSkillExists(t, project, "claude", "demo", bmo.ScopeProject) {
		t.Fatal("a cancelled sweep removed a skill")
	}
}

// One destination bmo cannot clean up must not strand the rest: the others are
// removed and the refusal is reported.
func TestRemoveEverywhereContinuesPastABlockedDestination(t *testing.T) {
	home := isolateHome(t)
	project := t.TempDir()
	installIntoProject(t, project, "claude", "demo")
	blocked := t.TempDir()
	installIntoProject(t, blocked, "codex", "demo")
	// Metadata claiming subagents a portable harness cannot host is the state
	// remove refuses; it must not stop the sweep.
	target, err := bmo.ResolveTarget("codex", bmo.ScopeProject, blocked, "")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := bmo.ReadMetadata(target.MetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := meta.Skills["demo"]
	entry.Agents = []string{"helper.md"}
	meta.Skills["demo"] = entry
	if err := bmo.WriteMetadata(target.MetadataPath, meta); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	out, err := runBmo(t, home, "remove", "demo", "everywhere", "--yes")
	if err == nil || !strings.Contains(err.Error(), "could not be removed from 1 destination") {
		t.Fatalf("bmo remove demo everywhere error = %v, want the blocked destination reported\n%s", err, out)
	}
	if !strings.Contains(out, "Cannot remove from "+blocked+" (codex):") {
		t.Fatalf("expected the refusal before the preview:\n%s", out)
	}
	if installedSkillExists(t, project, "claude", "demo", bmo.ScopeProject) {
		t.Fatalf("a blocked destination stranded the removable copy:\n%s", out)
	}
	if !installedSkillExists(t, blocked, "codex", "demo", bmo.ScopeProject) {
		t.Fatal("the blocked copy was removed anyway")
	}
}

// A sweep resolves its own destinations, so an explicit directory cannot join
// one; the refusal has to say which form to use instead.
func TestRemoveEverywhereRejectsSkillsDir(t *testing.T) {
	home := isolateHome(t)
	t.Chdir(t.TempDir())

	out, err := runBmo(t, home, "remove", "demo", "everywhere", "--skills-dir", "sk", "--yes")
	if err == nil || !strings.Contains(err.Error(), "cannot discover arbitrary --skills-dir locations") {
		t.Fatalf("bmo remove demo everywhere --skills-dir error = %v, want a refusal\n%s", err, out)
	}
}

func TestRemoveEveryoneRejectsASingleDestination(t *testing.T) {
	for _, flag := range [][]string{{"--skills-dir", "sk"}, {"--harness", "codex"}} {
		t.Run(flag[0], func(t *testing.T) {
			home := isolateHome(t)
			t.Chdir(t.TempDir())
			args := append([]string{"remove", "demo", "everyone"}, flag...)
			out, err := runBmo(t, home, args...)
			if err == nil || !strings.Contains(err.Error(), "already covers every harness") {
				t.Fatalf("bmo remove demo everyone %s error = %v, want a refusal\n%s", flag[0], err, out)
			}
		})
	}
}

// Naming a harness narrows the sweep to it rather than widening to all.
func TestRemoveEverywhereHonorsANamedHarness(t *testing.T) {
	home := isolateHome(t)
	project := t.TempDir()
	installIntoProject(t, project, "claude", "demo")
	installIntoProject(t, project, "codex", "demo")
	t.Chdir(t.TempDir())

	out, err := runBmo(t, home, "remove", "demo", "everywhere", "codex", "--yes")
	if err != nil {
		t.Fatalf("bmo remove demo everywhere codex: %v\n%s", err, out)
	}
	if installedSkillExists(t, project, "codex", "demo", bmo.ScopeProject) {
		t.Fatalf("the codex copy survived:\n%s", out)
	}
	if !installedSkillExists(t, project, "claude", "demo", bmo.ScopeProject) {
		t.Fatalf("naming codex must leave the claude copy alone:\n%s", out)
	}
}

// The case a real machine hits: a repo that was moved after installation, so
// its metadata records a path that no longer exists. The sweep must still
// clear the copy that is really there.
func TestRemoveEverywhereClearsARelocatedProject(t *testing.T) {
	home := isolateHome(t)
	parent := t.TempDir()
	before := filepath.Join(parent, "before")
	if err := os.MkdirAll(before, 0o755); err != nil {
		t.Fatal(err)
	}
	installIntoProject(t, before, "claude", "demo")
	after := filepath.Join(parent, "shipped", "after")
	if err := os.MkdirAll(filepath.Dir(after), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(before, after); err != nil {
		t.Fatal(err)
	}
	onlyRegisterProjects(t, after)
	t.Chdir(t.TempDir())

	out, err := runBmo(t, home, "remove", "demo", "everywhere", "--yes")
	if err != nil {
		t.Fatalf("bmo remove demo everywhere: %v\n%s", err, out)
	}
	if !strings.Contains(out, filepath.Join(after, ".claude", "skills", "demo")) {
		t.Fatalf("expected the preview to name the copy that is really there:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(after, ".claude", "skills", "demo")); !os.IsNotExist(err) {
		t.Fatalf("the relocated copy survived the sweep: %v\n%s", err, out)
	}
	if installedSkillExists(t, after, "claude", "demo", bmo.ScopeProject) {
		t.Fatalf("metadata still tracks the removed skill:\n%s", out)
	}
}

// Files deleted by hand leave an entry that is the only thing still calling
// the skill installed. The sweep clears it, and says what it actually did.
func TestRemoveEverywhereUntracksAnEntryWithNoFilesLeft(t *testing.T) {
	home := isolateHome(t)
	project := t.TempDir()
	installIntoProject(t, project, "claude", "demo")
	if err := os.RemoveAll(filepath.Join(project, ".claude", "skills", "demo")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	out, err := runBmo(t, home, "remove", "demo", "everywhere", "--yes")
	if err != nil {
		t.Fatalf("bmo remove demo everywhere: %v\n%s", err, out)
	}
	if !strings.Contains(out, "untracks the entry only") ||
		!strings.Contains(out, "Untracked 1 stale entry of demo.") {
		t.Fatalf("expected the sweep to report an untrack rather than a deletion:\n%s", out)
	}
	if strings.Contains(out, "Removed 1 copy") {
		t.Fatalf("nothing was on disk, so nothing was removed:\n%s", out)
	}
	if installedSkillExists(t, project, "claude", "demo", bmo.ScopeProject) {
		t.Fatalf("the stale entry survived:\n%s", out)
	}
}

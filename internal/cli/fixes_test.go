package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/justin06lee/bmo/internal/bmo"
)

// bumpSourceSkill rewrites a skill's SKILL.md so its content hash changes.
func bumpSourceSkill(t *testing.T, root, name string) {
	t.Helper()
	body := "---\nname: " + name + "\ndescription: The " + name + " skill.\n---\n# " + name + " v2\n"
	if err := os.WriteFile(filepath.Join(root, "skills", name, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// One failing scope must not strand the other: a broken global source still
// lets the project-scope sweep run (and vice versa).
func TestUpdateContinuesAcrossScopesAfterGlobalFailure(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Chdir(project)
	globalSrc := t.TempDir()
	writeSourceSkill(t, globalSrc, "alpha")
	projectSrc := t.TempDir()
	writeSourceSkill(t, projectSrc, "beta")

	if out, err := runBmo(t, home, "add", filepath.Join(globalSrc, "skills", "alpha"), "--yes"); err != nil {
		t.Fatalf("install alpha: %v\n%s", err, out)
	}
	if out, err := runBmo(t, home, "add", filepath.Join(projectSrc, "skills", "beta"), "here", "--yes"); err != nil {
		t.Fatalf("install beta: %v\n%s", err, out)
	}

	if err := os.RemoveAll(filepath.Join(globalSrc, "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	bumpSourceSkill(t, projectSrc, "beta")

	out, err := runBmo(t, home, "update")
	if err == nil || !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("expected the broken global skill to be reported, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "Updated beta") {
		t.Fatalf("expected the project scope to still update beta:\n%s", out)
	}
	installed, readErr := os.ReadFile(filepath.Join(project, ".claude", "skills", "beta", "SKILL.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(installed), "beta v2") {
		t.Fatalf("project install did not carry the new content: %q", installed)
	}
}

// Names that were legal before the portable presets tightened the grammar must
// keep updating on the default Claude target; portable targets reject them.
func TestUpdateAcceptsLegacyHyphenNames(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	src := t.TempDir()
	writeSourceSkill(t, src, "my--skill")

	if out, err := runBmo(t, home, "add", src, "--yes"); err != nil {
		t.Fatalf("legacy-name install failed: %v\n%s", err, out)
	}
	bumpSourceSkill(t, src, "my--skill")
	out, err := runBmo(t, home, "update")
	if err != nil {
		t.Fatalf("legacy-name update failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Updated my--skill") {
		t.Fatalf("expected my--skill to update:\n%s", out)
	}

	if _, err := runBmo(t, home, "add", src, "codex", "--yes"); err == nil || !strings.Contains(err.Error(), "single hyphens") {
		t.Fatalf("expected portable target to reject the legacy name, got %v", err)
	}
}

// `bmo update everywhere` must reach project installs recorded for non-Claude
// harnesses even when no harness is named on the command line.
func TestUpdateEverywhereReachesOtherHarnessProjects(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")

	t.Chdir(project)
	if out, err := runBmo(t, home, "add", src, "codex", "here", "--yes"); err != nil {
		t.Fatalf("codex project install failed: %v\n%s", err, out)
	}
	t.Chdir(t.TempDir())
	bumpSourceSkill(t, src, "alpha")

	out, err := runBmo(t, home, "update", "everywhere")
	if err != nil {
		t.Fatalf("update everywhere failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, project+" (codex):") || !strings.Contains(out, "Updated alpha") {
		t.Fatalf("expected the codex project install to be visited and updated:\n%s", out)
	}
	installed, readErr := os.ReadFile(filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(installed), "alpha v2") {
		t.Fatalf("codex project install did not carry the new content: %q", installed)
	}
}

// A fan-out that fails partway must undo the destinations it already wrote, so
// a plain retry does not trip over the leftovers as "already exists".
func TestAddEveryoneRollsBackOnMidLoopFailure(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	t.Setenv("PATH", t.TempDir())
	for _, dir := range []string{".codex", ".gemini"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A file where gemini's skills directory belongs makes its install fail
	// after codex's has already succeeded.
	if err := os.WriteFile(filepath.Join(home, ".gemini", "skills"), []byte("in the way\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")

	out, err := runBmo(t, home, "add", src, "everyone", "--yes")
	if err == nil || !strings.Contains(err.Error(), "rolled them back") {
		t.Fatalf("expected a rolled-back failure, got %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".agents", "skills", "alpha")); !os.IsNotExist(statErr) {
		t.Fatalf("codex copy should have been rolled back: %v", statErr)
	}
	meta, metaErr := bmo.ReadMetadata(filepath.Join(home, ".bmo", "codex-skills.json"))
	if metaErr != nil {
		t.Fatal(metaErr)
	}
	if _, tracked := meta.Skills["alpha"]; tracked {
		t.Fatal("rolled-back install must not stay tracked in metadata")
	}

	// With the obstacle removed, a plain retry (no --force) succeeds.
	if err := os.Remove(filepath.Join(home, ".gemini", "skills")); err != nil {
		t.Fatal(err)
	}
	if out, err := runBmo(t, home, "add", src, "everyone", "--yes"); err != nil {
		t.Fatalf("retry after rollback failed: %v\n%s", err, out)
	}
	for _, dir := range []string{".agents/skills", ".gemini/skills"} {
		if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(dir), "alpha", "SKILL.md")); err != nil {
			t.Fatalf("expected %s installed on retry: %v", dir, err)
		}
	}
}

// A local source folder named after a harness stays installable by bare name.
func TestAddInstallsLocalFolderNamedLikeHarness(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Chdir(cwd)
	dir := filepath.Join(cwd, "codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: codex\ndescription: A skill that happens to be named codex.\n---\n# codex\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runBmo(t, home, "add", "codex", "--yes")
	if err != nil {
		t.Fatalf("bmo add codex (a local folder) failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "codex", "SKILL.md")); err != nil {
		t.Fatalf("expected the folder named codex installed as a skill: %v", err)
	}
}

// A skill literally named "everyone" must remain removable by name.
func TestRemoveSkillNamedEveryone(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	src := t.TempDir()
	writeSourceSkill(t, src, "everyone")

	if out, err := runBmo(t, home, "add", src, "--yes"); err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	out, err := runBmo(t, home, "remove", "everyone", "--yes")
	if err != nil {
		t.Fatalf("bmo remove everyone failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed everyone") {
		t.Fatalf("expected the skill named everyone to be removed:\n%s", out)
	}
}

// doctor's here/everywhere keyword and scope flags narrow the report.
func TestDoctorHonorsScopeSelection(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())

	out, err := runBmo(t, home, "doctor", "here")
	if err != nil {
		t.Fatalf("doctor here failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Project skills dir") || strings.Contains(out, "Global skills dir") {
		t.Fatalf("doctor here must report only the project scope:\n%s", out)
	}

	out, err = runBmo(t, home, "doctor", "--global")
	if err != nil {
		t.Fatalf("doctor --global failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "Project skills dir") || !strings.Contains(out, "Global skills dir") {
		t.Fatalf("doctor --global must report only the global scope:\n%s", out)
	}
}

// `bmo add --dry-run` over an existing install reports instead of failing.
func TestAddDryRunOverExistingInstall(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")

	if out, err := runBmo(t, home, "add", src, "--yes"); err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	out, err := runBmo(t, home, "add", src, "--dry-run")
	if err != nil {
		t.Fatalf("dry run over an existing install failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Dry run: would install alpha") {
		t.Fatalf("expected a dry-run report:\n%s", out)
	}
}

// The remove guard fires before the preview and prompt, so the user is never
// asked to confirm a removal that cannot happen (and never sees a blank
// subagent destination).
func TestRemoveGuardRunsBeforePrompt(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	installed := filepath.Join(home, ".agents", "skills", "demo")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := bmo.EmptyMetadata()
	meta.Skills["demo"] = bmo.SkillMeta{
		Name:          "demo",
		Scope:         bmo.ScopeGlobal,
		Harness:       bmo.HarnessCodex,
		InstalledPath: installed,
		Agents:        []string{"worker.md"},
	}
	if err := bmo.WriteMetadata(filepath.Join(home, ".bmo", "codex-skills.json"), meta); err != nil {
		t.Fatal(err)
	}

	out, err := runBmo(t, home, "remove", "demo", "codex")
	if err == nil || !strings.Contains(err.Error(), "no compatible agent destination") {
		t.Fatalf("expected the agents guard, got %v\n%s", err, out)
	}
	if strings.Contains(out, "Remove?") {
		t.Fatalf("the guard must fire before the confirmation prompt:\n%s", out)
	}
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("a refused removal must leave the install alone: %v", err)
	}
}

// Positional harness tokens match case-insensitively, like --harness does.
func TestPositionalHarnessMatchesCaseInsensitively(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	out, err := runBmo(t, home, "list", "Codex", "--json")
	if err != nil {
		t.Fatalf("bmo list Codex failed: %v\n%s", err, out)
	}
}

// Ten skills sharing one dead source must cost one resolution attempt, not ten.
func TestUpdateResolvesFailingSourceOnce(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "gone", http.StatusInternalServerError)
	}))
	defer server.Close()

	meta := bmo.EmptyMetadata()
	for _, name := range []string{"aa", "bb"} {
		meta.Skills[name] = bmo.SkillMeta{
			Name:          name,
			Scope:         bmo.ScopeGlobal,
			Source:        server.URL + "/skill.zip",
			InstalledPath: filepath.Join(home, ".claude", "skills", name),
			SourceType:    string(bmo.SourceZipURL),
		}
	}
	if err := bmo.WriteMetadata(filepath.Join(home, ".bmo", "skills.json"), meta); err != nil {
		t.Fatal(err)
	}

	out, err := runBmo(t, home, "update")
	if err == nil || !strings.Contains(err.Error(), "aa") || !strings.Contains(err.Error(), "bb") {
		t.Fatalf("expected both skills reported as failed, got %v\n%s", err, out)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("expected the dead source to be tried once, got %d attempts", got)
	}
}

// The --skills-dir escape hatch works end to end: install, list, doctor, and
// remove all resolve the same custom destination and its bmo-lock.json.
func TestSkillsDirEndToEnd(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Chdir(project)
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")

	if out, err := runBmo(t, home, "add", src, "--skills-dir", "tools/skills", "--yes"); err != nil {
		t.Fatalf("custom-dir install failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(project, "tools", "skills", "alpha", "SKILL.md")); err != nil {
		t.Fatalf("expected the skill in the custom directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "tools", "bmo-lock.json")); err != nil {
		t.Fatalf("expected bmo-lock.json beside the custom directory: %v", err)
	}

	out, err := runBmo(t, home, "list", "--skills-dir", "tools/skills", "--json")
	if err != nil || !strings.Contains(out, "alpha") {
		t.Fatalf("custom-dir list failed: %v\n%s", err, out)
	}

	out, err = runBmo(t, home, "doctor", "--skills-dir", "tools/skills")
	if err != nil || !strings.Contains(out, filepath.Join(project, "tools", "skills")) {
		t.Fatalf("custom-dir doctor failed: %v\n%s", err, out)
	}

	if out, err := runBmo(t, home, "remove", "alpha", "--skills-dir", "tools/skills", "--yes"); err != nil {
		t.Fatalf("custom-dir remove failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(project, "tools", "skills", "alpha")); !os.IsNotExist(err) {
		t.Fatalf("expected the custom-dir install removed: %v", err)
	}
}

// `bmo update codex` updates the install tracked in codex's metadata location.
func TestUpdateAcceptsPositionalHarness(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	src := t.TempDir()
	writeSourceSkill(t, src, "alpha")

	if out, err := runBmo(t, home, "add", src, "codex", "--yes"); err != nil {
		t.Fatalf("codex install failed: %v\n%s", err, out)
	}
	bumpSourceSkill(t, src, "alpha")
	out, err := runBmo(t, home, "update", "codex")
	if err != nil {
		t.Fatalf("bmo update codex failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Updated alpha") {
		t.Fatalf("expected the codex install to update:\n%s", out)
	}
	installed, readErr := os.ReadFile(filepath.Join(home, ".agents", "skills", "alpha", "SKILL.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(installed), "alpha v2") {
		t.Fatalf("codex install did not carry the new content: %q", installed)
	}
	meta, metaErr := bmo.ReadMetadata(filepath.Join(home, ".bmo", "codex-skills.json"))
	if metaErr != nil {
		t.Fatal(metaErr)
	}
	if _, tracked := meta.Skills["alpha"]; !tracked {
		t.Fatal("expected alpha still tracked in codex metadata after update")
	}
}

// Contradictory scope directives are rejected instead of silently filtering
// every scope out (list) or letting one side win unannounced.
func TestContradictoryScopeSelectionsAreRejected(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	if out, err := runBmo(t, home, "list", "everywhere", "--project"); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("expected list everywhere --project to be rejected, got %v\n%s", err, out)
	}
	if out, err := runBmo(t, home, "update", "here", "--global"); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("expected update here --global to be rejected, got %v\n%s", err, out)
	}
}

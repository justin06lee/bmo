package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justin06lee/bmo/internal/bmo"
)

// shareHome isolates a home directory and makes both Claude and Codex look
// installed, so a sync has two participants without depending on what the
// machine running the tests happens to have.
func shareHome(t *testing.T) string {
	home := isolateHome(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("BMO_APPLICATIONS_DIRS", t.TempDir())
	for _, dir := range []string{".claude", ".codex"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// writeSkillSource writes a standalone skill folder and returns its path.
func writeSkillSource(t *testing.T, name, description string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\n" + description + "\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func namedSkillSource(t *testing.T, name string) string {
	t.Helper()
	return writeSkillSource(t, name, "name: "+name+"\ndescription: The "+name+" skill.")
}

func trackedNames(t *testing.T, harness string, scope bmo.Scope, dir string) []string {
	t.Helper()
	target, err := bmo.ResolveTarget(harness, scope, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := bmo.ReadMetadata(target.MetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(meta.Skills))
	for name := range meta.Skills {
		names = append(names, name)
	}
	return names
}

func TestShareGivesEveryHarnessTheUnionAndIsIdempotent(t *testing.T) {
	home := shareHome(t)
	t.Chdir(t.TempDir())
	claudeOnly := namedSkillSource(t, "from-claude")
	codexOnly := namedSkillSource(t, "from-codex")
	if out, err := runBmo(t, home, "add", claudeOnly, "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if out, err := runBmo(t, home, "add", codexOnly, "codex", "--yes"); err != nil {
		t.Fatalf("add codex: %v\n%s", err, out)
	}

	out, err := runBmo(t, home, "share", "--global", "--yes")
	if err != nil {
		t.Fatalf("bmo share: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Copied from-codex from codex to claude") ||
		!strings.Contains(out, "Copied from-claude from claude to codex") {
		t.Fatalf("expected a two-way sync, got %q", out)
	}
	for _, harness := range []string{"claude", "codex"} {
		names := trackedNames(t, harness, bmo.ScopeGlobal, "")
		for _, want := range []string{"from-claude", "from-codex"} {
			if !containsName(names, want) {
				t.Fatalf("%s tracks %v, missing %s", harness, names, want)
			}
		}
	}
	skillMD := filepath.Join(home, ".agents", "skills", "from-claude", "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		t.Fatalf("expected the copied skill on disk: %v", err)
	}

	// Additive means a rerun has nothing left to do.
	out, err = runBmo(t, home, "share", "--global", "--yes")
	if err != nil {
		t.Fatalf("bmo share (second run): %v\n%s", err, out)
	}
	if !strings.Contains(out, "every harness already has the same skills") {
		t.Fatalf("expected the second run to be a no-op, got %q", out)
	}
}

func TestShareKeepsTheDonorsUpstreamSource(t *testing.T) {
	home := shareHome(t)
	t.Chdir(t.TempDir())
	source := namedSkillSource(t, "demo")
	if out, err := runBmo(t, home, "add", source, "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if out, err := runBmo(t, home, "share", "--global", "--yes"); err != nil {
		t.Fatalf("share: %v\n%s", err, out)
	}

	// The copy is made from Claude's installed folder, but recording that
	// folder as the source would make Codex's `bmo update` follow Claude
	// instead of the real upstream.
	target, err := bmo.ResolveTarget("codex", bmo.ScopeGlobal, "", "")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := bmo.ReadMetadata(target.MetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := meta.Skills["demo"]
	if !ok {
		t.Fatalf("expected codex to track demo, got %v", meta.Skills)
	}
	if entry.Source != source {
		t.Fatalf("source = %q, want the original upstream %q", entry.Source, source)
	}
	if entry.Harness != bmo.HarnessCodex {
		t.Fatalf("harness = %q, want codex", entry.Harness)
	}
	if entry.InstalledPath != filepath.Join(target.SkillsDir, "demo") {
		t.Fatalf("installed path = %q, want it under %s", entry.InstalledPath, target.SkillsDir)
	}
}

func TestShareNeverOverwritesAnUntrackedFolder(t *testing.T) {
	home := shareHome(t)
	t.Chdir(t.TempDir())
	if out, err := runBmo(t, home, "add", namedSkillSource(t, "demo"), "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	squatter := filepath.Join(home, ".agents", "skills", "demo")
	if err := os.MkdirAll(squatter, 0o755); err != nil {
		t.Fatal(err)
	}
	handWritten := filepath.Join(squatter, "SKILL.md")
	if err := os.WriteFile(handWritten, []byte("hand written\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runBmo(t, home, "share", "--global", "--yes")
	if err != nil {
		t.Fatalf("bmo share: %v\n%s", err, out)
	}
	if !strings.Contains(out, "skipped demo for codex") || !strings.Contains(out, "does not track") {
		t.Fatalf("expected the collision to be reported as a skip, got %q", out)
	}
	data, err := os.ReadFile(handWritten)
	if err != nil || string(data) != "hand written\n" {
		t.Fatalf("expected the untracked folder untouched, got %q (%v)", data, err)
	}
}

func TestShareSkipsSkillsAPortableHarnessWouldReject(t *testing.T) {
	home := shareHome(t)
	t.Chdir(t.TempDir())
	// A legacy Claude skill with no declared name cannot install into a
	// portable harness, so the sync leaves it where it is instead of failing.
	legacy := writeSkillSource(t, "legacy", "description: A legacy Claude skill with no declared name.")
	if out, err := runBmo(t, home, "add", legacy, "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if out, err := runBmo(t, home, "add", namedSkillSource(t, "portable"), "--yes"); err != nil {
		t.Fatalf("add portable: %v\n%s", err, out)
	}

	out, err := runBmo(t, home, "share", "--global", "--yes")
	if err != nil {
		t.Fatalf("bmo share: %v\n%s", err, out)
	}
	if !strings.Contains(out, "skipped legacy for codex") {
		t.Fatalf("expected legacy to be skipped, got %q", out)
	}
	// One unshareable skill must not strand the rest.
	if !strings.Contains(out, "Copied portable from claude to codex") {
		t.Fatalf("expected the portable skill to still be shared, got %q", out)
	}
}

func TestShareFromOneHarnessSeedsTheOthersOnly(t *testing.T) {
	home := shareHome(t)
	t.Chdir(t.TempDir())
	if out, err := runBmo(t, home, "add", namedSkillSource(t, "from-claude"), "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if out, err := runBmo(t, home, "add", namedSkillSource(t, "from-codex"), "codex", "--yes"); err != nil {
		t.Fatalf("add codex: %v\n%s", err, out)
	}

	out, err := runBmo(t, home, "share", "claude", "--global", "--yes")
	if err != nil {
		t.Fatalf("bmo share claude: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Seeding every other harness from claude") {
		t.Fatalf("expected the one-way header, got %q", out)
	}
	if !strings.Contains(out, "Copied from-claude from claude to codex") {
		t.Fatalf("expected codex to be seeded, got %q", out)
	}
	if containsName(trackedNames(t, "claude", bmo.ScopeGlobal, ""), "from-codex") {
		t.Fatalf("expected claude to gain nothing when it is the donor")
	}
}

func TestShareDryRunWritesNothing(t *testing.T) {
	home := shareHome(t)
	t.Chdir(t.TempDir())
	if out, err := runBmo(t, home, "add", namedSkillSource(t, "demo"), "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	out, err := runBmo(t, home, "share", "--global", "--dry-run")
	if err != nil {
		t.Fatalf("bmo share --dry-run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Dry run: would copy") || !strings.Contains(out, "demo") {
		t.Fatalf("expected a dry-run summary naming demo, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "demo")); !os.IsNotExist(err) {
		t.Fatalf("expected nothing written, got %v", err)
	}
	if containsName(trackedNames(t, "codex", bmo.ScopeGlobal, ""), "demo") {
		t.Fatalf("expected codex metadata untouched by a dry run")
	}
}

func TestShareHereSyncsOnlyThisProject(t *testing.T) {
	home := shareHome(t)
	project := t.TempDir()
	t.Chdir(project)
	if out, err := runBmo(t, home, "add", "here", namedSkillSource(t, "project-skill"), "--yes"); err != nil {
		t.Fatalf("add here: %v\n%s", err, out)
	}

	out, err := runBmo(t, home, "share", "here", "--yes")
	if err != nil {
		t.Fatalf("bmo share here: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Copied project-skill from claude to codex") {
		t.Fatalf("expected the project sync, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "project-skill")); err != nil {
		t.Fatalf("expected the copy inside the project: %v", err)
	}
	// A project skill stays a project skill: nothing leaks into the global
	// destinations.
	if containsName(trackedNames(t, "claude", bmo.ScopeGlobal, ""), "project-skill") {
		t.Fatalf("expected the project skill to stay out of global metadata")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "project-skill")); !os.IsNotExist(err) {
		t.Fatalf("expected no global copy, got %v", err)
	}
}

func TestShareEverywhereReachesRegisteredProjects(t *testing.T) {
	home := shareHome(t)
	project := t.TempDir()
	t.Chdir(project)
	if out, err := runBmo(t, home, "add", "here", namedSkillSource(t, "project-skill"), "--yes"); err != nil {
		t.Fatalf("add here: %v\n%s", err, out)
	}
	// Run from somewhere else entirely: the registry is what carries the
	// sweep back to the project.
	t.Chdir(t.TempDir())

	out, err := runBmo(t, home, "share", "everywhere", "everyone", "--yes")
	if err != nil {
		t.Fatalf("bmo share everywhere everyone: %v\n%s", err, out)
	}
	if !strings.Contains(out, project) {
		t.Fatalf("expected the registered project to be visited, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "project-skill")); err != nil {
		t.Fatalf("expected the project's codex copy: %v", err)
	}
}

func TestShareRejectsContradictoryTargeting(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"skills-dir", []string{"share", "--skills-dir", "sk"}, "syncs the built-in harness presets"},
		{"harness beside positional", []string{"share", "codex", "--harness", "gemini"}, "cannot be combined"},
		{"unknown harness", []string{"share", "--harness", "bogus"}, "unknown harness"},
		{"contradictory scope", []string{"share", "here", "--global"}, "cannot be combined with --global"},
		{"both scope flags", []string{"share", "--project", "--global"}, "none of the others"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := shareHome(t)
			t.Chdir(t.TempDir())
			out, err := runBmo(t, home, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("bmo %s error = %v, want one containing %q\n%s", strings.Join(tc.args, " "), err, tc.want, out)
			}
		})
	}
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func TestShareFromAHarnessWithNothingToGiveSaysSo(t *testing.T) {
	home := shareHome(t)
	t.Chdir(t.TempDir())
	if out, err := runBmo(t, home, "add", namedSkillSource(t, "demo"), "--yes"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	// Naming a harness normally triggers the first-run install of the bundled
	// skill for it; mark it done so gemini genuinely holds nothing.
	marker, err := bmo.BootstrapMarkerPathFor(bmo.HarnessGemini)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Gemini is neither detected nor tracking anything, so naming it as the
	// donor must say that rather than claim everything is already in sync.
	out, err := runBmo(t, home, "share", "gemini", "--global", "--yes")
	if err != nil {
		t.Fatalf("bmo share gemini: %v\n%s", err, out)
	}
	if !strings.Contains(out, "gemini tracks no skills in any of these locations") {
		t.Fatalf("expected an absent-donor summary, got %q", out)
	}
	if containsName(trackedNames(t, "codex", bmo.ScopeGlobal, ""), "demo") {
		t.Fatalf("expected no copies when the named donor has nothing")
	}
}

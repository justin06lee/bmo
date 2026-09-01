package bmo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRegistryRecordAndList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	projects, err := RegisteredProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected empty registry, got %v", projects)
	}

	a := t.TempDir()
	b := t.TempDir()
	for _, dir := range []string{a, b, a} { // a twice: dedup
		if err := RecordProject(dir); err != nil {
			t.Fatal(err)
		}
	}
	projects, err = RegisteredProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 deduplicated projects, got %v", projects)
	}
	for _, p := range projects {
		if !filepath.IsAbs(p) {
			t.Fatalf("expected absolute paths, got %q", p)
		}
	}
}

func TestRecordProjectsReportsOnlyNewEntries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, b := t.TempDir(), t.TempDir()

	added, err := RecordProjects([]string{a, b, a})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 {
		t.Fatalf("expected both projects reported once, got %v", added)
	}
	// A second pass adds nothing, which is what makes re-running `bmo scout`
	// cheap and its "recorded N new" line meaningful.
	added, err = RecordProjects([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Fatalf("expected no new projects on a repeat sweep, got %v", added)
	}
	projects, err := RegisteredProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 registered projects, got %v", projects)
	}
}

func TestPruneProjectsDropsOnlyVanishedDirectories(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	live := t.TempDir()
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordProjects([]string{live, gone}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	removed, err := PruneProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != gone {
		t.Fatalf("pruned = %v, want only %s", removed, gone)
	}
	projects, err := RegisteredProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0] != live {
		t.Fatalf("expected only %s to survive, got %v", live, projects)
	}
	// A registry with nothing left to prune is left untouched.
	removed, err = PruneProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Fatalf("expected a second prune to remove nothing, got %v", removed)
	}
}

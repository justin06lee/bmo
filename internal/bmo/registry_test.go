package bmo

import (
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

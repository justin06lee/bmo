package bmo

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHashDirStableAcrossIdenticalTrees(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	for _, dir := range []string{a, b} {
		writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: demo\ndescription: d\n---\nbody\n")
		writeFile(t, filepath.Join(dir, "references", "notes.md"), "notes\n")
	}
	hashA, err := HashDir(a)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := HashDir(b)
	if err != nil {
		t.Fatal(err)
	}
	if hashA != hashB {
		t.Fatalf("identical trees hashed differently: %s vs %s", hashA, hashB)
	}
}

func TestHashDirDetectsContentAndPathChanges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: demo\ndescription: d\n---\nbody\n")
	base, err := HashDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: demo\ndescription: d\n---\nnew body\n")
	changed, err := HashDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if changed == base {
		t.Fatal("expected hash to change when file content changes")
	}

	writeFile(t, filepath.Join(dir, "extra.md"), "more\n")
	added, err := HashDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if added == changed {
		t.Fatal("expected hash to change when a file is added")
	}
}

func TestHashDirIgnoresIgnoredDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: demo\ndescription: d\n---\nbody\n")
	base, err := HashDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "node_modules", "junk.js"), "junk\n")
	writeFile(t, filepath.Join(dir, ".git", "HEAD"), "ref\n")
	withIgnored, err := HashDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if withIgnored != base {
		t.Fatal("expected ignored directories to not affect the hash")
	}
}

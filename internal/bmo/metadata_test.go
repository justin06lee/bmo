package bmo

import (
	"path/filepath"
	"testing"
)

func TestMetadataReadWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills.json")
	meta := EmptyMetadata()
	meta.Skills["demo"] = SkillMeta{Name: "demo", Scope: ScopeGlobal, Source: "./demo"}
	if err := WriteMetadata(path, meta); err != nil {
		t.Fatal(err)
	}
	got, err := ReadMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Skills["demo"].Name != "demo" {
		t.Fatalf("unexpected metadata: %+v", got)
	}
}

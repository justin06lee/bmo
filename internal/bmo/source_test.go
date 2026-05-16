package bmo

import "testing"

func TestParseGitHubSource(t *testing.T) {
	src, err := ParseSource("github:owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if src.Type != SourceGitHub || src.Owner != "owner" || src.Repo != "repo" || src.SubPath != "" || src.Ref != "" {
		t.Fatalf("unexpected source: %+v", src)
	}
}

func TestParseGitHubSourceWithPathAndRef(t *testing.T) {
	src, err := ParseSource("github:owner/repo/path/to/skill@feature")
	if err != nil {
		t.Fatal(err)
	}
	if src.Owner != "owner" || src.Repo != "repo" || src.SubPath != "path/to/skill" || src.Ref != "feature" {
		t.Fatalf("unexpected source: %+v", src)
	}
}

func TestParseGitHubShorthand(t *testing.T) {
	src, err := ParseSource("owner/repo/path@v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if src.Type != SourceGitHub || src.Owner != "owner" || src.Repo != "repo" || src.SubPath != "path" || src.Ref != "v1.0.0" {
		t.Fatalf("unexpected source: %+v", src)
	}
	if src.Raw != "owner/repo/path@v1.0.0" {
		t.Fatalf("unexpected raw: %s", src.Raw)
	}
}

func TestParseRelativePathStaysLocal(t *testing.T) {
	src, err := ParseSource("./owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if src.Type != SourceLocal {
		t.Fatalf("expected local source, got %+v", src)
	}
}

func TestParseZipURL(t *testing.T) {
	src, err := ParseSource("https://example.com/skill.zip")
	if err != nil {
		t.Fatal(err)
	}
	if src.Type != SourceZipURL || src.URL == "" {
		t.Fatalf("unexpected source: %+v", src)
	}
}

package bmo

import (
	"archive/zip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestParseEmbeddedSource(t *testing.T) {
	for _, raw := range []string{"bmo", "self"} {
		src, err := ParseSource(raw)
		if err != nil {
			t.Fatalf("ParseSource(%q): %v", raw, err)
		}
		if src.Type != SourceEmbedded || src.Raw != EmbeddedSkillName {
			t.Fatalf("ParseSource(%q) unexpected source: %+v", raw, src)
		}
	}
}

func TestIsEmbeddedSource(t *testing.T) {
	cases := map[string]bool{
		"bmo":               true,
		"self":              true,
		"github:owner/repo": false,
		"./x":               false,
		"https://x.zip":     false,
		"owner/repo":        false,
	}
	for raw, want := range cases {
		if got := IsEmbeddedSource(raw); got != want {
			t.Fatalf("IsEmbeddedSource(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestParseSourceErrors(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantSub string
	}{
		{"empty", "", "source is required"},
		{"non-zip URL", "https://example.com/skill", ".zip"},
		{"missing repo", "github:owner", "github:owner/repo"},
		{"empty ref", "github:owner/repo@", "ref cannot be empty"},
		{"invalid chars", "github:bad owner/repo", "invalid GitHub source component"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSource(tc.raw)
			if err == nil {
				t.Fatalf("ParseSource(%q) expected error", tc.raw)
			}
			if tc.wantSub != "" && !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("ParseSource(%q) error = %q, want substring %q", tc.raw, err, tc.wantSub)
			}
		})
	}
}

func TestMaterializeEmbeddedNilFS(t *testing.T) {
	prev := embeddedFS
	embeddedFS = nil
	defer func() { embeddedFS = prev }()
	_, err := materializeEmbedded()
	if err == nil || !strings.Contains(err.Error(), "this build does not bundle the bmo skill") {
		t.Fatalf("materializeEmbedded() error = %v, want bundle message", err)
	}
}

// writeZip builds a temp zip from the given name->content entries and returns its path.
func writeZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(fw, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

func TestUnzipRejectsZipSlip(t *testing.T) {
	for _, name := range []string{"../evil.txt", "../../etc/passwd"} {
		zipPath := writeZip(t, map[string]string{name: "pwned"})
		dest := t.TempDir()
		err := unzip(zipPath, dest)
		if err == nil || !strings.Contains(err.Error(), "unsafe path") {
			t.Fatalf("unzip(%q) error = %v, want unsafe path", name, err)
		}
		// Ensure nothing was written outside dest.
		parent := filepath.Dir(dest)
		if _, statErr := os.Stat(filepath.Join(parent, "evil.txt")); statErr == nil {
			t.Fatalf("zip-slip wrote file outside dest for %q", name)
		}
	}
}

func TestUnzipExtractsNormalEntry(t *testing.T) {
	zipPath := writeZip(t, map[string]string{"sub/hello.txt": "world"})
	dest := t.TempDir()
	if err := unzip(zipPath, dest); err != nil {
		t.Fatalf("unzip: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "sub", "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "world" {
		t.Fatalf("extracted content = %q, want %q", got, "world")
	}
}

// zeroReader yields an endless stream of zero bytes without allocating.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestUnzipRejectsDecompressionBomb(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "bomb.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	fw, err := w.Create("big.bin")
	if err != nil {
		t.Fatal(err)
	}
	// Highly compressible zeros that expand beyond the cap.
	if _, err := io.CopyN(fw, zeroReader{}, maxArchiveBytes+1); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	err = unzip(zipPath, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "archive expands beyond") {
		t.Fatalf("unzip bomb error = %v, want archive expands beyond", err)
	}
}

func TestDownloadAndExtractRejectsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		// Stream zeros until the client stops reading (it caps at the limit).
		io.CopyN(w, zeroReader{}, maxArchiveBytes+1)
	}))
	defer srv.Close()
	_, err := downloadAndExtract(srv.URL, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "download exceeds") {
		t.Fatalf("downloadAndExtract error = %v, want download exceeds", err)
	}
}

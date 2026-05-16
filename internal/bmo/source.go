package bmo

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type SourceType string

const (
	SourceGitHub SourceType = "github"
	SourceLocal  SourceType = "local"
	SourceZipURL SourceType = "zip"
)

type Source struct {
	Raw      string
	Type     SourceType
	Owner    string
	Repo     string
	SubPath  string
	Ref      string
	LocalDir string
	URL      string
}

type ResolvedSource struct {
	Source Source
	Root   string
	Temp   string
}

var githubPartRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func ParseSource(raw string) (Source, error) {
	if raw == "" {
		return Source{}, errors.New("source is required")
	}
	if strings.HasPrefix(raw, "github:") {
		return parseGitHubSource(raw)
	}
	if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return Source{}, fmt.Errorf("invalid URL source: %s", raw)
		}
		if !strings.HasSuffix(strings.ToLower(u.Path), ".zip") {
			return Source{}, errors.New("URL sources must point to a .zip file")
		}
		return Source{Raw: raw, Type: SourceZipURL, URL: raw}, nil
	}
	return Source{Raw: raw, Type: SourceLocal, LocalDir: raw}, nil
}

func parseGitHubSource(raw string) (Source, error) {
	body := strings.TrimPrefix(raw, "github:")
	if body == "" {
		return Source{}, errors.New("invalid GitHub source")
	}
	ref := ""
	if i := strings.LastIndex(body, "@"); i >= 0 {
		ref = body[i+1:]
		body = body[:i]
		if ref == "" {
			return Source{}, errors.New("GitHub ref cannot be empty")
		}
	}
	parts := strings.Split(body, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return Source{}, errors.New("GitHub source must be github:owner/repo[/path][@ref]")
	}
	for _, part := range parts[:2] {
		if !githubPartRE.MatchString(part) {
			return Source{}, fmt.Errorf("invalid GitHub source component: %s", part)
		}
	}
	return Source{
		Raw:     raw,
		Type:    SourceGitHub,
		Owner:   parts[0],
		Repo:    parts[1],
		SubPath: filepath.Join(parts[2:]...),
		Ref:     ref,
	}, nil
}

func ResolveSource(src Source) (ResolvedSource, error) {
	switch src.Type {
	case SourceLocal:
		abs, err := filepath.Abs(src.LocalDir)
		if err != nil {
			return ResolvedSource{}, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return ResolvedSource{}, err
		}
		if !info.IsDir() {
			return ResolvedSource{}, fmt.Errorf("local source is not a directory: %s", src.LocalDir)
		}
		return ResolvedSource{Source: src, Root: abs}, nil
	case SourceGitHub:
		return resolveGitHubSource(src)
	case SourceZipURL:
		return resolveZipURL(src)
	default:
		return ResolvedSource{}, errors.New("unsupported source type")
	}
}

func resolveGitHubSource(src Source) (ResolvedSource, error) {
	tmp, err := os.MkdirTemp("", "bmo-source-*")
	if err != nil {
		return ResolvedSource{}, err
	}
	ref := src.Ref
	if ref != "" {
		root, err := downloadAndExtract(fmt.Sprintf("https://github.com/%s/%s/archive/%s.zip", src.Owner, src.Repo, ref), tmp)
		if err != nil {
			os.RemoveAll(tmp)
			return ResolvedSource{}, err
		}
		return sourceWithSubpath(src, root, tmp)
	}
	root, err := downloadAndExtract(fmt.Sprintf("https://github.com/%s/%s/archive/refs/heads/main.zip", src.Owner, src.Repo), tmp)
	if err != nil {
		root, err = downloadAndExtract(fmt.Sprintf("https://github.com/%s/%s/archive/refs/heads/master.zip", src.Owner, src.Repo), tmp)
	}
	if err != nil {
		os.RemoveAll(tmp)
		return ResolvedSource{}, err
	}
	src.Ref = "main"
	return sourceWithSubpath(src, root, tmp)
}

func resolveZipURL(src Source) (ResolvedSource, error) {
	tmp, err := os.MkdirTemp("", "bmo-source-*")
	if err != nil {
		return ResolvedSource{}, err
	}
	root, err := downloadAndExtract(src.URL, tmp)
	if err != nil {
		os.RemoveAll(tmp)
		return ResolvedSource{}, err
	}
	return ResolvedSource{Source: src, Root: root, Temp: tmp}, nil
}

func sourceWithSubpath(src Source, root, tmp string) (ResolvedSource, error) {
	if src.SubPath == "" {
		return ResolvedSource{Source: src, Root: root, Temp: tmp}, nil
	}
	candidate := filepath.Join(root, src.SubPath)
	info, err := os.Stat(candidate)
	if err != nil {
		return ResolvedSource{}, fmt.Errorf("source path not found: %s", src.SubPath)
	}
	if !info.IsDir() {
		return ResolvedSource{}, fmt.Errorf("source path is not a directory: %s", src.SubPath)
	}
	return ResolvedSource{Source: src, Root: candidate, Temp: tmp}, nil
}

func downloadAndExtract(rawURL, dest string) (string, error) {
	resp, err := http.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download failed for %s: %s", rawURL, resp.Status)
	}
	zipPath := filepath.Join(dest, "source.zip")
	out, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	extractDir := filepath.Join(dest, "extract")
	if err := unzip(zipPath, extractDir); err != nil {
		return "", err
	}
	return singleExtractRoot(extractDir)
}

func unzip(zipPath, dest string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	cleanDest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cleanDest, 0o755); err != nil {
		return err
	}
	for _, file := range reader.File {
		target := filepath.Join(cleanDest, file.Name)
		cleanTarget, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if cleanTarget != cleanDest && !strings.HasPrefix(cleanTarget, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("zip contains unsafe path: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanTarget, file.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(cleanTarget, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		src.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func singleExtractRoot(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var dirs []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry)
		}
	}
	if len(entries) == 1 && len(dirs) == 1 {
		return filepath.Join(dir, dirs[0].Name()), nil
	}
	return dir, nil
}

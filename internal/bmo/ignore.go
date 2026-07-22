package bmo

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// IgnoreFileName is the per-skill exclude list. It lets a repository whose root
// is itself a skill ship tests, CI config, and demo assets without installing
// them into the user's skills directory.
const IgnoreFileName = ".bmoignore"

// Ignore is a compiled .bmoignore file. The zero value matches nothing, so an
// absent file needs no special-casing at the call sites.
type Ignore struct {
	patterns    []ignorePattern
	hasNegation bool
}

type ignorePattern struct {
	negate   bool
	dirOnly  bool
	anchored bool
	// segs holds the slash-separated pattern for anchored patterns; base holds
	// the single component matched against a path's basename at any depth.
	segs []string
	base string
}

// LoadIgnore reads dir/.bmoignore. A missing file yields an empty Ignore.
//
// The syntax is the familiar gitignore subset: one pattern per line, `#` starts
// a comment, a trailing `/` restricts a pattern to directories, a leading `!`
// negates, and a `/` anywhere else anchors the pattern to the skill root. `*`
// and `?` are glob wildcards within a path segment and `**` spans segments.
func LoadIgnore(dir string) (*Ignore, error) {
	file, err := os.Open(filepath.Join(dir, IgnoreFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return &Ignore{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	ignore := &Ignore{}
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		pattern, err := compileIgnorePattern(text)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", IgnoreFileName, line, err)
		}
		if pattern.negate {
			ignore.hasNegation = true
		}
		ignore.patterns = append(ignore.patterns, pattern)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ignore, nil
}

func compileIgnorePattern(text string) (ignorePattern, error) {
	var pattern ignorePattern
	if strings.HasPrefix(text, "!") {
		pattern.negate = true
		text = strings.TrimSpace(text[1:])
	}
	if strings.HasSuffix(text, "/") {
		pattern.dirOnly = true
		text = strings.TrimSuffix(text, "/")
	}
	text = strings.TrimPrefix(text, "./")
	if strings.HasPrefix(text, "/") {
		pattern.anchored = true
		text = strings.TrimPrefix(text, "/")
	}
	if text == "" {
		return ignorePattern{}, errors.New("pattern is empty")
	}
	if strings.Contains(text, "/") {
		pattern.anchored = true
		pattern.segs = strings.Split(text, "/")
	} else if pattern.anchored {
		pattern.segs = []string{text}
	} else {
		pattern.base = text
	}
	for _, seg := range append(pattern.segs, pattern.base) {
		if seg == "" || seg == "**" {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			return ignorePattern{}, fmt.Errorf("invalid pattern %q: %w", text, err)
		}
	}
	return pattern, nil
}

// Match reports whether a path relative to the skill root is excluded.
//
// A path inside an ignored directory stays ignored: as in git, a negation
// cannot re-include a file whose parent directory was excluded, which keeps the
// result independent of walk order.
func (i *Ignore) Match(rel string, isDir bool) bool {
	if i == nil || len(i.patterns) == 0 {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel == "" || rel == "." {
		return false
	}
	// SKILL.md is what makes the folder a skill; excluding it would produce an
	// install that cannot be validated, so it is never ignorable.
	if rel == "SKILL.md" {
		return false
	}
	parts := strings.Split(rel, "/")
	for idx := 1; idx < len(parts); idx++ {
		if i.decide(strings.Join(parts[:idx], "/"), true) {
			return true
		}
	}
	return i.decide(rel, isDir)
}

// decide evaluates every pattern against one path; the last match wins, which
// is what makes a later `!pattern` line an exception to an earlier one.
func (i *Ignore) decide(rel string, isDir bool) bool {
	ignored := false
	for _, pattern := range i.patterns {
		if pattern.dirOnly && !isDir {
			continue
		}
		if pattern.matches(rel) {
			ignored = !pattern.negate
		}
	}
	return ignored
}

func (p ignorePattern) matches(rel string) bool {
	if !p.anchored {
		ok, err := path.Match(p.base, path.Base(rel))
		return err == nil && ok
	}
	return matchSegments(p.segs, strings.Split(rel, "/"))
}

// matchSegments matches a slash-separated pattern against path segments, with
// `**` spanning zero or more of them.
func matchSegments(pattern, parts []string) bool {
	if len(pattern) == 0 {
		return len(parts) == 0
	}
	if pattern[0] == "**" {
		for idx := 0; idx <= len(parts); idx++ {
			if matchSegments(pattern[1:], parts[idx:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	ok, err := path.Match(pattern[0], parts[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pattern[1:], parts[1:])
}

// Len reports how many patterns are in effect, so callers can tell the user a
// .bmoignore shaped what they are about to install.
func (i *Ignore) Len() int {
	if i == nil {
		return 0
	}
	return len(i.patterns)
}

// CanPrune reports whether a walk may skip an ignored directory outright.
//
// With no negation patterns an ignored directory can never contain an included
// file, so skipping it is safe and avoids descending into large excluded trees.
// Once any negation exists, the walk has to look inside.
func (i *Ignore) CanPrune() bool {
	return i != nil && !i.hasNegation
}

// walkIgnored walks dir, applying its .bmoignore, and calls fn for every
// included file with its slash-relative path. Directory entries are not
// reported; both the copier and the hasher only care about files.
func walkIgnored(dir string, ignore *Ignore, fn func(path, rel string, d fs.DirEntry) error) error {
	return filepath.WalkDir(dir, func(current string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(dir, current)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if current != dir && ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			if current != dir && ignore.Match(rel, true) && ignore.CanPrune() {
				return filepath.SkipDir
			}
			return nil
		}
		if ignore.Match(rel, false) {
			return nil
		}
		return fn(current, rel, d)
	})
}

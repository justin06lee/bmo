package bmo

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// EmbeddedSkillName is the canonical name of the skill bundled into the bmo
// binary. `bmo add bmo`, `bmo add self`, and `bmo init` all install it.
const EmbeddedSkillName = "bmo"

// embeddedFS holds the bundled bmo skill. It is rooted at the skill folder, so
// SKILL.md sits at its top level. main wires this up at startup via
// SetEmbeddedFS; it is nil in builds (or tests) that don't register it.
var embeddedFS fs.FS

// SetEmbeddedFS registers the filesystem holding the bundled bmo skill.
func SetEmbeddedFS(fsys fs.FS) {
	embeddedFS = fsys
}

// IsEmbeddedSource reports whether raw refers to the bundled bmo skill.
func IsEmbeddedSource(raw string) bool {
	return raw == EmbeddedSkillName || raw == "self"
}

// materializeEmbedded copies the embedded skill tree into a fresh temp dir and
// returns the root path. The caller owns the directory and must remove it.
func materializeEmbedded() (string, error) {
	if embeddedFS == nil {
		return "", errors.New("this build does not bundle the bmo skill")
	}
	if _, err := fs.Stat(embeddedFS, "SKILL.md"); err != nil {
		return "", errors.New("bundled bmo skill is missing SKILL.md")
	}
	tmp, err := os.MkdirTemp("", "bmo-embedded-*")
	if err != nil {
		return "", err
	}
	err = fs.WalkDir(embeddedFS, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		target := filepath.Join(tmp, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(embeddedFS, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

package bmo

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
)

// HashDir returns a deterministic content hash of a skill directory: a
// SHA-256 over each regular file's relative path and contents, walked in
// lexical order. Ignored directories, .bmoignore'd paths, and non-regular
// files (symlinks etc.) are skipped, matching what CopyDir installs.
//
// Hashing the same file set the copier writes is what lets `bmo update`
// compare a source tree against an installed one: an excluded file must not
// register as a difference forever.
func HashDir(dir string) (string, error) {
	ignore, err := LoadIgnore(dir)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	err = walkIgnored(dir, ignore, func(path, rel string, d fs.DirEntry) error {
		if !d.Type().IsRegular() {
			return nil
		}
		io.WriteString(h, rel)
		h.Write([]byte{0})
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return err
		}
		h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

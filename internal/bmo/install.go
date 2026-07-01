package bmo

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type InstallOptions struct {
	Scope  Scope
	Name   string
	Force  bool
	DryRun bool
	CWD    string
	Source Source
	Skill  Skill
}

func InstallSkill(opts InstallOptions) (SkillMeta, error) {
	if opts.CWD == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return SkillMeta{}, err
		}
		opts.CWD = cwd
	}
	skillsDir, metadataPath, err := ScopePaths(opts.Scope, opts.CWD)
	if err != nil {
		return SkillMeta{}, err
	}
	skill := opts.Skill
	if opts.Name != "" || skill.Name == "" {
		skill, err = ValidateSkill(opts.Skill.Path, opts.Name)
		if err != nil {
			return SkillMeta{}, err
		}
	}
	dest := filepath.Join(skillsDir, skill.Name)
	meta, err := ReadMetadata(metadataPath)
	if err != nil {
		return SkillMeta{}, err
	}
	var existing *SkillMeta
	if got, ok := meta.Skills[skill.Name]; ok {
		existing = &got
	}
	next := NewSkillMeta(skill, opts.Scope, opts.Source, dest, existing)
	_, destErr := os.Stat(dest)
	if destErr == nil && !opts.Force {
		return SkillMeta{}, fmt.Errorf("skill already installed: %s; use --force to replace it", skill.Name)
	}
	if opts.DryRun {
		return next, nil
	}
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return SkillMeta{}, err
	}
	backup := ""
	if destErr == nil && opts.Force {
		backup = dest + ".bmo-backup-" + time.Now().UTC().Format("20060102150405")
		if err := os.Rename(dest, backup); err != nil {
			return SkillMeta{}, err
		}
	}
	if err := CopyDir(skill.Path, dest); err != nil {
		if backup != "" {
			os.RemoveAll(dest)
			os.Rename(backup, dest)
		}
		return SkillMeta{}, err
	}
	meta.Skills[skill.Name] = next
	if err := WriteMetadata(metadataPath, meta); err != nil {
		os.RemoveAll(dest)
		if backup != "" {
			os.Rename(backup, dest)
		}
		return SkillMeta{}, err
	}
	if backup != "" {
		os.RemoveAll(backup)
	}
	return next, nil
}

func RemoveSkill(name string, scope Scope, cwd string) (SkillMeta, error) {
	skillsDir, metadataPath, err := ScopePaths(scope, cwd)
	if err != nil {
		return SkillMeta{}, err
	}
	meta, err := ReadMetadata(metadataPath)
	if err != nil {
		return SkillMeta{}, err
	}
	entry, ok := meta.Skills[name]
	if !ok {
		return SkillMeta{}, fmt.Errorf("skill is not tracked by bmo: %s", name)
	}
	if err := withinDir(skillsDir, entry.InstalledPath); err != nil {
		return SkillMeta{}, fmt.Errorf("refusing to remove %s: %w", name, err)
	}
	if err := os.RemoveAll(entry.InstalledPath); err != nil {
		return SkillMeta{}, err
	}
	delete(meta.Skills, name)
	if err := WriteMetadata(metadataPath, meta); err != nil {
		return SkillMeta{}, err
	}
	return entry, nil
}

// withinDir reports an error unless target resolves to a path inside parent.
func withinDir(parent, target string) error {
	if target == "" {
		return errors.New("path is empty")
	}
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if absTarget != absParent && !strings.HasPrefix(absTarget, absParent+string(os.PathSeparator)) {
		return fmt.Errorf("path %s is outside %s", absTarget, absParent)
	}
	return nil
}

func CopyDir(src, dest string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !srcInfo.IsDir() {
		return errors.New("source is not a directory")
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("destination already exists: %s", dest)
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			if path != src && ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("refusing to copy symlink: %s", path)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

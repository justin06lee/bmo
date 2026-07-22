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
	agentsDir, err := AgentsDir(opts.Scope, opts.CWD)
	if err != nil {
		return SkillMeta{}, err
	}
	// Agent files this skill already owns are fair game to overwrite; anything
	// else in the agents directory belongs to the user or another skill.
	owned := map[string]bool{}
	if existing != nil {
		for _, file := range existing.Agents {
			owned[file] = true
		}
	}
	if !opts.Force {
		if conflicts := agentConflicts(skill.Agents, agentsDir, owned); len(conflicts) > 0 {
			return SkillMeta{}, fmt.Errorf(
				"subagent already exists and is not owned by %s: %s; use --force to replace it",
				skill.Name, strings.Join(conflicts, ", "))
		}
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
	restoreSkill := func() {
		os.RemoveAll(dest)
		if backup != "" {
			os.Rename(backup, dest)
		}
	}
	if err := CopyDir(skill.Path, dest); err != nil {
		restoreSkill()
		return SkillMeta{}, err
	}
	rollbackAgents, commitAgents, err := installAgents(skill.Agents, skill.Path, agentsDir)
	if err != nil {
		restoreSkill()
		return SkillMeta{}, err
	}
	meta.Skills[skill.Name] = next
	if err := WriteMetadata(metadataPath, meta); err != nil {
		rollbackAgents()
		restoreSkill()
		return SkillMeta{}, err
	}
	commitAgents()
	// Subagents the previous version shipped and this one dropped would
	// otherwise linger in the agents directory forever.
	if existing != nil {
		if stale := staleAgents(existing.Agents, skill.Agents); len(stale) > 0 {
			removeAgents(stale, agentsDir)
		}
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
	if len(entry.Agents) > 0 {
		agentsDir, err := AgentsDir(scope, cwd)
		if err != nil {
			return SkillMeta{}, err
		}
		if err := removeAgents(entry.Agents, agentsDir); err != nil {
			return SkillMeta{}, err
		}
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
	ignore, err := LoadIgnore(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return walkIgnored(src, ignore, func(path, rel string, d fs.DirEntry) error {
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("refusing to copy symlink: %s", path)
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

// copyFile copies one regular file, preserving its mode and refusing to
// clobber an existing destination.
func copyFile(src, dest string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("refusing to copy symlink: %s", src)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to copy non-regular file: %s", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

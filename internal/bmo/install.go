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
	Scope Scope
	// Target selects a coding harness destination. A zero Target preserves the
	// original Claude Code behavior using Scope and CWD.
	Target Target
	Name   string
	Force  bool
	DryRun bool
	CWD    string
	Source Source
	Skill  Skill
}

func InstallSkill(opts InstallOptions) (SkillMeta, error) {
	var err error
	if opts.CWD == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return SkillMeta{}, err
		}
		opts.CWD = cwd
	}
	target := opts.Target
	if target.SkillsDir == "" {
		target, err = ResolveTarget("", opts.Scope, opts.CWD, "")
		if err != nil {
			return SkillMeta{}, err
		}
	}
	// A partially built target would copy the skill tree and then fail (or
	// record nothing) at the metadata step, so refuse it before writing.
	if target.MetadataPath == "" || target.Scope == "" {
		return SkillMeta{}, errors.New("install target must carry a scope and metadata path; use ResolveTarget")
	}
	// Target is authoritative when supplied by a multi-harness caller.
	opts.Scope = target.Scope
	skillsDir, metadataPath := target.SkillsDir, target.MetadataPath
	skill := opts.Skill
	if opts.Name != "" || skill.Name == "" {
		skill, err = ValidateSkill(opts.Skill.Path, opts.Name)
		if err != nil {
			return SkillMeta{}, err
		}
	}
	if err := ValidateSkillForTarget(skill, target); err != nil {
		return SkillMeta{}, err
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
	next := NewSkillMetaForTarget(skill, target, opts.Source, dest, existing)
	// A dry run reports what would happen and writes nothing, so it returns
	// before the conflict checks a real install would fail on — the CLI
	// previews conflicts itself and deliberately lets --dry-run through.
	if opts.DryRun {
		return next, nil
	}
	_, destErr := os.Stat(dest)
	if destErr == nil && !opts.Force {
		return SkillMeta{}, fmt.Errorf("skill already installed: %s; use --force to replace it", skill.Name)
	}
	agentsDir := target.AgentsDir
	// Agent files this skill already owns are fair game to overwrite; anything
	// else in the agents directory belongs to the user or another skill.
	owned := map[string]bool{}
	if existing != nil {
		for _, file := range existing.Agents {
			owned[file] = true
		}
	}
	if target.SupportsAgents() && !opts.Force {
		if conflicts := agentConflicts(skill.Agents, agentsDir, owned); len(conflicts) > 0 {
			return SkillMeta{}, fmt.Errorf(
				"subagent already exists and is not owned by %s: %s; use --force to replace it",
				skill.Name, strings.Join(conflicts, ", "))
		}
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
	agents := skill.Agents
	if !target.SupportsAgents() {
		agents = nil
	}
	rollbackAgents, commitAgents, err := installAgents(agents, skill.Path, agentsDir)
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
	// otherwise linger in the agents directory forever. Targets without an agent
	// destination are skipped: agentsDir is empty there, so tracked filenames
	// would resolve against the process working directory and delete whatever
	// happens to share their name.
	if existing != nil && target.SupportsAgents() {
		if stale := staleAgents(existing.Agents, skill.Agents); len(stale) > 0 {
			// The install itself succeeded, so a failed cleanup is reported
			// rather than silently swallowed or turned into a fatal error.
			if err := removeAgents(stale, agentsDir); err != nil {
				fmt.Fprintf(os.Stderr, "bmo: warning: could not remove stale subagents of %s: %v\n", skill.Name, err)
			}
		}
	}
	if backup != "" {
		os.RemoveAll(backup)
	}
	// Remember which repos bmo has installed into so `bmo update everywhere`
	// can find them later. Best-effort: a registry failure must not fail an
	// install that already succeeded.
	if opts.Scope == ScopeProject {
		_ = RecordProject(opts.CWD)
	}
	return next, nil
}

// Conflict describes something that would be overwritten by an install.
type Conflict struct {
	Skill string
	// Path is the installed skill directory, set when the skill already exists.
	Path string
	// Agents lists subagent files owned by someone else, if any.
	Agents []string
}

// CheckInstallConflictsForTarget reports what an install of skill would
// overwrite at one resolved destination.
//
// Batch installs use this to decide before writing anything: a partial install
// that stops halfway through leaves the user with an incoherent set of skills,
// so every conflict is surfaced up front.
func CheckInstallConflictsForTarget(skill Skill, target Target) (Conflict, error) {
	skillsDir, metadataPath := target.SkillsDir, target.MetadataPath
	conflict := Conflict{Skill: skill.Name}
	dest := filepath.Join(skillsDir, skill.Name)
	if _, err := os.Stat(dest); err == nil {
		conflict.Path = dest
	}
	meta, err := ReadMetadata(metadataPath)
	if err != nil {
		return Conflict{}, err
	}
	owned := map[string]bool{}
	if existing, ok := meta.Skills[skill.Name]; ok {
		for _, file := range existing.Agents {
			owned[file] = true
		}
	}
	if target.SupportsAgents() {
		conflict.Agents = agentConflicts(skill.Agents, target.AgentsDir, owned)
	}
	return conflict, nil
}

// Empty reports whether the install would overwrite nothing.
func (c Conflict) Empty() bool {
	return c.Path == "" && len(c.Agents) == 0
}

// RemoveSkillFromTarget removes a skill from one resolved harness destination.
func RemoveSkillFromTarget(name string, target Target) (SkillMeta, error) {
	skillsDir, metadataPath := target.SkillsDir, target.MetadataPath
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
	// Every reason to refuse is checked before the first deletion. Rejecting a
	// removal after the skill directory is gone would leave the metadata entry
	// pointing at files that no longer exist, which `bmo list` still reports and
	// the user cannot restore.
	if len(entry.Agents) > 0 && !target.SupportsAgents() {
		return SkillMeta{}, fmt.Errorf("metadata tracks subagents but harness %s has no compatible agent destination", target.Harness)
	}
	if err := os.RemoveAll(entry.InstalledPath); err != nil {
		return SkillMeta{}, err
	}
	if len(entry.Agents) > 0 {
		if err := removeAgents(entry.Agents, target.AgentsDir); err != nil {
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

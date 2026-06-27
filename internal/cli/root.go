package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/justin06lee/bmo/internal/bmo"
	"github.com/spf13/cobra"
)

type options struct {
	project bool
	global  bool
	name    string
	force   bool
	yes     bool
	dryRun  bool
	json    bool
	all     bool
}

func NewRootCommand() *cobra.Command {
	opts := &options{}
	root := &cobra.Command{
		Use:          "bmo",
		Short:        "A tiny installer for Claude Code skills",
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if shouldBootstrap(cmd, args) {
				bootstrapBmoSkill(cmd)
			}
		},
	}
	root.AddCommand(newAddCommand(opts))
	root.AddCommand(newInitCommand(opts))
	root.AddCommand(newInspectCommand())
	root.AddCommand(newListCommand(opts))
	root.AddCommand(newRemoveCommand(opts))
	root.AddCommand(newUpdateCommand(opts))
	root.AddCommand(newDoctorCommand())
	return root
}

func newAddCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add SOURCE",
		Short: "Install a Claude Code skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			scope := selectedScope(opts)
			src, err := bmo.ParseSource(args[0])
			if err != nil {
				return err
			}
			resolved, err := bmo.ResolveSource(src)
			if err != nil {
				return err
			}
			defer cleanupResolved(resolved)
			skill, err := selectSkill(resolved.Root, opts.name)
			if err != nil {
				return err
			}
			if opts.name != "" {
				skill, err = bmo.ValidateSkill(skill.Path, opts.name)
				if err != nil {
					return err
				}
			}
			skillsDir, _, err := bmo.ScopePaths(scope, cwd)
			if err != nil {
				return err
			}
			dest := filepath.Join(skillsDir, skill.Name)
			printSkillPreview(cmd, skill, src.Raw, scope, dest)
			if !opts.yes && !opts.dryRun {
				ok, err := confirm(cmd, "Install? [y/N] ")
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("install cancelled")
				}
			}
			meta, err := bmo.InstallSkill(bmo.InstallOptions{
				Scope:  scope,
				Name:   opts.name,
				Force:  opts.force,
				DryRun: opts.dryRun,
				CWD:    cwd,
				Source: resolved.Source,
				Skill:  skill,
			})
			if err != nil {
				return err
			}
			if opts.dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Dry run: would install %s to %s\n", meta.Name, meta.InstalledPath)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Installed %s to %s\n\nUse it in Claude Code:\n  /%s\n", meta.Name, meta.InstalledPath, meta.Name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.project, "project", false, "Install into ./.claude/skills")
	cmd.Flags().StringVar(&opts.name, "name", "", "Override destination skill folder name")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Replace an existing installed skill")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Skip interactive confirmation")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show what would happen without copying files")
	return cmd
}

func newInitCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Install the bundled bmo skill into Claude Code",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			scope := selectedScope(opts)
			meta, err := installBmoSkill(scope, cwd, true)
			if err != nil {
				return err
			}
			markBootstrapped()
			fmt.Fprintf(cmd.OutOrStdout(), "Installed %s to %s\n\nUse it in Claude Code:\n  /%s\n", meta.Name, meta.InstalledPath, meta.Name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.project, "project", false, "Install into ./.claude/skills")
	return cmd
}

func newInspectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect SOURCE",
		Short: "Inspect skills without installing them",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := bmo.ParseSource(args[0])
			if err != nil {
				return err
			}
			resolved, err := bmo.ResolveSource(src)
			if err != nil {
				return err
			}
			defer cleanupResolved(resolved)
			skills, err := bmo.DiscoverSkills(resolved.Root)
			if err != nil {
				return err
			}
			if len(skills) == 0 {
				return errors.New("no skills found")
			}
			for _, skill := range skills {
				fmt.Fprintf(cmd.OutOrStdout(), "Path: %s\nName: %s\nDescription: %s\nFiles: %d\n", skill.Path, skill.Name, skill.Description, skill.FileCount)
				if len(skill.NotableFiles) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "Notable files: %s\n", strings.Join(skill.NotableFiles, ", "))
				}
				if len(skill.Warnings) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "Warnings: %s\n", strings.Join(skill.Warnings, "; "))
				}
				if skill.Name == "" {
					fmt.Fprintln(cmd.OutOrStdout(), "Validation: failed")
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "Validation: ok")
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return nil
		},
	}
}

func newListCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills tracked by bmo",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			entries, err := listEntries(cwd, opts)
			if err != nil {
				return err
			}
			if opts.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(entries)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "NAME\tSCOPE\tSOURCE\tPATH\tUPDATED")
			for _, entry := range entries {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", entry.Name, entry.Scope, entry.Source, entry.InstalledPath, entry.UpdatedAt)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.project, "project", false, "Show only project installs")
	cmd.Flags().BoolVar(&opts.global, "global", false, "Show only global installs")
	cmd.Flags().BoolVar(&opts.json, "json", false, "Output JSON")
	return cmd
}

func newRemoveCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove SKILL_NAME",
		Short: "Remove an installed skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			scope := selectedScope(opts)
			_, metadataPath, err := bmo.ScopePaths(scope, cwd)
			if err != nil {
				return err
			}
			meta, err := bmo.ReadMetadata(metadataPath)
			if err != nil {
				return err
			}
			entry, ok := meta.Skills[args[0]]
			if !ok {
				return fmt.Errorf("skill exists on disk but not in metadata, or is not installed: %s\nTry: bmo doctor", args[0])
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Remove %s from %s\n", entry.Name, entry.InstalledPath)
			if !opts.yes {
				ok, err := confirm(cmd, "Remove? [y/N] ")
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("remove cancelled")
				}
			}
			removed, err := bmo.RemoveSkill(args[0], scope, cwd)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", removed.Name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.project, "project", false, "Use project metadata")
	cmd.Flags().BoolVar(&opts.global, "global", false, "Use global metadata")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Skip interactive confirmation")
	return cmd
}

func newUpdateCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update SKILL_NAME",
		Short: "Update installed skills from their original source",
		Args: func(cmd *cobra.Command, args []string) error {
			if opts.all {
				return cobra.NoArgs(cmd, args)
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			scopes := []bmo.Scope{selectedScope(opts)}
			if opts.all && !opts.project && !opts.global {
				scopes = []bmo.Scope{bmo.ScopeGlobal, bmo.ScopeProject}
			}
			for _, scope := range scopes {
				if err := updateScope(cmd, cwd, scope, args, opts); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.all, "all", false, "Update all tracked skills")
	cmd.Flags().BoolVar(&opts.project, "project", false, "Use project metadata")
	cmd.Flags().BoolVar(&opts.global, "global", false, "Use global metadata")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Skip interactive confirmation")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show what would happen without copying files")
	return cmd
}

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check local bmo and Claude Code skill setup",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "bmo doctor")
			fmt.Fprintln(cmd.OutOrStdout())
			for _, check := range bmo.RunDoctor(cwd) {
				fmt.Fprintf(cmd.OutOrStdout(), "%-7s %s\n", check.Status, check.Message)
			}
			return nil
		},
	}
}

func selectedScope(opts *options) bmo.Scope {
	if opts.project {
		return bmo.ScopeProject
	}
	return bmo.ScopeGlobal
}

func selectSkill(root, name string) (bmo.Skill, error) {
	skills, err := bmo.DiscoverSkills(root)
	if err != nil {
		return bmo.Skill{}, err
	}
	if len(skills) == 0 {
		return bmo.Skill{}, errors.New("no skills found")
	}
	if name != "" {
		for _, skill := range skills {
			if skill.Name == name || filepath.Base(skill.Path) == name {
				return skill, nil
			}
		}
	}
	if len(skills) == 1 {
		return skills[0], nil
	}
	var names []string
	for _, skill := range skills {
		names = append(names, fmt.Sprintf("%s (%s)", skill.Name, skill.Path))
	}
	return bmo.Skill{}, fmt.Errorf("multiple skills found; use --name to choose one: %s", strings.Join(names, ", "))
}

func printSkillPreview(cmd *cobra.Command, skill bmo.Skill, source string, scope bmo.Scope, dest string) {
	fmt.Fprintf(cmd.OutOrStdout(), "Found skill: %s\nDescription: %s\n\nSource: %s\nScope: %s\nDestination: %s\nFiles: %d\n", skill.Name, skill.Description, source, scope, dest, skill.FileCount)
	if len(skill.ExecutableFiles) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "\nThis skill includes executable-looking files:")
		for _, file := range skill.ExecutableFiles {
			fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", file)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "\nSkills may include executable code. Review third-party skills before use.")
	}
	fmt.Fprintln(cmd.OutOrStdout())
}

func confirm(cmd *cobra.Command, prompt string) (bool, error) {
	fmt.Fprint(cmd.OutOrStdout(), prompt)
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// installBmoSkill installs the bundled bmo skill from the embedded copy.
func installBmoSkill(scope bmo.Scope, cwd string, force bool) (bmo.SkillMeta, error) {
	src, err := bmo.ParseSource(bmo.EmbeddedSkillName)
	if err != nil {
		return bmo.SkillMeta{}, err
	}
	resolved, err := bmo.ResolveSource(src)
	if err != nil {
		return bmo.SkillMeta{}, err
	}
	defer cleanupResolved(resolved)
	skill, err := selectSkill(resolved.Root, "")
	if err != nil {
		return bmo.SkillMeta{}, err
	}
	return bmo.InstallSkill(bmo.InstallOptions{
		Scope:  scope,
		Force:  force,
		CWD:    cwd,
		Source: resolved.Source,
		Skill:  skill,
	})
}

// shouldBootstrap reports whether the one-time first-run install should run for
// this command. It is skipped for the commands that install the skill
// themselves, to avoid a redundant double install.
func shouldBootstrap(cmd *cobra.Command, args []string) bool {
	switch cmd.Name() {
	case "init":
		return false
	case "add":
		if len(args) == 1 && bmo.IsEmbeddedSource(args[0]) {
			return false
		}
	}
	return true
}

// bootstrapBmoSkill installs the bundled bmo skill once, the first time bmo is
// run. A sentinel file records that it happened so a later `bmo remove bmo`
// sticks. All failures are non-fatal — bmo should still run without it.
func bootstrapBmoSkill(cmd *cobra.Command) {
	marker, err := bmo.BootstrapMarkerPath()
	if err != nil {
		return
	}
	if _, err := os.Stat(marker); err == nil {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	if !bmoSkillTracked(cwd) {
		if meta, err := installBmoSkill(bmo.ScopeGlobal, cwd, false); err == nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "bmo: installed the bmo skill to %s (run `bmo remove bmo` to undo)\n", meta.InstalledPath)
		}
	}
	markBootstrapped()
}

// bmoSkillTracked reports whether the bmo skill is already recorded in global
// metadata.
func bmoSkillTracked(cwd string) bool {
	path, err := bmo.GlobalMetadataPath()
	if err != nil {
		return false
	}
	meta, err := bmo.ReadMetadata(path)
	if err != nil {
		return false
	}
	_, ok := meta.Skills[bmo.EmbeddedSkillName]
	return ok
}

// markBootstrapped writes the sentinel file recording the one-time install.
func markBootstrapped() {
	marker, err := bmo.BootstrapMarkerPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return
	}
	os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
}

func cleanupResolved(resolved bmo.ResolvedSource) {
	if resolved.Temp != "" {
		os.RemoveAll(resolved.Temp)
	}
}

func listEntries(cwd string, opts *options) ([]bmo.SkillMeta, error) {
	var entries []bmo.SkillMeta
	if !opts.project {
		path, err := bmo.GlobalMetadataPath()
		if err != nil {
			return nil, err
		}
		meta, err := bmo.ReadMetadata(path)
		if err != nil {
			return nil, err
		}
		for _, entry := range meta.Skills {
			entries = append(entries, entry)
		}
	}
	if !opts.global {
		meta, err := bmo.ReadMetadata(bmo.ProjectMetadataPath(cwd))
		if err != nil {
			return nil, err
		}
		for _, entry := range meta.Skills {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func updateScope(cmd *cobra.Command, cwd string, scope bmo.Scope, args []string, opts *options) error {
	_, metadataPath, err := bmo.ScopePaths(scope, cwd)
	if err != nil {
		return err
	}
	meta, err := bmo.ReadMetadata(metadataPath)
	if err != nil {
		return err
	}
	targets := map[string]bmo.SkillMeta{}
	if opts.all {
		targets = meta.Skills
	} else if entry, ok := meta.Skills[args[0]]; ok {
		targets[args[0]] = entry
	} else {
		return fmt.Errorf("skill is not tracked by bmo: %s", args[0])
	}
	for name, entry := range targets {
		src, err := bmo.ParseSource(entry.Source)
		if err != nil {
			return err
		}
		resolved, err := bmo.ResolveSource(src)
		if err != nil {
			return err
		}
		skill, err := selectSkill(resolved.Root, name)
		if err == nil {
			skill, err = bmo.ValidateSkill(skill.Path, name)
		}
		if err != nil {
			cleanupResolved(resolved)
			return err
		}
		_, err = bmo.InstallSkill(bmo.InstallOptions{Scope: scope, Name: name, Force: true, DryRun: opts.dryRun, CWD: cwd, Source: resolved.Source, Skill: skill})
		cleanupResolved(resolved)
		if err != nil {
			return err
		}
		if opts.dryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "Dry run: would update %s\n", name)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Updated %s\n", name)
		}
	}
	return nil
}

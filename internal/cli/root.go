package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
		Use:           "bmo",
		Short:         "A tiny installer for Claude Code skills",
		Version:       buildVersion(),
		SilenceUsage:  true,
		SilenceErrors: true, // main prints the returned error once
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
	root.AddCommand(newUpgradeCommand())
	return root
}

func newAddCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add SOURCE [here|everywhere]",
		Short: "Install a Claude Code skill",
		Args:  argsWithKeyword(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			args, keyword, err := splitScopeKeyword(args)
			if err != nil {
				return err
			}
			scope := keywordScope(keyword, opts)
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
			if _, err := os.Stat(dest); err == nil && !opts.force && !opts.dryRun {
				return fmt.Errorf("skill already installed: %s; use --force to replace it", skill.Name)
			}
			agentsDir, err := bmo.AgentsDir(scope, cwd)
			if err != nil {
				return err
			}
			printSkillPreview(cmd, skill, src.Raw, scope, dest, agentsDir)
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
			fmt.Fprintf(cmd.OutOrStdout(), "Installed %s to %s\n", meta.Name, meta.InstalledPath)
			if len(meta.Agents) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Installed %d subagents to %s: %s\n",
					len(meta.Agents), agentsDir, strings.Join(bmo.AgentNames(skill.Agents), ", "))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nUse it in Claude Code:\n  /%s\n", meta.Name)
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
		Use:   "init [here|everywhere]",
		Short: "Install the bundled bmo skill into Claude Code",
		Args:  argsWithKeyword(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			_, keyword, err := splitScopeKeyword(args)
			if err != nil {
				return err
			}
			scope := keywordScope(keyword, opts)
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
				if len(skill.Agents) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "Subagents: %s\n", strings.Join(bmo.AgentNames(skill.Agents), ", "))
				}
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
		Use:   "list [here|everywhere]",
		Short: "List installed skills tracked by bmo",
		Args:  argsWithKeyword(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			_, keyword, err := splitScopeKeyword(args)
			if err != nil {
				return err
			}
			applyKeywordFilter(keyword, opts)
			entries, err := listEntries(cwd, opts)
			if err != nil {
				return err
			}
			if opts.json {
				if entries == nil {
					entries = []bmo.SkillMeta{}
				}
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
		Use:   "remove SKILL_NAME [here|everywhere]",
		Short: "Remove an installed skill",
		Args:  argsWithKeyword(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			args, keyword, err := splitScopeKeyword(args)
			if err != nil {
				return err
			}
			scope := keywordScope(keyword, opts)
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
				return fmt.Errorf("skill is not tracked by bmo in %s scope: %s\nTry: bmo list, or bmo doctor", scope, args[0])
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Remove %s from %s\n", entry.Name, entry.InstalledPath)
			if len(entry.Agents) > 0 {
				agentsDir, err := bmo.AgentsDir(scope, cwd)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Also removes %d subagents from %s: %s\n",
					len(entry.Agents), agentsDir, strings.Join(entry.Agents, ", "))
			}
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
		Use:   "update [SKILL_NAME] [here|everywhere]",
		Short: "Update installed skills whose source content changed",
		Args: argsWithKeyword(func(cmd *cobra.Command, args []string) error {
			if opts.all {
				return cobra.NoArgs(cmd, args)
			}
			return cobra.MaximumNArgs(1)(cmd, args)
		}),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			args, keyword, err := splitScopeKeyword(args)
			if err != nil {
				return err
			}
			applyKeywordFilter(keyword, opts)
			if len(args) == 0 {
				opts.all = true
			}
			scopes := []bmo.Scope{selectedScope(opts)}
			if opts.all && !opts.project && !opts.global {
				scopes = []bmo.Scope{bmo.ScopeGlobal, bmo.ScopeProject}
			}
			// Skills tracked from the same source share one download per run.
			cache := map[string]bmo.ResolvedSource{}
			defer func() {
				for _, resolved := range cache {
					cleanupResolved(resolved)
				}
			}()
			for _, scope := range scopes {
				if err := updateScope(cmd, cwd, scope, args, opts, cache); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.all, "all", false, "Update all tracked skills (default when no name is given)")
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

// splitScopeKeyword pulls an optional "here" / "everywhere" location keyword out
// of a command's positional args. "here" means the current project,
// "everywhere" means the global install. It returns the args with the keyword
// removed plus the keyword that was found (empty string if none). Specifying
// more than one keyword is an error.
func splitScopeKeyword(args []string) (rest []string, keyword string, err error) {
	for _, arg := range args {
		if arg == "here" || arg == "everywhere" {
			if keyword != "" {
				return nil, "", errors.New("specify only one location keyword (here or everywhere)")
			}
			keyword = arg
			continue
		}
		rest = append(rest, arg)
	}
	return rest, keyword, nil
}

// keywordScope resolves the scope for commands that act on a single scope (add,
// remove). A keyword wins; otherwise the --project/--global flags decide, and
// the default is global ("everywhere").
func keywordScope(keyword string, opts *options) bmo.Scope {
	switch keyword {
	case "here":
		return bmo.ScopeProject
	case "everywhere":
		return bmo.ScopeGlobal
	default:
		return selectedScope(opts)
	}
}

// applyKeywordFilter folds a location keyword into the --project/--global flag
// pair used by list and update, where "no keyword and no flag" keeps the
// existing both-scopes behavior.
func applyKeywordFilter(keyword string, opts *options) {
	switch keyword {
	case "here":
		opts.project = true
	case "everywhere":
		opts.global = true
	}
}

// argsWithKeyword wraps a cobra positional-args validator so it counts args
// after an optional location keyword has been stripped out.
func argsWithKeyword(base cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		rest, _, err := splitScopeKeyword(args)
		if err != nil {
			return err
		}
		if base == nil {
			return nil
		}
		return base(cmd, rest)
	}
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

func printSkillPreview(cmd *cobra.Command, skill bmo.Skill, source string, scope bmo.Scope, dest, agentsDir string) {
	fmt.Fprintf(cmd.OutOrStdout(), "Found skill: %s\nDescription: %s\n\nSource: %s\nScope: %s\nDestination: %s\nFiles: %d\n", skill.Name, skill.Description, source, scope, dest, skill.FileCount)
	if len(skill.Agents) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "Subagents: %s\nSubagent destination: %s\n",
			strings.Join(bmo.AgentNames(skill.Agents), ", "), agentsDir)
	}
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
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "completion" {
			return false
		}
	}
	switch cmd.Name() {
	case "init", "help", "__complete", "__completeNoDesc":
		return false
	case "add":
		rest, _, err := splitScopeKeyword(args)
		if err == nil && len(rest) == 1 && bmo.IsEmbeddedSource(rest[0]) {
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
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Scope != entries[j].Scope {
			return entries[i].Scope < entries[j].Scope
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

func updateScope(cmd *cobra.Command, cwd string, scope bmo.Scope, args []string, opts *options, cache map[string]bmo.ResolvedSource) error {
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
		return fmt.Errorf("skill is not tracked by bmo in %s scope: %s (try 'bmo update %s here' or '--all')", scope, args[0], args[0])
	}
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := targets[name]
		resolved, ok := cache[entry.Source]
		if !ok {
			src, err := bmo.ParseSource(entry.Source)
			if err != nil {
				return err
			}
			resolved, err = bmo.ResolveSource(src)
			if err != nil {
				return err
			}
			cache[entry.Source] = resolved
		}
		skill, err := selectSkill(resolved.Root, name)
		if err == nil {
			skill, err = bmo.ValidateSkill(skill.Path, name)
		}
		if err != nil {
			return err
		}
		if !skillChanged(skill.Path, entry.InstalledPath) {
			fmt.Fprintf(cmd.OutOrStdout(), "%s is up to date\n", name)
			continue
		}
		_, err = bmo.InstallSkill(bmo.InstallOptions{Scope: scope, Name: name, Force: true, DryRun: opts.dryRun, CWD: cwd, Source: resolved.Source, Skill: skill})
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

// skillChanged reports whether the resolved source content differs from the
// installed copy. Hash failures (e.g. a missing install dir) count as changed
// so the update proceeds and repairs the install.
func skillChanged(sourceDir, installedDir string) bool {
	installedHash, err := bmo.HashDir(installedDir)
	if err != nil {
		return true
	}
	sourceHash, err := bmo.HashDir(sourceDir)
	if err != nil {
		return true
	}
	return sourceHash != installedHash
}

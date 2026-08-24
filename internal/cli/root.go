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
	project   bool
	global    bool
	harness   string
	skillsDir string
	name      string
	force     bool
	yes       bool
	dryRun    bool
	json      bool
	all       bool
}

func NewRootCommand() *cobra.Command {
	opts := &options{}
	root := &cobra.Command{
		Use:           "bmo",
		Short:         "A tiny installer for coding-agent skills",
		Version:       buildVersion(),
		SilenceUsage:  true,
		SilenceErrors: true, // main prints the returned error once
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if shouldBootstrap(cmd, args) {
				bootstrapBmoSkillForOptions(cmd, args, opts)
			}
		},
	}
	root.AddCommand(newAddCommand(opts))
	root.AddCommand(newInitCommand(opts))
	root.AddCommand(newInspectCommand())
	root.AddCommand(newListCommand(opts))
	root.AddCommand(newRemoveCommand(opts))
	root.AddCommand(newUpdateCommand(opts))
	root.AddCommand(newDoctorCommand(opts))
	root.AddCommand(newHarnessesCommand())
	root.AddCommand(newUpgradeCommand())
	return root
}

func newAddCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add SOURCE [here|everywhere] [HARNESS|everyone]",
		Short: "Install a coding-agent skill",
		Args: func(cmd *cobra.Command, args []string) error {
			rest, _, _, err := splitAddKeywords(args)
			if err != nil {
				return err
			}
			return cobra.ExactArgs(1)(cmd, rest)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			args, keyword, positionalHarness, err := splitAddKeywords(args)
			if err != nil {
				return err
			}
			effective := *opts
			if positionalHarness != "" {
				if opts.harness != "" || opts.skillsDir != "" {
					return errors.New("a positional harness cannot be combined with --harness or --skills-dir")
				}
				effective.harness = positionalHarness
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
			if positionalHarness == "everyone" {
				return addEveryone(cmd, resolved, scope, cwd, &effective)
			}
			target, err := targetFor(scope, cwd, &effective)
			if err != nil {
				return err
			}
			if effective.all {
				return addAll(cmd, resolved, src, scope, cwd, &effective)
			}
			skill, err := selectSkill(resolved.Root, effective.name)
			if err != nil {
				return err
			}
			if effective.name != "" {
				skill, err = bmo.ValidateSkill(skill.Path, effective.name)
				if err != nil {
					return err
				}
			}
			if err := bmo.ValidateSkillForTarget(skill, target); err != nil {
				return err
			}
			dest := filepath.Join(target.SkillsDir, skill.Name)
			if _, err := os.Stat(dest); err == nil && !effective.force && !effective.dryRun {
				return fmt.Errorf("skill already installed: %s; use --force to replace it", skill.Name)
			}
			printSkillPreview(cmd, skill, src.Raw, target, dest)
			if !effective.yes && !effective.dryRun {
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
				Target: target,
				Name:   effective.name,
				Force:  effective.force,
				DryRun: effective.dryRun,
				CWD:    cwd,
				Source: resolved.Source,
				Skill:  skill,
			})
			if err != nil {
				return err
			}
			if effective.dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Dry run: would install %s to %s\n", meta.Name, meta.InstalledPath)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Installed %s to %s\n", meta.Name, meta.InstalledPath)
			if len(meta.Agents) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Installed %d subagents to %s: %s\n",
					len(meta.Agents), target.AgentsDir, strings.Join(bmo.AgentNames(skill.Agents), ", "))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nUse it in %s:\n  %s\n", target.Harness, target.InvocationHint(meta.Name))
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.project, "project", false, "Install into the harness's project skills directory")
	cmd.Flags().StringVar(&opts.name, "name", "", "Override destination skill folder name")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Replace an existing installed skill")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Skip interactive confirmation")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show what would happen without copying files")
	cmd.Flags().BoolVar(&opts.all, "all", false, "Install every skill the source contains")
	addHarnessFlags(cmd, opts)
	return cmd
}

// addAll installs every skill a source contains.
//
// Repositories commonly ship a suite of skills that are only useful together,
// and installing them one `add` at a time is both tedious and easy to get
// half-done. The whole batch is validated before anything is written so the
// user does not end up with a partial suite.
func addAll(cmd *cobra.Command, resolved bmo.ResolvedSource, src bmo.Source, scope bmo.Scope, cwd string, opts *options) error {
	if opts.name != "" {
		return errors.New("--name renames a single skill; it cannot be combined with --all")
	}
	skills, err := bmo.DiscoverSkills(resolved.Root)
	if err != nil {
		return err
	}
	if len(skills) == 0 {
		return errors.New("no skills found")
	}
	var invalid []string
	byName := map[string][]string{}
	for _, skill := range skills {
		if skill.Name == "" {
			invalid = append(invalid, fmt.Sprintf("%s (%s)", skill.Path, strings.Join(skill.Warnings, "; ")))
			continue
		}
		byName[skill.Name] = append(byName[skill.Name], skill.Path)
	}
	if len(invalid) > 0 {
		return fmt.Errorf("cannot install every skill: %d failed validation:\n  %s",
			len(invalid), strings.Join(invalid, "\n  "))
	}
	// Two folders resolving to one name would silently install whichever came
	// last. Point at a narrower subpath instead of guessing which was meant.
	var duplicates []string
	for name, paths := range byName {
		if len(paths) > 1 {
			sort.Strings(paths)
			duplicates = append(duplicates, fmt.Sprintf("%s: %s", name, strings.Join(paths, ", ")))
		}
	}
	if len(duplicates) > 0 {
		sort.Strings(duplicates)
		return fmt.Errorf("cannot install every skill: %d duplicate skill names in this source:\n  %s\n"+
			"Install from a narrower subpath (for example owner/repo/skills), or add them one at a time with --name",
			len(duplicates), strings.Join(duplicates, "\n  "))
	}

	target, err := targetFor(scope, cwd, opts)
	if err != nil {
		return err
	}
	skillsDir, agentsDir := target.SkillsDir, target.AgentsDir
	var incompatible []string
	for _, skill := range skills {
		if err := bmo.ValidateSkillForTarget(skill, target); err != nil {
			incompatible = append(incompatible, fmt.Sprintf("%s: %v", skill.Name, err))
		}
	}
	if len(incompatible) > 0 {
		return fmt.Errorf("cannot install every skill for %s:\n  %s", target.Harness, strings.Join(incompatible, "\n  "))
	}
	var conflicts []string
	agentOwners := map[string]string{}
	for _, skill := range skills {
		conflict, err := bmo.CheckInstallConflictsForTarget(skill, target)
		if err != nil {
			return err
		}
		if !opts.force && !conflict.Empty() {
			if conflict.Path != "" {
				conflicts = append(conflicts, fmt.Sprintf("%s is already installed at %s", skill.Name, conflict.Path))
			}
			for _, agent := range conflict.Agents {
				conflicts = append(conflicts, fmt.Sprintf("%s ships subagent %s, which bmo does not own", skill.Name, agent))
			}
		}
		// Two skills in one source claiming one subagent file is an authoring
		// bug: whichever installs last would silently win.
		if target.SupportsAgents() {
			for _, agent := range skill.Agents {
				if owner, ok := agentOwners[agent.File]; ok {
					conflicts = append(conflicts, fmt.Sprintf("subagent %s is shipped by both %s and %s", agent.File, owner, skill.Name))
					continue
				}
				agentOwners[agent.File] = skill.Name
			}
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return fmt.Errorf("cannot install every skill:\n  %s\nUse --force to replace what bmo already installed",
			strings.Join(conflicts, "\n  "))
	}

	printBatchPreview(cmd, skills, src.Raw, target, skillsDir, agentsDir)
	if !opts.yes && !opts.dryRun {
		ok, err := confirm(cmd, fmt.Sprintf("Install all %d? [y/N] ", len(skills)))
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("install cancelled")
		}
	}
	installed, agentCount := 0, 0
	for _, skill := range skills {
		meta, err := bmo.InstallSkill(bmo.InstallOptions{
			Scope:  scope,
			Target: target,
			Force:  opts.force,
			DryRun: opts.dryRun,
			CWD:    cwd,
			Source: resolved.Source,
			Skill:  skill,
		})
		if err != nil {
			return fmt.Errorf("installed %d of %d skills, then failed on %s: %w", installed, len(skills), skill.Name, err)
		}
		installed++
		agentCount += len(meta.Agents)
	}
	verb := "Installed"
	if opts.dryRun {
		verb = "Dry run: would install"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %d skills to %s\n", verb, installed, skillsDir)
	if agentCount > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %d subagents to %s\n", verb, agentCount, agentsDir)
	}
	return nil
}

type detectedTarget struct {
	target    bmo.Target
	harnesses []string
}

// addEveryone installs into every harness detected from PATH or an existing
// user config directory. Shared destinations are written once.
func addEveryone(cmd *cobra.Command, resolved bmo.ResolvedSource, scope bmo.Scope, cwd string, opts *options) error {
	if opts.all && opts.name != "" {
		return errors.New("--name renames a single skill; it cannot be combined with --all")
	}
	destinations, err := detectedTargets(scope, cwd)
	if err != nil {
		return err
	}

	var skills []bmo.Skill
	if opts.all {
		skills, err = bmo.DiscoverSkills(resolved.Root)
		if err != nil {
			return err
		}
		if len(skills) == 0 {
			return errors.New("no skills found")
		}
		byName := map[string]string{}
		for _, skill := range skills {
			if skill.Name == "" {
				return fmt.Errorf("cannot install every skill: %s failed validation: %s", skill.Path, strings.Join(skill.Warnings, "; "))
			}
			if previous, exists := byName[skill.Name]; exists {
				return fmt.Errorf("cannot install every skill: duplicate name %s in %s and %s", skill.Name, previous, skill.Path)
			}
			byName[skill.Name] = skill.Path
		}
	} else {
		skill, selectErr := selectSkill(resolved.Root, opts.name)
		if selectErr != nil {
			return selectErr
		}
		if opts.name != "" {
			skill, selectErr = bmo.ValidateSkill(skill.Path, opts.name)
			if selectErr != nil {
				return selectErr
			}
		}
		skills = []bmo.Skill{skill}
	}

	var problems []string
	for _, destination := range destinations {
		label := strings.Join(destination.harnesses, "+")
		agentOwners := map[string]string{}
		for _, skill := range skills {
			if err := bmo.ValidateSkillForTarget(skill, destination.target); err != nil {
				problems = append(problems, fmt.Sprintf("%s/%s: %v", label, skill.Name, err))
				continue
			}
			conflict, err := bmo.CheckInstallConflictsForTarget(skill, destination.target)
			if err != nil {
				return err
			}
			if !opts.force && conflict.Path != "" {
				problems = append(problems, fmt.Sprintf("%s/%s already exists at %s", label, skill.Name, conflict.Path))
			}
			if !opts.force {
				for _, agent := range conflict.Agents {
					problems = append(problems, fmt.Sprintf("%s/%s does not own subagent %s", label, skill.Name, agent))
				}
			}
			if destination.target.SupportsAgents() {
				for _, agent := range skill.Agents {
					if owner, exists := agentOwners[agent.File]; exists {
						problems = append(problems, fmt.Sprintf("%s subagent %s is shipped by both %s and %s", label, agent.File, owner, skill.Name))
					}
					agentOwners[agent.File] = skill.Name
				}
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("cannot install for everyone:\n  %s", strings.Join(problems, "\n  "))
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Found %d skill(s) for %d detected harness(es) across %d destination(s)\n\n", len(skills), detectedHarnessCount(destinations), len(destinations))
	for _, destination := range destinations {
		fmt.Fprintf(out, "  %-24s %s\n", strings.Join(destination.harnesses, ", "), destination.target.SkillsDir)
	}
	fmt.Fprintln(out)
	if !opts.yes && !opts.dryRun {
		ok, err := confirm(cmd, "Install for everyone? [y/N] ")
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("install cancelled")
		}
	}

	installed := 0
	for _, destination := range destinations {
		for _, skill := range skills {
			_, err := bmo.InstallSkill(bmo.InstallOptions{
				Scope: scope, Target: destination.target, Name: opts.name,
				Force: opts.force, DryRun: opts.dryRun, CWD: cwd,
				Source: resolved.Source, Skill: skill,
			})
			if err != nil {
				return fmt.Errorf("installed %d copies, then failed for %s: %w", installed, strings.Join(destination.harnesses, "+"), err)
			}
			installed++
		}
	}
	if opts.dryRun {
		fmt.Fprintf(out, "Dry run: would install %d skill copies.\n", installed)
	} else {
		fmt.Fprintf(out, "Installed %d skill copies for everyone.\n", installed)
	}
	return nil
}

func detectedTargets(scope bmo.Scope, cwd string) ([]detectedTarget, error) {
	infos := bmo.DetectedHarnesses()
	if len(infos) == 0 {
		return nil, errors.New("no coding harnesses detected (install one, create its config directory, or name a harness explicitly)")
	}
	var targets []detectedTarget
	byDir := map[string]int{}
	for _, info := range infos {
		target, err := bmo.ResolveTarget(string(info.Name), scope, cwd, "")
		if err != nil {
			return nil, err
		}
		key := filepath.Clean(target.SkillsDir)
		if index, exists := byDir[key]; exists {
			targets[index].harnesses = append(targets[index].harnesses, string(info.Name))
			continue
		}
		byDir[key] = len(targets)
		targets = append(targets, detectedTarget{target: target, harnesses: []string{string(info.Name)}})
	}
	return targets, nil
}

func detectedHarnessCount(targets []detectedTarget) int {
	count := 0
	for _, target := range targets {
		count += len(target.harnesses)
	}
	return count
}

func printBatchPreview(cmd *cobra.Command, skills []bmo.Skill, source string, target bmo.Target, skillsDir, agentsDir string) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Found %d skills\n\nSource: %s\nHarness: %s\nScope: %s\nDestination: %s\n", len(skills), source, target.Harness, target.Scope, skillsDir)
	totalAgents, withExecutables := 0, 0
	for _, skill := range skills {
		totalAgents += len(skill.Agents)
		if len(skill.ExecutableFiles) > 0 {
			withExecutables++
		}
	}
	if totalAgents > 0 && target.SupportsAgents() {
		fmt.Fprintf(out, "Subagent destination: %s\n", agentsDir)
	} else if totalAgents > 0 {
		fmt.Fprintln(out, "Bundled agents remain skill resources (their format is harness-specific).")
	}
	fmt.Fprintln(out)
	for _, skill := range skills {
		line := fmt.Sprintf("  %-24s %d files", skill.Name, skill.FileCount)
		if len(skill.Agents) > 0 {
			line += fmt.Sprintf(", %d subagents", len(skill.Agents))
		}
		fmt.Fprintln(out, line)
	}
	if withExecutables > 0 {
		fmt.Fprintf(out, "\n%d of these skills include executable-looking files.\n", withExecutables)
		fmt.Fprintln(out, "Skills may include executable code. Review third-party skills before use.")
	}
	fmt.Fprintln(out)
}

func newInitCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [here|everywhere]",
		Short: "Install the bundled bmo skill into a coding harness",
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
			target, err := targetFor(scope, cwd, opts)
			if err != nil {
				return err
			}
			meta, err := installBmoSkillToTarget(target, cwd, true)
			if err != nil {
				return err
			}
			markBootstrappedFor(target.Harness)
			fmt.Fprintf(cmd.OutOrStdout(), "Installed %s to %s\n\nUse it in %s:\n  %s\n", meta.Name, meta.InstalledPath, target.Harness, target.InvocationHint(meta.Name))
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.project, "project", false, "Install into the harness's project skills directory")
	addHarnessFlags(cmd, opts)
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
				if skill.IgnoreRules > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "Excludes: %d .bmoignore rules applied\n", skill.IgnoreRules)
				}
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
			printSkillList(cmd.OutOrStdout(), entries, cwd, time.Now())
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.project, "project", false, "Show only project installs")
	cmd.Flags().BoolVar(&opts.global, "global", false, "Show only global installs")
	cmd.Flags().BoolVar(&opts.json, "json", false, "Output JSON")
	addHarnessFlags(cmd, opts)
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
			target, err := targetFor(scope, cwd, opts)
			if err != nil {
				return err
			}
			meta, err := bmo.ReadMetadata(target.MetadataPath)
			if err != nil {
				return err
			}
			entry, ok := meta.Skills[args[0]]
			if !ok {
				return fmt.Errorf("skill is not tracked by bmo in %s scope: %s\nTry: bmo list, or bmo doctor", scope, args[0])
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Remove %s from %s\n", entry.Name, entry.InstalledPath)
			if len(entry.Agents) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Also removes %d subagents from %s: %s\n",
					len(entry.Agents), target.AgentsDir, strings.Join(entry.Agents, ", "))
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
			removed, err := bmo.RemoveSkillFromTarget(args[0], target)
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
	addHarnessFlags(cmd, opts)
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
			if len(args) == 0 {
				opts.all = true
			}
			// Skills tracked from the same source share one download per run.
			cache := map[string]bmo.ResolvedSource{}
			defer func() {
				for _, resolved := range cache {
					cleanupResolved(resolved)
				}
			}()
			// On update, "everywhere" reaches past the current directory:
			// global skills plus every project bmo has ever installed into.
			if keyword == "everywhere" {
				return updateEverywhere(cmd, cwd, args, opts, cache)
			}
			applyKeywordFilter(keyword, opts)
			scopes := []bmo.Scope{selectedScope(opts)}
			if opts.all && !opts.project && !opts.global {
				scopes = []bmo.Scope{bmo.ScopeGlobal, bmo.ScopeProject}
			}
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
	addHarnessFlags(cmd, opts)
	return cmd
}

func newDoctorCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local bmo and coding-harness skill setup",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "bmo doctor")
			fmt.Fprintln(cmd.OutOrStdout())
			var checks []bmo.DoctorCheck
			if opts.skillsDir != "" {
				target, err := targetFor(selectedScope(opts), cwd, opts)
				if err != nil {
					return err
				}
				checks = bmo.RunDoctorForTarget(target)
			} else {
				var err error
				checks, err = bmo.RunDoctorForHarness(cwd, opts.harness)
				if err != nil {
					return err
				}
			}
			for _, check := range checks {
				fmt.Fprintf(cmd.OutOrStdout(), "%-7s %s\n", check.Status, check.Message)
			}
			return nil
		},
	}
	addHarnessFlags(cmd, opts)
	cmd.Flags().BoolVar(&opts.project, "project", false, "Treat a custom skills directory as project-scoped")
	cmd.Flags().BoolVar(&opts.global, "global", false, "Treat a custom skills directory as global")
	return cmd
}

func newHarnessesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "harnesses",
		Short: "List built-in coding-harness presets",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "HARNESS\tPROJECT SKILLS\tGLOBAL SKILLS\tDESCRIPTION")
			for _, info := range bmo.Harnesses() {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t~/%s\t%s\n", info.Name, info.ProjectDir, info.GlobalDir, info.Description)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "\nInstall with: bmo add SOURCE HARNESS")
			fmt.Fprintln(cmd.OutOrStdout(), "Install to detected harnesses with: bmo add SOURCE everyone")
			fmt.Fprintln(cmd.OutOrStdout(), "For any other harness, pass --skills-dir PATH.")
		},
	}
}

func addHarnessFlags(cmd *cobra.Command, opts *options) {
	cmd.Flags().StringVar(&opts.harness, "harness", "", "Target harness (default claude; see `bmo harnesses`)")
	cmd.Flags().StringVar(&opts.skillsDir, "skills-dir", "", "Use an explicit skills directory for any other harness")
}

func targetFor(scope bmo.Scope, cwd string, opts *options) (bmo.Target, error) {
	return bmo.ResolveTarget(opts.harness, scope, cwd, opts.skillsDir)
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

// splitAddKeywords extracts both location and harness keywords accepted by
// `bmo add`. Harness names are positional aliases for --harness; "everyone"
// selects every harness detected on the machine.
func splitAddKeywords(args []string) (rest []string, scopeKeyword, harnessKeyword string, err error) {
	harnessNames := map[string]bool{"everyone": true}
	for _, name := range bmo.HarnessNames() {
		harnessNames[name] = true
	}
	for _, arg := range args {
		if arg == "here" || arg == "everywhere" {
			if scopeKeyword != "" {
				return nil, "", "", errors.New("specify only one location keyword (here or everywhere)")
			}
			scopeKeyword = arg
			continue
		}
		if harnessNames[arg] {
			if harnessKeyword != "" {
				return nil, "", "", errors.New("specify only one harness (or everyone)")
			}
			harnessKeyword = arg
			continue
		}
		rest = append(rest, arg)
	}
	return rest, scopeKeyword, harnessKeyword, nil
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

func printSkillPreview(cmd *cobra.Command, skill bmo.Skill, source string, target bmo.Target, dest string) {
	fmt.Fprintf(cmd.OutOrStdout(), "Found skill: %s\nDescription: %s\n\nSource: %s\nHarness: %s\nScope: %s\nDestination: %s\nFiles: %d\n", skill.Name, skill.Description, source, target.Harness, target.Scope, dest, skill.FileCount)
	if skill.IgnoreRules > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "Excludes: %d .bmoignore rules applied\n", skill.IgnoreRules)
	}
	if len(skill.Agents) > 0 && target.SupportsAgents() {
		fmt.Fprintf(cmd.OutOrStdout(), "Subagents: %s\nSubagent destination: %s\n",
			strings.Join(bmo.AgentNames(skill.Agents), ", "), target.AgentsDir)
	} else if len(skill.Agents) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Bundled agents remain skill resources (their format is harness-specific).")
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
	target, err := bmo.ResolveTarget("", scope, cwd, "")
	if err != nil {
		return bmo.SkillMeta{}, err
	}
	return installBmoSkillToTarget(target, cwd, force)
}

func installBmoSkillToTarget(target bmo.Target, cwd string, force bool) (bmo.SkillMeta, error) {
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
		Scope:  target.Scope,
		Target: target,
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
	case "init", "harnesses", "help", "__complete", "__completeNoDesc":
		return false
	case "add":
		rest, _, _, err := splitAddKeywords(args)
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
	bootstrapBmoSkillForOptions(cmd, nil, &options{})
}

func bootstrapBmoSkillForOptions(cmd *cobra.Command, args []string, opts *options) {
	if opts.skillsDir != "" {
		return
	}
	harnessName := opts.harness
	if cmd.Name() == "add" {
		_, _, positionalHarness, err := splitAddKeywords(args)
		if err == nil && positionalHarness != "" {
			if opts.harness != "" {
				return
			}
			if positionalHarness == "everyone" {
				return
			}
			harnessName = positionalHarness
		}
	}
	harness, err := bmo.ParseHarness(harnessName)
	if err != nil {
		return
	}
	marker, err := bmo.BootstrapMarkerPathFor(harness)
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
	target, err := bmo.ResolveTarget(string(harness), bmo.ScopeGlobal, cwd, "")
	if err != nil {
		return
	}
	if !bmoSkillTrackedInTarget(target) {
		if meta, err := installBmoSkillToTarget(target, cwd, false); err == nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "bmo: installed the bmo skill to %s (run `bmo remove bmo` to undo)\n", meta.InstalledPath)
		}
	}
	markBootstrappedFor(harness)
}

// bmoSkillTracked reports whether the bmo skill is already recorded in global
// metadata.
func bmoSkillTracked(cwd string) bool {
	target, err := bmo.ResolveTarget("", bmo.ScopeGlobal, cwd, "")
	if err != nil {
		return false
	}
	return bmoSkillTrackedInTarget(target)
}

func bmoSkillTrackedInTarget(target bmo.Target) bool {
	meta, err := bmo.ReadMetadata(target.MetadataPath)
	if err != nil {
		return false
	}
	_, ok := meta.Skills[bmo.EmbeddedSkillName]
	return ok
}

// markBootstrapped writes the sentinel file recording the one-time install.
func markBootstrapped() {
	markBootstrappedFor(bmo.HarnessClaude)
}

func markBootstrappedFor(harness bmo.Harness) {
	marker, err := bmo.BootstrapMarkerPathFor(harness)
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
	if opts.skillsDir != "" {
		target, err := targetFor(selectedScope(opts), cwd, opts)
		if err != nil {
			return nil, err
		}
		meta, err := bmo.ReadMetadata(target.MetadataPath)
		if err != nil {
			return nil, err
		}
		for _, entry := range meta.Skills {
			entries = append(entries, entry)
		}
		return entries, nil
	}
	if !opts.project {
		target, err := targetFor(bmo.ScopeGlobal, cwd, opts)
		if err != nil {
			return nil, err
		}
		meta, err := bmo.ReadMetadata(target.MetadataPath)
		if err != nil {
			return nil, err
		}
		for _, entry := range meta.Skills {
			entries = append(entries, entry)
		}
	}
	if !opts.global {
		target, err := targetFor(bmo.ScopeProject, cwd, opts)
		if err != nil {
			return nil, err
		}
		meta, err := bmo.ReadMetadata(target.MetadataPath)
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

// updateEverywhere updates the global scope plus every project recorded in
// the registry, so `bmo update everywhere` reaches repos without being run
// inside them. With a skill name, only the places tracking that skill run.
func updateEverywhere(cmd *cobra.Command, cwd string, args []string, opts *options, cache map[string]bmo.ResolvedSource) error {
	out := cmd.OutOrStdout()
	if opts.skillsDir != "" {
		return errors.New("update everywhere cannot discover arbitrary --skills-dir locations; choose --project or --global")
	}
	currentProject, err := targetFor(bmo.ScopeProject, cwd, opts)
	if err != nil {
		return err
	}
	// Backfill: repos installed into before the registry existed register the
	// first time an update runs inside them.
	if hasTrackedSkills(currentProject.MetadataPath) {
		_ = bmo.RecordProject(cwd)
	}
	named := ""
	if len(args) == 1 {
		named = args[0]
	}
	global, err := targetFor(bmo.ScopeGlobal, cwd, opts)
	if err != nil {
		return err
	}
	globalPath := global.MetadataPath
	found := false
	if (named == "" && hasTrackedSkills(globalPath)) || (named != "" && metadataHasSkill(globalPath, named)) {
		fmt.Fprintln(out, "Global:")
		if err := updateScope(cmd, cwd, bmo.ScopeGlobal, args, opts, cache); err != nil {
			return err
		}
		found = true
	}
	projects, err := bmo.RegisteredProjects()
	if err != nil {
		return err
	}
	for _, dir := range projects {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			fmt.Fprintf(out, "\nSkipping %s (directory no longer exists)\n", dir)
			continue
		}
		project, err := targetFor(bmo.ScopeProject, dir, opts)
		if err != nil {
			return err
		}
		metaPath := project.MetadataPath
		if (named == "" && !hasTrackedSkills(metaPath)) || (named != "" && !metadataHasSkill(metaPath, named)) {
			continue
		}
		if found {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s:\n", dir)
		if err := updateScope(cmd, dir, bmo.ScopeProject, args, opts, cache); err != nil {
			return err
		}
		found = true
	}
	if !found {
		if named != "" {
			return fmt.Errorf("skill is not tracked by bmo anywhere: %s", named)
		}
		fmt.Fprintln(out, "No tracked skills anywhere yet.")
	}
	return nil
}

// hasTrackedSkills reports whether the metadata file at path tracks anything.
func hasTrackedSkills(path string) bool {
	meta, err := bmo.ReadMetadata(path)
	return err == nil && len(meta.Skills) > 0
}

// metadataHasSkill reports whether the metadata file at path tracks name.
func metadataHasSkill(path, name string) bool {
	meta, err := bmo.ReadMetadata(path)
	if err != nil {
		return false
	}
	_, ok := meta.Skills[name]
	return ok
}

func updateScope(cmd *cobra.Command, cwd string, scope bmo.Scope, args []string, opts *options, cache map[string]bmo.ResolvedSource) error {
	target, err := targetFor(scope, cwd, opts)
	if err != nil {
		return err
	}
	meta, err := bmo.ReadMetadata(target.MetadataPath)
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
		_, err = bmo.InstallSkill(bmo.InstallOptions{Scope: scope, Target: target, Name: name, Force: true, DryRun: opts.dryRun, CWD: cwd, Source: resolved.Source, Skill: skill})
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

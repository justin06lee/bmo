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
	"text/tabwriter"
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
	root.AddCommand(newScoutCommand(opts))
	root.AddCommand(newShareCommand(opts))
	root.AddCommand(newUpdateCommand(opts))
	root.AddCommand(newDoctorCommand(opts))
	root.AddCommand(newHarnessesCommand())
	root.AddCommand(newUpgradeCommand())
	return root
}

func newAddCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [here|everywhere] SOURCE [HARNESS|everyone]",
		Short: "Install a coding-agent skill",
		Example: `  bmo add everywhere owner/repo everyone
  bmo add here owner/repo codex
  bmo add --global gemini owner/repo --all --yes`,
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
			effective, err := withPositionalHarness(opts, positionalHarness)
			if err != nil {
				return err
			}
			if err := keywordScopeConflict(keyword, effective); err != nil {
				return err
			}
			scope := keywordScope(keyword, effective)
			src, err := bmo.ParseSource(args[0])
			if err != nil {
				return err
			}
			resolved, err := bmo.ResolveSource(src)
			if err != nil {
				return err
			}
			defer cleanupResolved(resolved)
			if positionalHarness == everyoneKeyword {
				return addEveryone(cmd, resolved, scope, cwd, effective)
			}
			target, err := targetFor(scope, cwd, effective)
			if err != nil {
				return err
			}
			if effective.all {
				return addAll(cmd, resolved, src, scope, cwd, effective)
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
			fmt.Fprintf(cmd.OutOrStdout(), "\nUse it in %s:\n  %s\n", target.DisplayHarness(), target.InvocationHint(meta.Name))
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.project, "project", false, "Install into the harness's project skills directory")
	cmd.Flags().BoolVar(&opts.global, "global", false, "Install into the harness's global skills directory")
	cmd.Flags().StringVar(&opts.name, "name", "", "Override destination skill folder name")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Replace an existing installed skill")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Skip interactive confirmation")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show what would happen without copying files")
	cmd.Flags().BoolVar(&opts.all, "all", false, "Install every skill the source contains")
	addHarnessFlags(cmd, opts)
	cmd.MarkFlagsMutuallyExclusive("project", "global")
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
	if err := checkBatchNames(skills); err != nil {
		return err
	}

	target, err := targetFor(scope, cwd, opts)
	if err != nil {
		return err
	}
	skillsDir, agentsDir := target.SkillsDir, target.AgentsDir
	problems, forceFixable, err := batchProblems(skills, target, opts.force, "")
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		msg := fmt.Sprintf("cannot install every skill:\n  %s", strings.Join(problems, "\n  "))
		if forceFixable {
			msg += "\nUse --force to replace what bmo already installed"
		}
		return errors.New(msg)
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
	var done []batchInstall
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
			// Without --force the preflight proved every destination was
			// empty, so undoing the partial batch is safe and keeps a plain
			// retry from tripping over its own leftovers.
			if !opts.force && !opts.dryRun {
				rollbackBatch(cmd, done)
				return fmt.Errorf("failed on %s after installing %d of %d skills; rolled the batch back: %w", skill.Name, installed, len(skills), err)
			}
			return fmt.Errorf("installed %d of %d skills, then failed on %s: %w", installed, len(skills), skill.Name, err)
		}
		if !opts.dryRun {
			done = append(done, batchInstall{meta.Name, target})
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

// checkBatchNames rejects a batch containing invalid skills or two folders
// resolving to one name. Two folders resolving to one name would silently
// install whichever came last; point at a narrower subpath instead.
func checkBatchNames(skills []bmo.Skill) error {
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
	return nil
}

// batchProblems lists everything an install of skills into one target would
// violate or overwrite, in one shared implementation so `--all` and `everyone`
// cannot drift apart. label prefixes multi-destination output ("codex+amp");
// forceFixable reports whether --force would clear at least one problem, so
// the caller only suggests it when it would actually help.
func batchProblems(skills []bmo.Skill, target bmo.Target, force bool, label string) (problems []string, forceFixable bool, err error) {
	prefix := ""
	if label != "" {
		prefix = label + ": "
	}
	agentOwners := map[string]string{}
	for _, skill := range skills {
		if err := bmo.ValidateSkillForTarget(skill, target); err != nil {
			problems = append(problems, fmt.Sprintf("%s%s: %v", prefix, skill.Name, err))
			continue
		}
		conflict, err := bmo.CheckInstallConflictsForTarget(skill, target)
		if err != nil {
			return nil, false, err
		}
		if !force && !conflict.Empty() {
			if conflict.Path != "" {
				problems = append(problems, fmt.Sprintf("%s%s is already installed at %s", prefix, skill.Name, conflict.Path))
				forceFixable = true
			}
			for _, agent := range conflict.Agents {
				problems = append(problems, fmt.Sprintf("%s%s ships subagent %s, which bmo does not own", prefix, skill.Name, agent))
				forceFixable = true
			}
		}
		// Two skills in one source claiming one subagent file is an authoring
		// bug: whichever installs last would silently win.
		if target.SupportsAgents() {
			for _, agent := range skill.Agents {
				if owner, ok := agentOwners[agent.File]; ok {
					problems = append(problems, fmt.Sprintf("%ssubagent %s is shipped by both %s and %s", prefix, agent.File, owner, skill.Name))
					continue
				}
				agentOwners[agent.File] = skill.Name
			}
		}
	}
	return problems, forceFixable, nil
}

// batchInstall records one successful install so a failed batch can undo it.
type batchInstall struct {
	name   string
	target bmo.Target
}

// rollbackBatch best-effort removes the copies a failed batch already wrote.
// Callers invoke it only for no-force batches, where the preflight proved
// every destination was previously empty, so removal cannot destroy state
// that predates the batch.
func rollbackBatch(cmd *cobra.Command, installed []batchInstall) {
	for i := len(installed) - 1; i >= 0; i-- {
		entry := installed[i]
		if _, err := bmo.RemoveSkillFromTarget(entry.name, entry.target); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "bmo: warning: could not roll back %s from %s: %v\n", entry.name, entry.target.SkillsDir, err)
		}
	}
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
		if err := checkBatchNames(skills); err != nil {
			return err
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
	anyForceFixable := false
	for _, destination := range destinations {
		destProblems, forceFixable, err := batchProblems(skills, destination.target, opts.force, strings.Join(destination.harnesses, "+"))
		if err != nil {
			return err
		}
		problems = append(problems, destProblems...)
		anyForceFixable = anyForceFixable || forceFixable
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		msg := fmt.Sprintf("cannot install for everyone:\n  %s", strings.Join(problems, "\n  "))
		if anyForceFixable {
			msg += "\nUse --force to replace what bmo already installed"
		}
		return errors.New(msg)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Found %d skill(s) for %d detected harness(es) across %d destination(s)\n\n", len(skills), detectedHarnessCount(destinations), len(destinations))
	for _, destination := range destinations {
		fmt.Fprintf(out, "  %-24s %s\n", strings.Join(destination.harnesses, ", "), destination.target.SkillsDir)
	}
	fmt.Fprintln(out)
	// Fanning one source out to every harness at once is the widest install bmo
	// performs, so it must not be the one that hides what the other previews warn
	// about.
	printExecutableWarning(out, skills)
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
	var done []batchInstall
	for _, destination := range destinations {
		for _, skill := range skills {
			meta, err := bmo.InstallSkill(bmo.InstallOptions{
				Scope: scope, Target: destination.target, Name: opts.name,
				Force: opts.force, DryRun: opts.dryRun, CWD: cwd,
				Source: resolved.Source, Skill: skill,
			})
			if err != nil {
				label := strings.Join(destination.harnesses, "+")
				// Leaving the completed destinations behind would turn the
				// retry into an "already exists" preflight failure, so a
				// no-force fan-out undoes itself on the way out.
				if !opts.force && !opts.dryRun {
					rollbackBatch(cmd, done)
					return fmt.Errorf("failed for %s after installing %d copies; rolled them back so a plain retry works: %w", label, installed, err)
				}
				return fmt.Errorf("installed %d copies, then failed for %s: %w", installed, label, err)
			}
			if !opts.dryRun {
				done = append(done, batchInstall{meta.Name, destination.target})
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
	fmt.Fprintf(out, "Found %d skills\n\nSource: %s\nHarness: %s\nScope: %s\nDestination: %s\n", len(skills), source, target.DisplayHarness(), target.Scope, skillsDir)
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

// printExecutableWarning names the skills carrying executable-looking files,
// matching the warning the single-skill preview prints.
func printExecutableWarning(out io.Writer, skills []bmo.Skill) {
	var withExecutables []string
	for _, skill := range skills {
		if len(skill.ExecutableFiles) > 0 {
			withExecutables = append(withExecutables, skill.Name)
		}
	}
	if len(withExecutables) == 0 {
		return
	}
	fmt.Fprintln(out, "These skills include executable-looking files:")
	for _, name := range withExecutables {
		fmt.Fprintf(out, "- %s\n", name)
	}
	fmt.Fprintln(out, "\nSkills may include executable code. Review third-party skills before use.")
	fmt.Fprintln(out)
}

func newInitCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [here|everywhere] [HARNESS]",
		Short: "Install the bundled bmo skill into a coding harness",
		Example: `  bmo init here
  bmo init codex`,
		Args: argsWithKeywords(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			_, keyword, positionalHarness, err := splitHarnessKeywords(args, minPositionalArgs(cmd))
			if err != nil {
				return err
			}
			effective, err := withPositionalHarness(opts, positionalHarness)
			if err != nil {
				return err
			}
			if err := keywordScopeConflict(keyword, effective); err != nil {
				return err
			}
			scope := keywordScope(keyword, effective)
			target, err := targetFor(scope, cwd, effective)
			if err != nil {
				return err
			}
			meta, err := installBmoSkillToTarget(target, cwd, true)
			if err != nil {
				return err
			}
			markBootstrappedFor(target.Harness)
			fmt.Fprintf(cmd.OutOrStdout(), "Installed %s to %s\n\nUse it in %s:\n  %s\n", meta.Name, meta.InstalledPath, target.DisplayHarness(), target.InvocationHint(meta.Name))
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.project, "project", false, "Install into the harness's project skills directory")
	cmd.Flags().BoolVar(&opts.global, "global", false, "Install into the harness's global skills directory")
	addHarnessFlags(cmd, opts)
	cmd.MarkFlagsMutuallyExclusive("project", "global")
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
		Use:   "list [here|everywhere] [HARNESS]",
		Short: "List installed skills tracked by bmo",
		Example: `  bmo list here
  bmo list codex`,
		Args: argsWithKeywords(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			_, keyword, positionalHarness, err := splitHarnessKeywords(args, minPositionalArgs(cmd))
			if err != nil {
				return err
			}
			effective, err := withPositionalHarness(opts, positionalHarness)
			if err != nil {
				return err
			}
			if err := keywordScopeConflict(keyword, effective); err != nil {
				return err
			}
			applyKeywordFilter(keyword, effective)
			entries, err := listEntries(cwd, effective)
			if err != nil {
				return err
			}
			if effective.json {
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
	cmd.MarkFlagsMutuallyExclusive("project", "global")
	return cmd
}

func newUpdateCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update [SKILL_NAME] [here|everywhere|universe] [HARNESS|everyone]",
		Short: "Update installed skills whose source content changed",
		Example: `  bmo update demo here
  bmo update codex
  bmo update everywhere everyone`,
		Args: argsWithKeywords(func(cmd *cobra.Command, args []string) error {
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
			args, keyword, positionalHarness, err := splitCommandKeywords(cmd, args)
			if err != nil {
				return err
			}
			// "everyone" is not a harness to resolve; it clears the selection
			// so the sweep visits every preset's destinations.
			everyone := positionalHarness == everyoneKeyword
			if everyone {
				if opts.harness != "" || opts.skillsDir != "" {
					return errors.New(`"everyone" already covers every harness; drop --harness and --skills-dir`)
				}
				positionalHarness = ""
			}
			effective, err := withPositionalHarness(opts, positionalHarness)
			if err != nil {
				return err
			}
			if err := keywordScopeConflict(keyword, effective); err != nil {
				return err
			}
			if len(args) == 0 {
				effective.all = true
			}
			// Skills tracked from the same source share one download per run.
			cache := map[string]sourceResolution{}
			defer func() {
				for _, res := range cache {
					cleanupResolved(res.resolved)
				}
			}()
			// On update, "everywhere" reaches past the current directory:
			// global skills plus every project bmo has ever installed into.
			// "universe" is the same sweep, spelled the way the rest of the
			// CLI names that reach.
			if keyword == "everywhere" || keyword == universeKeyword {
				return updateEverywhere(cmd, cwd, args, effective, cache)
			}
			if everyone {
				return updateEveryone(cmd, cwd, keyword, args, effective, cache)
			}
			applyKeywordFilter(keyword, effective)
			scopes := []bmo.Scope{selectedScope(effective)}
			if effective.all && !effective.project && !effective.global {
				scopes = []bmo.Scope{bmo.ScopeGlobal, bmo.ScopeProject}
			}
			// One scope's failures must not strand the other scope's skills:
			// finish the sweep, then report everything that went wrong.
			var failures []string
			for _, scope := range scopes {
				if err := updateScope(cmd, cwd, scope, args, effective, cache); err != nil {
					failures = append(failures, err.Error())
				}
			}
			if len(failures) > 0 {
				return errors.New(strings.Join(failures, "\n"))
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
	cmd.MarkFlagsMutuallyExclusive("project", "global")
	return cmd
}

func newDoctorCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor [here|everywhere] [HARNESS]",
		Short: "Check local bmo and coding-harness skill setup",
		Example: `  bmo doctor
  bmo doctor codex`,
		// Without a validator cobra accepts anything, and doctor would answer
		// for the default harness while silently dropping the argument.
		Args: argsWithKeywords(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			_, keyword, positionalHarness, err := splitHarnessKeywords(args, minPositionalArgs(cmd))
			if err != nil {
				return err
			}
			effective, err := withPositionalHarness(opts, positionalHarness)
			if err != nil {
				return err
			}
			if err := keywordScopeConflict(keyword, effective); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "bmo doctor")
			fmt.Fprintln(cmd.OutOrStdout())
			var checks []bmo.DoctorCheck
			scopeSelected := keyword != "" || effective.project || effective.global
			if effective.skillsDir != "" {
				target, err := targetFor(keywordScope(keyword, effective), cwd, effective)
				if err != nil {
					return err
				}
				checks = bmo.RunDoctorForTarget(target)
			} else if scopeSelected {
				// An explicit scope selection narrows the report; without one
				// a built-in harness is diagnosed for both scopes.
				checks, err = bmo.RunDoctorForHarnessScope(cwd, effective.harness, keywordScope(keyword, effective))
				if err != nil {
					return err
				}
			} else {
				checks, err = bmo.RunDoctorForHarness(cwd, effective.harness)
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
	cmd.MarkFlagsMutuallyExclusive("project", "global")
	return cmd
}

func newHarnessesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "harnesses",
		Short: "List built-in coding-harness presets",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "HARNESS\tPROJECT SKILLS\tGLOBAL SKILLS\tDESCRIPTION")
			for _, info := range bmo.Harnesses() {
				fmt.Fprintf(tw, "%s\t%s\t~/%s\t%s\n", info.Name, info.ProjectDir, info.GlobalDir, info.Description)
			}
			tw.Flush()
			fmt.Fprintln(cmd.OutOrStdout(), "\nInstall with: bmo add SOURCE HARNESS")
			fmt.Fprintln(cmd.OutOrStdout(), "Install to detected harnesses with: bmo add SOURCE everyone")
			fmt.Fprintln(cmd.OutOrStdout(), "Give them all the same skills with: bmo share everyone")
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

// everyoneKeyword fans a command out across harnesses: add installs into every
// detected one, remove and update sweep every one's destinations, and share
// syncs between them. The commands that resolve a single destination reject it.
const everyoneKeyword = "everyone"

// universeKeyword is the widest reach bmo has: the global destinations plus
// every project in the registry, so `bmo remove universe NAME` deletes every
// copy on the machine without visiting a single repo. Only the commands that
// act on installs bmo already tracked can use it; the ones that resolve a
// single destination to write to reject it.
//
// `update` and `share` predate the word and spell the same sweep
// "everywhere", which is why sweepEverything exists.
const universeKeyword = "universe"

// sweepEverything canonicalizes the keyword update and share use for the
// machine-wide sweep. Their "everywhere" has always reached past the global
// scope, and changing that under people who type it daily would be worse than
// carrying the synonym. `remove` keeps "everywhere" meaning the global
// destinations it means everywhere else in the CLI.
func sweepEverything(keyword string) string {
	if keyword == "everywhere" {
		return universeKeyword
	}
	return keyword
}

// sweepsTheUniverse reports whether "universe" is meaningful for a command.
// remove, update, and share act on installs that already exist and can visit
// every one of them; every other command resolves a single destination to
// write to or read from, where "the whole machine" names nothing.
func sweepsTheUniverse(name string) bool {
	switch name {
	case "remove", "update", "share":
		return true
	}
	return false
}

// universeUnsupported explains the refusal, and points at the keyword that
// does name a destination the command can use.
func universeUnsupported() error {
	return errors.New(`"universe" reaches every project bmo has installed into and is only supported by bmo remove, bmo update, and bmo share; use "everywhere" for the global destination or "here" for this project`)
}

// splitKeywords pulls the optional location keyword ("here" / "everywhere") and
// an optional harness token out of a command's positional args, returning what
// is left for the command itself. "here" means the current project,
// "everywhere" means the global install; a harness name is a positional alias
// for --harness. Keywords are matched case-insensitively, like --harness;
// skill names and sources that could collide with them are always lowercase.
// Naming two of either is an error.
//
// minArgs is how many positional args the command still needs. A skill (or a
// local source folder) may legitimately be named after a harness — or even
// "everyone" — so when consuming such a token would starve the command of a
// required argument (`bmo remove codex`, `bmo add codex`) the leftmost such
// tokens stay positional instead.
func splitKeywords(args []string, minArgs int) (rest []string, scopeKeyword, harnessKeyword string, err error) {
	harnessNames := map[string]bool{everyoneKeyword: true}
	for _, name := range bmo.HarnessSelectors() {
		harnessNames[name] = true
	}
	// Counted up front so the outcome does not depend on argument order.
	demotable := minArgs
	for _, arg := range args {
		if !locationKeyword(arg) && !harnessNames[strings.ToLower(arg)] {
			demotable--
		}
	}
	for _, arg := range args {
		lower := strings.ToLower(arg)
		switch {
		case locationKeyword(arg):
			if scopeKeyword != "" {
				return nil, "", "", errors.New("specify only one location keyword (here, everywhere, or universe)")
			}
			scopeKeyword = lower
		case harnessNames[lower] && demotable <= 0:
			if harnessKeyword != "" {
				return nil, "", "", errors.New("specify only one harness (or everyone)")
			}
			harnessKeyword = lower
		default:
			if harnessNames[lower] {
				demotable--
			}
			rest = append(rest, arg)
		}
	}
	return rest, scopeKeyword, harnessKeyword, nil
}

// locationKeyword reports whether an argument names one of the three reaches a
// command can be pointed at, matched case-insensitively like --harness.
func locationKeyword(arg string) bool {
	switch strings.ToLower(arg) {
	case "here", "everywhere", universeKeyword:
		return true
	}
	return false
}

// splitAddKeywords extracts the keywords accepted by `bmo add`. The source is
// a required positional and may itself be a local folder named after a
// harness (`bmo add codex` installs ./codex), so harness-shaped tokens are
// demoted to it exactly like remove's skill name.
func splitAddKeywords(args []string) (rest []string, scopeKeyword, harnessKeyword string, err error) {
	rest, scopeKeyword, harnessKeyword, err = splitKeywords(args, 1)
	if err != nil {
		return nil, "", "", err
	}
	if scopeKeyword == universeKeyword {
		return nil, "", "", universeUnsupported()
	}
	return rest, scopeKeyword, harnessKeyword, nil
}

// splitHarnessKeywords is splitKeywords for the harness-aware commands that
// resolve exactly one destination, so "everyone" cannot mean anything for them
// and is reported instead of being taken for a harness or a skill name.
func splitHarnessKeywords(args []string, minArgs int) (rest []string, scopeKeyword, harnessKeyword string, err error) {
	rest, scopeKeyword, harnessKeyword, err = splitKeywords(args, minArgs)
	if err != nil {
		return nil, "", "", err
	}
	if harnessKeyword == everyoneKeyword {
		return nil, "", "", errors.New(`"everyone" fans out across harnesses and is only supported by bmo add, bmo remove, bmo update, and bmo share; run this command per harness instead (a skill literally named everyone is covered by the command's no-name form, e.g. a plain bmo list)`)
	}
	return rest, scopeKeyword, harnessKeyword, nil
}

// fansOutAcrossHarnesses reports whether "everyone" is meaningful for a
// command: add installs into every detected harness, while remove, update, and
// share sweep every harness's destinations at once.
func fansOutAcrossHarnesses(name string) bool {
	switch name {
	case "add", "remove", "update", "share":
		return true
	}
	return false
}

// splitCommandKeywords parses a command's positional keywords the way that
// command accepts them, so its argument validator and its RunE can never
// disagree about which tokens are keywords.
func splitCommandKeywords(cmd *cobra.Command, args []string) (rest []string, scopeKeyword, harnessKeyword string, err error) {
	if fansOutAcrossHarnesses(cmd.Name()) {
		rest, scopeKeyword, harnessKeyword, err = splitKeywords(args, minPositionalArgs(cmd))
	} else {
		rest, scopeKeyword, harnessKeyword, err = splitHarnessKeywords(args, minPositionalArgs(cmd))
	}
	if err != nil {
		return nil, "", "", err
	}
	if scopeKeyword == universeKeyword && !sweepsTheUniverse(cmd.Name()) {
		return nil, "", "", universeUnsupported()
	}
	return rest, scopeKeyword, harnessKeyword, nil
}

// minPositionalArgs reports how many positional args a harness-aware command
// still needs once its keywords are stripped. add's source and remove's skill
// name are required, and either may itself look like a harness name.
func minPositionalArgs(cmd *cobra.Command) int {
	switch cmd.Name() {
	case "add", "remove":
		return 1
	}
	return 0
}

// withPositionalHarness copies opts and folds in a positional harness token.
// The options struct is shared by every command's flags, so a command must work
// from its own copy rather than mutate it.
func withPositionalHarness(opts *options, harness string) (*options, error) {
	effective := *opts
	if harness == "" {
		return &effective, nil
	}
	if opts.harness != "" || opts.skillsDir != "" {
		return nil, errors.New("a positional harness cannot be combined with --harness or --skills-dir")
	}
	effective.harness = harness
	return &effective, nil
}

// keywordScopeConflict rejects contradictory scope directives — a location
// keyword pointing one way and a --project/--global flag the other — instead
// of letting one silently win (or, on list, silently filtering both scopes
// out and reporting an empty listing).
func keywordScopeConflict(keyword string, opts *options) error {
	if keyword == "here" && opts.global {
		return errors.New(`"here" cannot be combined with --global`)
	}
	if keyword == "everywhere" && opts.project {
		return errors.New(`"everywhere" cannot be combined with --project`)
	}
	if keyword == universeKeyword && (opts.project || opts.global) {
		return errors.New(`"universe" covers the global destinations and every registered project; drop --project and --global`)
	}
	return nil
}

// keywordScope resolves the scope for commands that act on a single scope
// (add, init, remove, and doctor's custom-directory form). A keyword wins;
// otherwise the --project/--global flags decide, and the default is global
// ("everywhere"). Contradictions are rejected by keywordScopeConflict first.
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

// argsWithKeywords wraps a cobra positional-args validator so it counts args
// after the optional location and harness keywords have been stripped out.
// Wrapping the real validator keeps stray arguments an error instead of
// something the command silently ignores.
func argsWithKeywords(base cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		rest, _, _, err := splitCommandKeywords(cmd, args)
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
	fmt.Fprintf(cmd.OutOrStdout(), "Found skill: %s\nDescription: %s\n\nSource: %s\nHarness: %s\nScope: %s\nDestination: %s\nFiles: %d\n", skill.Name, skill.Description, source, target.DisplayHarness(), target.Scope, dest, skill.FileCount)
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

// bootstrapBmoSkillForOptions installs the bundled bmo skill once, the first
// time bmo is run. A sentinel file records that it happened so a later
// `bmo remove bmo` sticks. All failures are non-fatal — bmo should still run
// without it.
func bootstrapBmoSkillForOptions(cmd *cobra.Command, args []string, opts *options) {
	if opts.skillsDir != "" || opts.dryRun {
		return
	}
	harnessName := opts.harness
	// A positional harness names where this run is aimed, so the first-run
	// install follows it instead of seeding a harness the user never mentioned.
	if positionalHarness := positionalHarnessFor(cmd, args); positionalHarness != "" {
		if opts.harness != "" {
			return
		}
		if positionalHarness == everyoneKeyword {
			return
		}
		harnessName = positionalHarness
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

// positionalHarnessFor reports the harness token a harness-aware command was
// given, parsed the same way that command parses it. Commands that take no
// harness token (and any parse error, which the command itself will report)
// yield an empty name.
func positionalHarnessFor(cmd *cobra.Command, args []string) string {
	switch cmd.Name() {
	case "add", "init", "list", "remove", "update", "doctor", "share":
		_, _, harness, err := splitKeywords(args, minPositionalArgs(cmd))
		if err != nil {
			return ""
		}
		return harness
	}
	return ""
}

// bmoSkillTrackedInTarget reports whether the bmo skill is already recorded
// in the target's metadata.
func bmoSkillTrackedInTarget(target bmo.Target) bool {
	meta, err := bmo.ReadMetadata(target.MetadataPath)
	if err != nil {
		return false
	}
	_, ok := meta.Skills[bmo.EmbeddedSkillName]
	return ok
}

// markBootstrappedFor writes the sentinel file recording the one-time install.
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

// sweepLocation is one place a fan-out command acts on: a directory paired
// with the scope whose destinations it resolves there.
type sweepLocation struct {
	dir   string
	scope bmo.Scope
	label string
}

// sweepLocations resolves where a multi-place command works, shared by remove,
// update, and share so the three cannot drift apart on what a location keyword
// means.
//
// "universe" reaches past the current directory: the global destinations plus
// every project bmo has installed into. "everywhere" is the global
// destinations, "here" is this project alone, and with no keyword a sweep
// covers the two destinations the current directory resolves to. Registered
// projects that have since been deleted are returned separately so the caller
// can report them rather than fail.
//
// Callers pass the canonical keyword: update and share, whose "everywhere" has
// always meant the machine-wide sweep, translate it with sweepEverything
// first.
func sweepLocations(cwd, keyword string, opts *options) (locations []sweepLocation, missing []string, err error) {
	global := sweepLocation{dir: cwd, scope: bmo.ScopeGlobal, label: "Global"}
	project := sweepLocation{dir: cwd, scope: bmo.ScopeProject, label: cwd}
	switch {
	case keyword == universeKeyword:
		locations = []sweepLocation{global}
		projects, err := bmo.RegisteredProjects()
		if err != nil {
			return nil, nil, err
		}
		for _, dir := range projects {
			if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
				missing = append(missing, dir)
				continue
			}
			locations = append(locations, sweepLocation{dir: dir, scope: bmo.ScopeProject, label: dir})
		}
		return locations, missing, nil
	case keyword == "here" || opts.project:
		return []sweepLocation{project}, nil, nil
	case keyword == "everywhere" || opts.global:
		// --global stays the single global destination it has always been;
		// only "universe" reaches into registered projects.
		return []sweepLocation{global}, nil, nil
	default:
		return []sweepLocation{global, project}, nil, nil
	}
}

// updateSweepRequest is one multi-destination update.
type updateSweepRequest struct {
	locations []sweepLocation
	// missing are registered projects that no longer exist on disk.
	missing   []string
	harnesses []string
	args      []string
	opts      *options
	cache     map[string]sourceResolution
	// where names the sweep's reach in its "nothing tracked" messages.
	where string
	// hint is printed after the empty message, when the sweep found nothing.
	hint string
}

// updateSweep updates every harness destination at every location, skipping
// the ones that track nothing so a nine-harness sweep does not report eight
// failures for a machine that only uses one. Presets may share one metadata
// file (codex and amp share the project .agents destination); each file is
// visited once, credited to the first name. One destination's failures do not
// stop the sweep; they are aggregated and reported at the end.
func updateSweep(cmd *cobra.Command, req updateSweepRequest) error {
	out := cmd.OutOrStdout()
	named := ""
	if len(req.args) == 1 {
		named = req.args[0]
	}
	for _, dir := range req.missing {
		fmt.Fprintf(out, "Skipping %s (directory no longer exists)\n", dir)
	}
	if len(req.missing) > 0 {
		fmt.Fprintln(out)
	}
	found := false
	var failures []string
	seenMetadata := map[string]bool{}
	for _, location := range req.locations {
		for _, hname := range req.harnesses {
			target, err := bmo.ResolveTarget(hname, location.scope, location.dir, "")
			if err != nil {
				return err
			}
			if seenMetadata[target.MetadataPath] {
				continue
			}
			seenMetadata[target.MetadataPath] = true
			if (named == "" && !hasTrackedSkills(target.MetadataPath)) || (named != "" && !metadataHasSkill(target.MetadataPath, named)) {
				continue
			}
			if found {
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "%s (%s):\n", location.label, hname)
			harnessOpts := *req.opts
			harnessOpts.harness = hname
			if err := updateScope(cmd, location.dir, location.scope, req.args, &harnessOpts, req.cache); err != nil {
				failures = append(failures, err.Error())
			}
			found = true
		}
	}
	if !found {
		if named != "" {
			return fmt.Errorf("skill is not tracked by bmo %s: %s", req.where, named)
		}
		fmt.Fprintf(out, "No tracked skills %s yet.\n", req.where)
		if req.hint != "" {
			fmt.Fprintln(out, req.hint)
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}

// updateEverywhere updates the global scope plus every project recorded in
// the registry, so `bmo update everywhere` (or `universe`) reaches repos
// without being run inside them. With a skill name, only the places tracking that skill run.
// Unless a harness is named, every built-in preset is swept: project installs
// bmo registered for codex, gemini, and friends must not go silently stale
// just because the default harness is Claude.
func updateEverywhere(cmd *cobra.Command, cwd string, args []string, opts *options, cache map[string]sourceResolution) error {
	if opts.skillsDir != "" {
		return errors.New("a machine-wide update cannot discover arbitrary --skills-dir locations; choose --project or --global")
	}
	harnessNames := everywhereHarnesses(opts)
	if err := recordCurrentProject(cwd, harnessNames); err != nil {
		return err
	}
	locations, missing, err := sweepLocations(cwd, universeKeyword, opts)
	if err != nil {
		return err
	}
	return updateSweep(cmd, updateSweepRequest{
		locations: locations, missing: missing, harnesses: harnessNames,
		args: args, opts: opts, cache: cache, where: "anywhere",
		hint: "Run `bmo scout` from a directory above your repos to record project installs bmo has not seen yet.",
	})
}

// updateEveryone updates every harness's destinations at the locations the
// current directory resolves to. `everywhere` reaches further and is handled
// by updateEverywhere; this is the form for "every harness, right here".
func updateEveryone(cmd *cobra.Command, cwd, keyword string, args []string, opts *options, cache map[string]sourceResolution) error {
	locations, missing, err := sweepLocations(cwd, keyword, opts)
	if err != nil {
		return err
	}
	where := "in any harness here"
	if keyword == "" && !opts.project && !opts.global {
		where = "in any harness for this directory"
	}
	return updateSweep(cmd, updateSweepRequest{
		locations: locations, missing: missing, harnesses: everywhereHarnesses(opts),
		args: args, opts: opts, cache: cache, where: where,
	})
}

// everywhereHarnesses resolves which presets an everywhere sweep visits: the
// explicitly selected one, or all of them. Claude leads for familiarity, and
// codex precedes amp so the shared .agents metadata file is credited to
// codex — the same preference DetectedHarnesses applies.
func everywhereHarnesses(opts *options) []string {
	if opts.harness != "" {
		return []string{opts.harness}
	}
	order := bmo.HarnessPreferenceOrder()
	names := make([]string, 0, len(order))
	for _, harness := range order {
		names = append(names, string(harness))
	}
	return names
}

// recordCurrentProject backfills the registry from the current directory:
// repos installed into before the registry existed register the first time an
// "everywhere" sweep runs inside them, whichever harness they used. Failing to
// write the registry is not worth failing the sweep over — the destinations
// the sweep already resolved are unaffected.
func recordCurrentProject(cwd string, harnessNames []string) error {
	for _, hname := range harnessNames {
		project, err := bmo.ResolveTarget(hname, bmo.ScopeProject, cwd, "")
		if err != nil {
			return err
		}
		if hasTrackedSkills(project.MetadataPath) {
			_ = bmo.RecordProject(cwd)
			return nil
		}
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

// sourceResolution caches one resolution outcome — failures included, so ten
// skills sharing one dead source fail after a single network attempt instead
// of ten.
type sourceResolution struct {
	resolved bmo.ResolvedSource
	err      error
}

func updateScope(cmd *cobra.Command, cwd string, scope bmo.Scope, args []string, opts *options, cache map[string]sourceResolution) error {
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
	// One unreachable source or newly invalid skill must not strand the rest:
	// updating many skills is the common case, and aborting the sweep would
	// leave whichever names happened to sort later permanently stale.
	var failures []string
	for _, name := range names {
		entry := targets[name]
		res, ok := cache[entry.Source]
		if !ok {
			src, err := bmo.ParseSource(entry.Source)
			if err == nil {
				res.resolved, res.err = bmo.ResolveSource(src)
			} else {
				res.err = err
			}
			cache[entry.Source] = res
		}
		if res.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", name, res.err))
			continue
		}
		resolved := res.resolved
		skill, err := selectSkill(resolved.Root, name)
		if err == nil {
			skill, err = bmo.ValidateSkill(skill.Path, name)
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if !skillChanged(skill.Path, entry.InstalledPath) {
			fmt.Fprintf(cmd.OutOrStdout(), "%s is up to date\n", name)
			continue
		}
		_, err = bmo.InstallSkill(bmo.InstallOptions{Scope: scope, Target: target, Name: name, Force: true, DryRun: opts.dryRun, CWD: cwd, Source: resolved.Source, Skill: skill})
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if opts.dryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "Dry run: would update %s\n", name)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Updated %s\n", name)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d of %d skills could not be updated:\n  %s",
			len(failures), len(names), strings.Join(failures, "\n  "))
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

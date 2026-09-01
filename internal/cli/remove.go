package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/justin06lee/bmo/internal/bmo"
	"github.com/spf13/cobra"
)

func newRemoveCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove SKILL_NAME [here|everywhere|universe] [HARNESS|everyone]",
		Short: "Remove an installed skill",
		Long: `Uninstall a skill: its directory, the subagent files bmo recorded for it, and
its metadata entry.

` + "`here`" + ` and ` + "`everywhere`" + ` name one destination each, as they do everywhere else
in the CLI: this project, or the global skills directory.

` + "`universe`" + ` makes remove a sweep instead — the skill is deleted from the global
install and from every project in the registry, for every harness, in a single
run. ` + "`everyone`" + ` widens whichever location was named across every harness, so
` + "`bmo remove NAME everywhere everyone`" + ` clears every harness's global install and
nothing else. Naming a harness limits a sweep to that harness.

Run ` + "`bmo scout`" + ` first so ` + "`bmo remove universe`" + ` knows about projects bmo has
not installed into itself.`,
		Example: `  bmo remove demo here
  bmo remove demo codex
  bmo remove demo universe
  bmo remove demo everywhere everyone`,
		Args: argsWithKeywords(cobra.ExactArgs(1)),
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
			if keyword == universeKeyword || everyone {
				return removeSweep(cmd, cwd, args[0], keyword, effective)
			}
			return removeOne(cmd, cwd, args[0], keyword, effective)
		},
	}
	cmd.Flags().BoolVar(&opts.project, "project", false, "Use project metadata")
	cmd.Flags().BoolVar(&opts.global, "global", false, "Use global metadata")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Skip interactive confirmation")
	addHarnessFlags(cmd, opts)
	cmd.MarkFlagsMutuallyExclusive("project", "global")
	return cmd
}

// removeOne removes a skill from the single destination the scope keyword and
// flags resolve to.
func removeOne(cmd *cobra.Command, cwd, name, keyword string, opts *options) error {
	scope := keywordScope(keyword, opts)
	target, err := targetFor(scope, cwd, opts)
	if err != nil {
		return err
	}
	meta, err := bmo.ReadMetadata(target.MetadataPath)
	if err != nil {
		return err
	}
	entry, ok := meta.Skills[name]
	if !ok {
		return fmt.Errorf("skill is not tracked by bmo in %s scope: %s\nTry: bmo remove %s universe, bmo list, or bmo doctor", scope, name, name)
	}
	// The same refusal RemoveSkillFromTarget would raise, surfaced
	// before the preview so the user is not prompted to confirm a
	// removal that cannot happen (and the preview never renders an
	// empty subagent destination).
	if reason := removalBlocked(entry, target); reason != "" {
		return errors.New(reason)
	}
	installed, onDisk := bmo.TrackedSkillPath(name, entry, target)
	if onDisk {
		fmt.Fprintf(cmd.OutOrStdout(), "Remove %s from %s\n", entry.Name, installed)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Untrack %s: no copy of it is left in %s\n", entry.Name, target.SkillsDir)
	}
	if len(entry.Agents) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "Also removes %d subagents from %s: %s\n",
			len(entry.Agents), target.AgentsDir, strings.Join(entry.Agents, ", "))
	}
	if err := removalConfirmed(cmd, opts, "Remove? [y/N] "); err != nil {
		return err
	}
	removed, err := bmo.RemoveSkillFromTarget(name, target)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", removed.Name)
	return nil
}

// removalConfirmed asks once for a whole removal, unless --yes already
// answered.
func removalConfirmed(cmd *cobra.Command, opts *options, prompt string) error {
	if opts.yes {
		return nil
	}
	ok, err := confirm(cmd, prompt)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("remove cancelled")
	}
	return nil
}

// removal is one destination a sweeping remove acts on: the target holding a
// tracked copy of the skill, its metadata entry, and the label the sweep
// reports it under.
type removal struct {
	label  string
	target bmo.Target
	entry  bmo.SkillMeta
	// path is the directory that will be deleted. It is empty when the tracked
	// copy is already gone and only the metadata entry is left to clean up.
	path string
	// reason is empty when the copy can be removed, and explains the refusal
	// when it cannot.
	reason string
}

// removeSweep deletes every copy of one skill the sweep can reach: the whole
// machine under "universe", or every harness at the location "everyone" was
// paired with. Unlike the single-destination form it is not an error for a
// location to lack the skill — a sweep is about the end state, so destinations
// that never had it are simply not visited. Only a sweep that finds no copy at
// all fails.
//
// One destination's refusal must not strand the others, so blocked copies are
// reported up front — before the confirmation, so nobody approves a removal
// that cannot happen — and again as the aggregated error at the end.
func removeSweep(cmd *cobra.Command, cwd, name, keyword string, opts *options) error {
	if opts.skillsDir != "" {
		return errors.New("a sweeping remove resolves its own destinations and cannot discover arbitrary --skills-dir locations; drop \"universe\"/\"everyone\" to remove from that one directory")
	}
	harnesses := everywhereHarnesses(opts)
	if keyword == universeKeyword {
		// Backfill: a repo installed into before the registry existed
		// registers the first time a sweep runs inside it.
		if err := recordCurrentProject(cwd, harnesses); err != nil {
			return err
		}
	}
	locations, missing, err := sweepLocations(cwd, keyword, opts)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	for _, dir := range missing {
		fmt.Fprintf(out, "Skipping %s (directory no longer exists)\n", dir)
	}
	if len(missing) > 0 {
		fmt.Fprintln(out)
	}
	found, err := findRemovals(name, locations, harnesses)
	if err != nil {
		return err
	}
	var removals, blocked []removal
	for _, hit := range found {
		if hit.reason != "" {
			blocked = append(blocked, hit)
			continue
		}
		removals = append(removals, hit)
	}
	for _, hit := range blocked {
		fmt.Fprintf(out, "Cannot remove from %s: %s\n", hit.label, hit.reason)
	}
	if len(blocked) > 0 {
		fmt.Fprintln(out)
	}
	if len(removals) == 0 {
		if len(blocked) > 0 {
			return blockedError(name, blocked)
		}
		return fmt.Errorf("skill is not tracked by bmo %s: %s\nTry: bmo list, bmo scout, or bmo doctor", sweepWhere(keyword, opts), name)
	}
	fmt.Fprintf(out, "Remove %s from %d %s:\n", name, len(removals), plural(len(removals), "place", "places"))
	for _, hit := range removals {
		if hit.path == "" {
			fmt.Fprintf(out, "  %s: no copy left in %s; untracks the entry only\n", hit.label, hit.target.SkillsDir)
		} else {
			fmt.Fprintf(out, "  %s: %s\n", hit.label, hit.path)
		}
		if len(hit.entry.Agents) > 0 {
			fmt.Fprintf(out, "    and %d subagents in %s: %s\n",
				len(hit.entry.Agents), hit.target.AgentsDir, strings.Join(hit.entry.Agents, ", "))
		}
	}
	if err := removalConfirmed(cmd, opts, fmt.Sprintf("Remove %s? [y/N] ", plural(len(removals), "it", "them all"))); err != nil {
		return err
	}
	// A failure partway through is reported at the end rather than aborting:
	// stopping would leave the skill installed in the destinations that
	// happened to sort later, which is the state the user asked to be rid of.
	var failures []string
	deleted, untracked := 0, 0
	for _, hit := range removals {
		if _, err := bmo.RemoveSkillFromTarget(name, hit.target); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", hit.label, err))
			continue
		}
		if hit.path == "" {
			untracked++
			fmt.Fprintf(out, "Untracked %s in %s\n", name, hit.label)
			continue
		}
		deleted++
		fmt.Fprintf(out, "Removed %s from %s\n", name, hit.label)
	}
	if deleted > 0 {
		fmt.Fprintf(out, "Removed %d %s of %s.\n", deleted, plural(deleted, "copy", "copies"), name)
	}
	if untracked > 0 {
		fmt.Fprintf(out, "Untracked %d stale %s of %s.\n", untracked, plural(untracked, "entry", "entries"), name)
	}
	if len(failures) > 0 {
		for _, hit := range blocked {
			failures = append(failures, fmt.Sprintf("%s: %s", hit.label, hit.reason))
		}
		return fmt.Errorf("%d of %d copies could not be removed:\n  %s",
			len(failures), len(removals)+len(blocked), strings.Join(failures, "\n  "))
	}
	if len(blocked) > 0 {
		return blockedError(name, blocked)
	}
	return nil
}

// findRemovals collects every tracked copy of the skill across the sweep's
// locations and harnesses. Presets that share one metadata file (codex and amp
// share a project's .agents) hold one install, so each file is visited once and
// credited to the first name.
func findRemovals(name string, locations []sweepLocation, harnesses []string) ([]removal, error) {
	var found []removal
	seenMetadata := map[string]bool{}
	for _, location := range locations {
		for _, hname := range harnesses {
			target, err := bmo.ResolveTarget(hname, location.scope, location.dir, "")
			if err != nil {
				return nil, err
			}
			if seenMetadata[target.MetadataPath] {
				continue
			}
			seenMetadata[target.MetadataPath] = true
			meta, err := bmo.ReadMetadata(target.MetadataPath)
			label := fmt.Sprintf("%s (%s)", location.label, hname)
			if err != nil {
				// Unreadable metadata is reported rather than skipped: it may
				// well be the file tracking the copy the user wants gone.
				found = append(found, removal{label: label, target: target, reason: err.Error()})
				continue
			}
			entry, ok := meta.Skills[name]
			if !ok {
				continue
			}
			path, _ := bmo.TrackedSkillPath(name, entry, target)
			found = append(found, removal{
				label: label, target: target, entry: entry,
				path: path, reason: removalBlocked(entry, target),
			})
		}
	}
	return found, nil
}

// removalBlocked explains why a tracked copy cannot be removed, or returns an
// empty string when it can. It is the refusal RemoveSkillFromTarget would
// raise, checked early so a preview never promises a removal that cannot
// happen.
func removalBlocked(entry bmo.SkillMeta, target bmo.Target) string {
	if len(entry.Agents) > 0 && !target.SupportsAgents() {
		return fmt.Sprintf("metadata tracks subagents but harness %s has no compatible agent destination; see bmo doctor", target.Harness)
	}
	return ""
}

// blockedError reports the destinations a sweep refused to touch, so a partial
// removal still exits non-zero with the reason attached.
func blockedError(name string, blocked []removal) error {
	reasons := make([]string, 0, len(blocked))
	for _, hit := range blocked {
		reasons = append(reasons, fmt.Sprintf("%s: %s", hit.label, hit.reason))
	}
	return fmt.Errorf("%s could not be removed from %d %s:\n  %s",
		name, len(blocked), plural(len(blocked), "destination", "destinations"), strings.Join(reasons, "\n  "))
}

// sweepWhere names a sweep's reach for its "nothing tracked" message, so the
// refusal describes the ground actually covered rather than a scope the user
// never named.
func sweepWhere(keyword string, opts *options) string {
	switch {
	case keyword == universeKeyword:
		return "anywhere"
	case keyword == "everywhere" || opts.global:
		return "in any harness globally"
	case keyword == "here" || opts.project:
		return "in any harness here"
	default:
		return "in any harness for this directory"
	}
}

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/justin06lee/bmo/internal/bmo"
	"github.com/spf13/cobra"
)

// shareParticipant is one harness destination taking part in a sync, together
// with the skills it already tracks there.
type shareParticipant struct {
	target    bmo.Target
	harnesses []string
	meta      bmo.Metadata
}

// label names the destination. Presets that resolve to one directory (codex
// and amp share a project's .agents) are reported together, because they are
// one set of files.
func (p shareParticipant) label() string { return strings.Join(p.harnesses, "+") }

// shareDonor is the copy of a skill a sync reads from.
type shareDonor struct {
	participant int
	skill       bmo.Skill
	source      bmo.Source
}

// shareCopy is one planned addition: a skill one harness has and another does
// not.
type shareCopy struct {
	to    int
	from  string
	name  string
	skill bmo.Skill
	// source is the donor's recorded provenance, carried across so the new
	// copy updates from the same upstream rather than from its donor.
	source bmo.Source
}

// shareSkip records a skill a sync deliberately left alone. An empty `to`
// means the skill could not be read from any harness at all.
type shareSkip struct {
	to     string
	name   string
	reason string
}

// sharePlan is one location's work: who takes part, what would be copied, and
// what was left alone.
type sharePlan struct {
	location     sweepLocation
	participants []shareParticipant
	copies       []shareCopy
	skips        []shareSkip
	// donorAbsent marks a named-harness sync whose donor has nothing to give
	// at this location, so the summary can say that rather than claim
	// everything is already in sync.
	donorAbsent bool
}

func newShareCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "share [here|everywhere] [everyone|HARNESS]",
		Short: "Give every harness the same set of skills",
		Long: `Sync bmo-tracked skills across coding harnesses so each one ends up with the
union of what all of them have.

The sync is purely additive. A skill is only ever copied into a harness that
does not already have it, nothing is replaced, and nothing is deleted, so
running it twice is a no-op. Skills stay in the location they were installed
in: a project's harnesses exchange that project's skills, and the global
destinations exchange global skills.

Naming one harness seeds every other harness from it instead, leaving that
harness untouched.

Copies are made from the installed skill folder, not re-downloaded, but each
copy keeps the donor's recorded source so ` + "`bmo update`" + ` still tracks the real
upstream. Use ` + "`bmo scout`" + ` first so ` + "`bmo share everywhere`" + ` knows about every
project.`,
		Example: `  bmo share
  bmo share everywhere everyone
  bmo share here
  bmo share codex --dry-run`,
		Args: argsWithKeywords(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			_, keyword, positionalHarness, err := splitCommandKeywords(cmd, args)
			if err != nil {
				return err
			}
			if opts.skillsDir != "" {
				return errors.New("share syncs the built-in harness presets; --skills-dir names a single destination instead")
			}
			from, err := shareDonorHarness(opts, positionalHarness)
			if err != nil {
				return err
			}
			if err := keywordScopeConflict(keyword, opts); err != nil {
				return err
			}
			locations, missing, err := sweepLocations(cwd, keyword, opts)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, dir := range missing {
				fmt.Fprintf(out, "Skipping %s (directory no longer exists)\n", dir)
			}
			plans, err := sharePlans(locations, from)
			if err != nil {
				return err
			}
			printSharePlans(cmd, plans, from)
			copies := shareCopyCount(plans)
			if copies == 0 {
				return nil
			}
			if !opts.yes && !opts.dryRun {
				ok, err := confirm(cmd, fmt.Sprintf("Copy %d %s? [y/N] ", copies, plural(copies, "skill", "skills")))
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("share cancelled")
				}
			}
			return executeSharePlans(cmd, plans, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.project, "project", false, "Sync only this project's destinations")
	cmd.Flags().BoolVar(&opts.global, "global", false, "Sync only the global destinations")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Skip interactive confirmation")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show what would be copied without writing anything")
	addHarnessFlags(cmd, opts)
	cmd.MarkFlagsMutuallyExclusive("project", "global")
	return cmd
}

// shareDonorHarness resolves the optional single-harness form. "everyone" and
// no harness at all both mean the full two-way sync; naming one harness makes
// it the only donor.
func shareDonorHarness(opts *options, positionalHarness string) (string, error) {
	selector := positionalHarness
	if selector == everyoneKeyword {
		if opts.harness != "" {
			return "", errors.New(`"everyone" already covers every harness; drop --harness`)
		}
		return "", nil
	}
	if opts.harness != "" {
		if selector != "" {
			return "", errors.New("a positional harness cannot be combined with --harness or --skills-dir")
		}
		selector = opts.harness
	}
	if selector == "" {
		return "", nil
	}
	harness, err := bmo.ParseHarness(selector)
	if err != nil {
		return "", err
	}
	return string(harness), nil
}

func sharePlans(locations []sweepLocation, from string) ([]sharePlan, error) {
	// Detection probes PATH, config directories, and app bundles; it answers
	// the same for every location, so a sweep across fifty repos runs it once.
	detected := map[bmo.Harness]bool{}
	for _, info := range bmo.DetectedHarnesses() {
		detected[info.Name] = true
	}
	plans := make([]sharePlan, 0, len(locations))
	for _, location := range locations {
		participants, err := shareParticipants(location, detected)
		if err != nil {
			return nil, err
		}
		plan := sharePlan{location: location, participants: participants}
		planShareCopies(&plan, from)
		plans = append(plans, plan)
	}
	return plans, nil
}

// shareParticipants resolves the destinations that take part in a sync at one
// location: every harness detected on this machine, plus any harness that
// already tracks skills there. Excluding the rest keeps share from creating
// configuration trees for tools the user does not have, while never leaving
// out a harness that is demonstrably in use.
func shareParticipants(location sweepLocation, detected map[bmo.Harness]bool) ([]shareParticipant, error) {
	var participants []shareParticipant
	byDir := map[string]int{}
	for _, harness := range bmo.HarnessPreferenceOrder() {
		target, err := bmo.ResolveTarget(string(harness), location.scope, location.dir, "")
		if err != nil {
			return nil, err
		}
		key := filepath.Clean(target.SkillsDir)
		if index, exists := byDir[key]; exists {
			// A second preset resolving to the same folder is the same set of
			// files, not a second destination to copy into.
			participants[index].harnesses = append(participants[index].harnesses, string(harness))
			continue
		}
		meta, err := bmo.ReadMetadata(target.MetadataPath)
		if err != nil {
			return nil, fmt.Errorf("%s (%s): %w", location.label, harness, err)
		}
		if !detected[harness] && len(meta.Skills) == 0 {
			continue
		}
		byDir[key] = len(participants)
		participants = append(participants, shareParticipant{
			target: target, harnesses: []string{string(harness)}, meta: meta,
		})
	}
	return participants, nil
}

// planShareCopies fills in what one location's sync would do. Every reason to
// refuse is decided here, before anything is written, so the preview the user
// confirms is the work that actually runs.
func planShareCopies(plan *sharePlan, from string) {
	donorIndices := make([]int, 0, len(plan.participants))
	fromIndex := -1
	for i, participant := range plan.participants {
		if from != "" && !slices.Contains(participant.harnesses, from) {
			continue
		}
		if from != "" {
			fromIndex = i
		}
		donorIndices = append(donorIndices, i)
	}
	if from != "" && fromIndex == -1 {
		plan.donorAbsent = true
		return
	}
	var names []string
	for _, i := range donorIndices {
		for name := range plan.participants[i].meta.Skills {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)

	// One donor is chosen per skill up front: the choice does not depend on
	// the recipient, and resolving it once means an unreadable copy is
	// reported once instead of once per destination.
	donors := map[string]shareDonor{}
	for _, name := range names {
		var reason error
		for _, i := range donorIndices {
			entry, ok := plan.participants[i].meta.Skills[name]
			if !ok {
				continue
			}
			// The installed folder is the source. Re-resolving each skill's
			// upstream would turn a local sync into a network operation that
			// fails whenever a repository moved, while the copy on disk is
			// exactly what the donor harness is using today.
			skill, err := bmo.ValidateSkill(entry.InstalledPath, name)
			if err != nil {
				reason = err
				continue
			}
			donors[name] = shareDonor{participant: i, skill: skill, source: bmo.SourceFromMeta(entry)}
			break
		}
		if _, ok := donors[name]; !ok {
			plan.skips = append(plan.skips, shareSkip{
				name:   name,
				reason: fmt.Sprintf("no harness holds a readable copy (%v)", reason),
			})
		}
	}

	for i := range plan.participants {
		if i == fromIndex {
			continue
		}
		recipient := plan.participants[i]
		for _, name := range names {
			donor, ok := donors[name]
			if !ok {
				continue
			}
			if _, tracked := recipient.meta.Skills[name]; tracked {
				continue
			}
			if err := bmo.ValidateSkillForTarget(donor.skill, recipient.target); err != nil {
				plan.skips = append(plan.skips, shareSkip{to: recipient.label(), name: name, reason: err.Error()})
				continue
			}
			conflict, err := bmo.CheckInstallConflictsForTarget(donor.skill, recipient.target)
			if err != nil {
				plan.skips = append(plan.skips, shareSkip{to: recipient.label(), name: name, reason: err.Error()})
				continue
			}
			if !conflict.Empty() {
				// Additive means additive: something bmo did not put there
				// stays exactly as it is.
				plan.skips = append(plan.skips, shareSkip{to: recipient.label(), name: name, reason: shareConflictReason(conflict)})
				continue
			}
			plan.copies = append(plan.copies, shareCopy{
				to: i, from: plan.participants[donor.participant].label(),
				name: name, skill: donor.skill, source: donor.source,
			})
		}
	}
}

// shareConflictReason explains an untracked collision in one line.
func shareConflictReason(conflict bmo.Conflict) string {
	var reasons []string
	if conflict.Path != "" {
		reasons = append(reasons, "a folder bmo does not track is already at "+conflict.Path)
	}
	if len(conflict.Agents) > 0 {
		reasons = append(reasons, "it ships subagents bmo does not own: "+strings.Join(conflict.Agents, ", "))
	}
	return strings.Join(reasons, "; ")
}

func shareCopyCount(plans []sharePlan) int {
	total := 0
	for _, plan := range plans {
		total += len(plan.copies)
	}
	return total
}

func printSharePlans(cmd *cobra.Command, plans []sharePlan, from string) {
	out := cmd.OutOrStdout()
	if from != "" {
		fmt.Fprintf(out, "Seeding every other harness from %s.\n\n", from)
	}
	participants := 0
	for _, plan := range plans {
		participants += len(plan.participants)
		fmt.Fprintf(out, "%s:\n", plan.location.label)
		if len(plan.participants) == 0 {
			fmt.Fprintln(out, "  no harnesses detected or in use here")
			fmt.Fprintln(out)
			continue
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		gains := map[int][]shareCopy{}
		for _, item := range plan.copies {
			gains[item.to] = append(gains[item.to], item)
		}
		for i, participant := range plan.participants {
			change := "up to date"
			if added := gains[i]; len(added) > 0 {
				change = fmt.Sprintf("+%d: %s", len(added), strings.Join(shareCopyNames(added), ", "))
			}
			fmt.Fprintf(tw, "  %s\t%s\t%d %s\t%s\n",
				participant.label(), participant.target.SkillsDir,
				len(participant.meta.Skills), plural(len(participant.meta.Skills), "skill", "skills"), change)
		}
		tw.Flush()
		for _, skip := range plan.skips {
			if skip.to == "" {
				fmt.Fprintf(out, "  skipped %s everywhere: %s\n", skip.name, skip.reason)
				continue
			}
			fmt.Fprintf(out, "  skipped %s for %s: %s\n", skip.name, skip.to, skip.reason)
		}
		fmt.Fprintln(out)
	}
	copies := shareCopyCount(plans)
	if copies == 0 {
		skips, donorAbsent := 0, 0
		for _, plan := range plans {
			skips += len(plan.skips)
			if plan.donorAbsent {
				donorAbsent++
			}
		}
		switch {
		case donorAbsent == len(plans) && from != "":
			fmt.Fprintf(out, "Nothing to share: %s tracks no skills in any of these locations.\n", from)
		case participants < 2:
			fmt.Fprintln(out, "Nothing to share: a sync needs at least two harness destinations.")
		case skips > 0:
			// Saying "already in sync" here would paper over the copies the
			// plan deliberately refused to make.
			fmt.Fprintf(out, "Nothing to copy: %d %s skipped, listed above.\n", skips, plural(skips, "addition was", "additions were"))
		default:
			fmt.Fprintln(out, "Nothing to share: every harness already has the same skills.")
		}
		return
	}
	active := 0
	for _, plan := range plans {
		if len(plan.copies) > 0 {
			active++
		}
	}
	fmt.Fprintf(out, "%d %s to copy across %d %s. Nothing is replaced or removed.\n\n",
		copies, plural(copies, "skill", "skills"),
		active, plural(active, "location", "locations"))
}

func shareCopyNames(copies []shareCopy) []string {
	names := make([]string, 0, len(copies))
	for _, item := range copies {
		names = append(names, item.name)
	}
	return names
}

// executeSharePlans performs the planned copies. Each one was preflighted as a
// conflict-free addition, so a failure here is local to that copy: the sweep
// finishes the rest and reports what broke, and a rerun simply skips whatever
// already landed.
func executeSharePlans(cmd *cobra.Command, plans []sharePlan, opts *options) error {
	out := cmd.OutOrStdout()
	copied := 0
	var failures []string
	for _, plan := range plans {
		for _, item := range plan.copies {
			recipient := plan.participants[item.to]
			_, err := bmo.InstallSkill(bmo.InstallOptions{
				Scope:  plan.location.scope,
				Target: recipient.target,
				DryRun: opts.dryRun,
				CWD:    plan.location.dir,
				Source: item.source,
				Skill:  item.skill,
			})
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s to %s: %v", item.name, recipient.label(), err))
				continue
			}
			copied++
			if !opts.dryRun {
				fmt.Fprintf(out, "Copied %s from %s to %s\n", item.name, item.from, recipient.label())
			}
		}
	}
	if opts.dryRun {
		fmt.Fprintf(out, "Dry run: would copy %d %s.\n", copied, plural(copied, "skill", "skills"))
		return nil
	}
	fmt.Fprintf(out, "\nShared %d %s.\n", copied, plural(copied, "skill", "skills"))
	if len(failures) > 0 {
		return fmt.Errorf("%d %s could not be copied:\n  %s",
			len(failures), plural(len(failures), "skill", "skills"), strings.Join(failures, "\n  "))
	}
	return nil
}

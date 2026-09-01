package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/justin06lee/bmo/internal/bmo"
	"github.com/spf13/cobra"
)

// scoutReport is the --json shape. It reports the sweep and what it changed,
// so a script can diff the registry without re-reading it.
type scoutReport struct {
	bmo.ScoutResult
	Recorded []string `json:"recorded"`
	Pruned   []string `json:"pruned,omitempty"`
	Total    int      `json:"total_registered"`
}

func newScoutCommand(opts *options) *cobra.Command {
	var (
		depth  int
		hidden bool
		prune  bool
	)
	cmd := &cobra.Command{
		Use:   "scout [PATH]",
		Short: "Find bmo-installed skills below a directory and record their projects",
		Long: `Walk every directory below PATH (the current directory by default) and record
the projects bmo has installed skills into, so ` + "`bmo update everywhere`" + ` and
` + "`bmo share`" + ` can reach them without being run inside each repo.

Dependency, build, and VCS trees (node_modules, .venv, target, .git, …) are
skipped, as are unrelated hidden directories. A harness's own configuration
directory is always inspected. Nothing is installed, moved, or deleted: scout
only reads skill metadata and writes ~/.bmo/projects.json.`,
		Example: `  bmo scout
  bmo scout ~/code --depth 3
  bmo scout --dry-run
  bmo scout --prune`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := ""
			if len(args) == 1 {
				root = args[0]
			}
			if depth < 0 {
				return fmt.Errorf("--depth cannot be negative: %d", depth)
			}
			result, err := bmo.Scout(bmo.ScoutOptions{Root: root, MaxDepth: depth, IncludeHidden: hidden})
			if err != nil {
				return err
			}
			var recorded, pruned []string
			if !opts.dryRun {
				// Pruning first keeps a directory that was deleted and
				// recreated from being reported as both pruned and recorded.
				if prune {
					pruned, err = bmo.PruneProjects()
					if err != nil {
						return err
					}
				}
				recorded, err = bmo.RecordProjects(result.ProjectDirs())
				if err != nil {
					return err
				}
			}
			total, err := bmo.RegisteredProjects()
			if err != nil {
				return err
			}
			if opts.json {
				report := scoutReport{ScoutResult: result, Recorded: recorded, Pruned: pruned, Total: len(total)}
				if report.Findings == nil {
					report.Findings = []bmo.ScoutFinding{}
				}
				if report.Recorded == nil {
					report.Recorded = []string{}
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			}
			printScoutReport(cmd, result, recorded, pruned, len(total), opts.dryRun)
			return nil
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 0, "Limit how many directories below PATH a project may sit (0 = unlimited)")
	cmd.Flags().BoolVar(&hidden, "hidden", false, "Also descend into unrelated hidden directories")
	cmd.Flags().BoolVar(&prune, "prune", false, "Drop registered projects whose directory no longer exists")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Report findings without recording them")
	cmd.Flags().BoolVar(&opts.json, "json", false, "Output JSON")
	return cmd
}

func printScoutReport(cmd *cobra.Command, result bmo.ScoutResult, recorded, pruned []string, total int, dryRun bool) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Scouted %s\n\n", result.Root)
	if len(result.Findings) == 0 {
		fmt.Fprintf(out, "No bmo installs found in %d directories.\n", result.Scanned)
	} else {
		newDirs := map[string]bool{}
		for _, dir := range recorded {
			newDirs[dir] = true
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "PROJECT\tHARNESSES\tSKILLS\tREGISTRY")
		for _, finding := range result.Findings {
			state := "recorded"
			switch {
			case finding.Registered:
				state = "known"
			case dryRun:
				state = "would record"
			case !newDirs[finding.Dir]:
				// Two harness directories under one project resolve to a
				// single registry entry, so only the first is "recorded".
				state = "known"
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
				finding.Dir, joinNames(finding.Harnesses), len(finding.Skills), state)
		}
		tw.Flush()
		fmt.Fprintf(out, "\nFound %d %s holding %d tracked %s in %d directories.\n",
			len(result.Findings), plural(len(result.Findings), "project", "projects"),
			result.SkillCount(), plural(result.SkillCount(), "skill", "skills"), result.Scanned)
	}
	for _, lock := range result.Invalid {
		fmt.Fprintf(out, "Warning: unreadable metadata, skipped: %s\n", lock)
	}
	if len(result.Unreadable) > 0 {
		fmt.Fprintf(out, "Warning: %d directories could not be read (first: %s)\n",
			len(result.Unreadable), result.Unreadable[0])
	}
	if dryRun {
		fmt.Fprintln(out, "\nDry run: the project registry was not changed.")
		return
	}
	for _, dir := range pruned {
		fmt.Fprintf(out, "Forgot %s (directory no longer exists)\n", dir)
	}
	fmt.Fprintf(out, "Recorded %d new %s; %d registered for `bmo update everywhere`.\n",
		len(recorded), plural(len(recorded), "project", "projects"), total)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// joinNames renders a harness list for a table cell.
func joinNames(names []string) string {
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ", ")
}

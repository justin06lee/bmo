package bmo

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// scoutSkipDirs are directory names a sweep never descends into. Each one is a
// tree that makes a filesystem walk slow — dependency caches, build output, VCS
// internals — without ever holding a harness's project configuration. Keeping
// the list here rather than in the CLI means every caller of Scout prunes the
// same way.
var scoutSkipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".jj": true,
	"node_modules": true, "bower_components": true, ".pnpm-store": true, ".yarn": true,
	"vendor": true, "Pods": true, "Carthage": true, "DerivedData": true,
	".venv": true, "venv": true, "site-packages": true, "__pycache__": true,
	".tox": true, ".mypy_cache": true, ".pytest_cache": true, ".ruff_cache": true,
	"target": true, "dist": true, "build": true, "out": true, "coverage": true,
	".next": true, ".nuxt": true, ".svelte-kit": true, ".turbo": true, ".parcel-cache": true,
	".gradle": true, ".m2": true, ".cargo": true, ".rustup": true, ".stack-work": true,
	".terraform": true, ".ccache": true, ".cache": true, ".Trash": true,
}

// ScoutOptions configures a sweep for projects bmo has installed skills into.
type ScoutOptions struct {
	// Root is where the sweep starts. An empty value means the process
	// working directory.
	Root string
	// MaxDepth limits how far below Root a project may sit, counted in
	// directories (1 means Root and its immediate children). Zero is
	// unlimited.
	MaxDepth int
	// IncludeHidden also descends into dot-directories. Harness configuration
	// directories are always inspected regardless of this setting; it only
	// controls whether unrelated hidden trees are walked.
	IncludeHidden bool
}

// ScoutFinding is one project directory holding bmo-tracked skills.
type ScoutFinding struct {
	// Dir is the project root — the parent of the harness configuration
	// directory, and exactly what `bmo update everywhere` needs recorded.
	Dir string `json:"dir"`
	// Harnesses names the presets whose lock file lives under Dir, in
	// preference order.
	Harnesses []string `json:"harnesses"`
	// LockFiles are the metadata files that produced this finding.
	LockFiles []string `json:"lock_files"`
	// Skills is the union of tracked skill names across those lock files.
	Skills []string `json:"skills"`
	// Registered reports whether the registry already knew this directory
	// before the sweep.
	Registered bool `json:"registered"`
}

// ScoutResult is everything one sweep learned.
type ScoutResult struct {
	Root     string         `json:"root"`
	Findings []ScoutFinding `json:"findings"`
	// Scanned counts the directories the walk actually entered.
	Scanned int `json:"scanned"`
	// Unreadable lists directories the walk could not enter, usually a
	// permissions problem. A sweep reports them instead of aborting: one
	// unreadable folder must not hide every project beside it.
	Unreadable []string `json:"unreadable,omitempty"`
	// Invalid lists lock files that exist but could not be parsed. They are
	// real bmo projects with a real problem, so they are surfaced rather than
	// silently skipped.
	Invalid []string `json:"invalid,omitempty"`
}

// Scout walks the tree below opts.Root and reports every project directory
// whose harness configuration holds a bmo lock file tracking at least one
// skill. It reads metadata and never writes: recording the findings is the
// caller's decision.
//
// Only the built-in presets are discoverable, because only their project
// layout is predictable. Installs made with --skills-dir put their lock file
// in an arbitrary location, which is the same reason `bmo update everywhere`
// refuses that flag.
func Scout(opts ScoutOptions) (ScoutResult, error) {
	root := opts.Root
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return ScoutResult{}, err
		}
		root = cwd
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return ScoutResult{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return ScoutResult{}, err
	}
	if !info.IsDir() {
		return ScoutResult{}, fmt.Errorf("scout root is not a directory: %s", root)
	}

	registered := map[string]bool{}
	projects, err := RegisteredProjects()
	if err != nil {
		return ScoutResult{}, err
	}
	for _, dir := range projects {
		registered[dir] = true
	}

	byConfigDir := HarnessesByProjectConfigDir()
	result := ScoutResult{Root: root}
	found := map[string]*ScoutFinding{}

	collect := func(configDir string, infos []HarnessInfo) {
		lock := filepath.Join(configDir, ProjectLockFileName)
		if _, err := os.Stat(lock); err != nil {
			return
		}
		meta, err := ReadMetadata(lock)
		if err != nil {
			result.Invalid = append(result.Invalid, lock)
			return
		}
		if len(meta.Skills) == 0 {
			// A lock file that tracks nothing is not worth recording: an
			// update sweep would visit it and find no work.
			return
		}
		dir := filepath.Dir(configDir)
		finding, ok := found[dir]
		if !ok {
			finding = &ScoutFinding{Dir: dir, Registered: registered[dir]}
			found[dir] = finding
		}
		for _, harness := range infos {
			finding.Harnesses = append(finding.Harnesses, string(harness.Name))
		}
		finding.LockFiles = append(finding.LockFiles, lock)
		for name := range meta.Skills {
			// One project can track the same skill for several harnesses; the
			// finding names each skill once.
			if !slices.Contains(finding.Skills, name) {
				finding.Skills = append(finding.Skills, name)
			}
		}
	}

	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// A directory bmo cannot open is reported and stepped over. Only
			// an unreadable root is fatal, and Stat above already caught that.
			result.Unreadable = append(result.Unreadable, path)
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// WalkDir does not follow symlinks, so IsDir is false for a symlinked
		// directory: the sweep cannot loop, and it never leaves the tree the
		// user pointed it at.
		if !entry.IsDir() {
			return nil
		}
		result.Scanned++
		if path == root {
			return nil
		}
		name := entry.Name()
		// Harness configuration directories are matched before the skip and
		// hidden rules, because every one of them is hidden by design.
		if infos, ok := byConfigDir[name]; ok {
			collect(path, infos)
			return fs.SkipDir
		}
		if scoutSkipDirs[name] {
			return fs.SkipDir
		}
		if !opts.IncludeHidden && strings.HasPrefix(name, ".") {
			return fs.SkipDir
		}
		// The depth budget is spent on candidate project directories. A
		// project's own configuration directory sits one level deeper and is
		// matched above, before this check could prune it.
		if opts.MaxDepth > 0 && scoutDepth(root, path) > opts.MaxDepth {
			return fs.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		return ScoutResult{}, walkErr
	}

	order := HarnessPreferenceOrder()
	for _, finding := range found {
		sort.Strings(finding.Skills)
		sort.Strings(finding.LockFiles)
		// Report harnesses in bmo's canonical order rather than in the order
		// their configuration directories happened to sort in.
		sort.Slice(finding.Harnesses, func(i, j int) bool {
			return slices.Index(order, Harness(finding.Harnesses[i])) < slices.Index(order, Harness(finding.Harnesses[j]))
		})
		result.Findings = append(result.Findings, *finding)
	}
	sort.Slice(result.Findings, func(i, j int) bool { return result.Findings[i].Dir < result.Findings[j].Dir })
	sort.Strings(result.Unreadable)
	sort.Strings(result.Invalid)
	return result, nil
}

// scoutDepth reports how many directories below root path sits.
func scoutDepth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(os.PathSeparator)) + 1
}

// ProjectDirs returns just the directories a sweep found, ready to hand to
// RecordProjects.
func (r ScoutResult) ProjectDirs() []string {
	dirs := make([]string, 0, len(r.Findings))
	for _, finding := range r.Findings {
		dirs = append(dirs, finding.Dir)
	}
	return dirs
}

// SkillCount totals the tracked skills across every finding.
func (r ScoutResult) SkillCount() int {
	total := 0
	for _, finding := range r.Findings {
		total += len(finding.Skills)
	}
	return total
}

package bmo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AgentsDirName is the folder inside a skill that holds Claude Code subagent
// definitions. It is a plain convention: any skill may ship one.
const AgentsDirName = "agents"

// Agent is a Claude Code subagent definition shipped inside a skill.
//
// Subagents are not skills. A skill is instructions loaded into the current
// context; a subagent is a separate worker with its own context window, model,
// and tool allowlist, spawned by name. Claude Code discovers subagents from its
// agents directory, which sits beside the skills directory rather than inside
// it, so a skill's agents/ folder has to be installed to a second destination.
type Agent struct {
	// File is the base filename as shipped, e.g. "seo-technical.md". It is
	// also the installed filename, so a skill's layout is predictable.
	File string
	// Name is the frontmatter name, or the filename stem when absent. This is
	// what Claude Code resolves when spawning the subagent.
	Name        string
	Description string
}

// DiscoverAgents reads the top-level *.md files in a skill's agents/ folder.
// A missing folder is not an error: most skills ship no subagents.
//
// Only the top level is read. Claude Code does not scan nested folders, so
// silently installing them would produce agents that never resolve.
func DiscoverAgents(skillDir string) ([]Agent, error) {
	dir := filepath.Join(skillDir, AgentsDirName)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// An agent file excluded by .bmoignore is not installed, so it must not be
	// discovered either: tracking a subagent that was never copied would make
	// doctor report it as missing forever.
	ignore, err := LoadIgnore(skillDir)
	if err != nil {
		return nil, err
	}
	var agents []Agent
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		if ignore.Match(AgentsDirName+"/"+entry.Name(), false) {
			continue
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("refusing to read symlinked agent: %s", filepath.Join(AgentsDirName, entry.Name()))
		}
		agent, err := parseAgent(dir, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(AgentsDirName, entry.Name()), err)
		}
		agents = append(agents, agent)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].File < agents[j].File })
	return agents, nil
}

func parseAgent(dir, file string) (Agent, error) {
	content, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return Agent{}, err
	}
	fm, err := parseFrontmatter(content)
	if err != nil {
		return Agent{}, err
	}
	if strings.TrimSpace(fm.Description) == "" {
		return Agent{}, errors.New("agent frontmatter must include a non-empty description")
	}
	name := fm.Name
	if name == "" {
		name = NormalizeSkillName(strings.TrimSuffix(file, filepath.Ext(file)))
	}
	if err := ValidateAgentName(name); err != nil {
		return Agent{}, err
	}
	return Agent{File: file, Name: name, Description: fm.Description}, nil
}

// ValidateAgentName applies the skill name rules to a subagent name: Claude
// Code resolves subagents by this name, so the same kebab-case constraint keeps
// them addressable.
func ValidateAgentName(name string) error {
	if err := ValidateSkillName(name); err != nil {
		return fmt.Errorf("invalid agent name: %w", err)
	}
	return nil
}

// AgentFiles returns the installed filenames for a set of agents, which is what
// gets recorded in metadata so removal can be exact.
func AgentFiles(agents []Agent) []string {
	if len(agents) == 0 {
		return nil
	}
	files := make([]string, 0, len(agents))
	for _, agent := range agents {
		files = append(files, agent.File)
	}
	sort.Strings(files)
	return files
}

// AgentNames returns the resolved subagent names, for display.
func AgentNames(agents []Agent) []string {
	if len(agents) == 0 {
		return nil
	}
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		names = append(names, agent.Name)
	}
	sort.Strings(names)
	return names
}

// agentConflicts lists agent files that already exist in agentsDir and are not
// owned by the skill being installed. Installing over another skill's subagent
// would silently change that skill's behavior, so it is refused without --force.
func agentConflicts(agents []Agent, agentsDir string, owned map[string]bool) []string {
	var conflicts []string
	for _, agent := range agents {
		if owned[agent.File] {
			continue
		}
		if _, err := os.Stat(filepath.Join(agentsDir, agent.File)); err == nil {
			conflicts = append(conflicts, agent.File)
		}
	}
	sort.Strings(conflicts)
	return conflicts
}

// installAgents copies a skill's agent files into agentsDir.
//
// Existing files are moved aside first, so the returned rollback restores the
// directory exactly as it was if any later step of the install fails. Callers
// must invoke either rollback (on failure) or commit (on success); commit drops
// the backups.
func installAgents(agents []Agent, srcDir, agentsDir string) (rollback func(), commit func(), err error) {
	if len(agents) == 0 {
		return func() {}, func() {}, nil
	}
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return nil, nil, err
	}
	stamp := time.Now().UTC().Format("20060102150405")
	backups := map[string]string{} // target path -> backup path
	var written []string
	undo := func() {
		for _, path := range written {
			os.Remove(path)
		}
		for target, backup := range backups {
			os.Rename(backup, target)
		}
	}
	for _, agent := range agents {
		target := filepath.Join(agentsDir, agent.File)
		if _, statErr := os.Stat(target); statErr == nil {
			backup := target + ".bmo-backup-" + stamp
			if err := os.Rename(target, backup); err != nil {
				undo()
				return nil, nil, err
			}
			backups[target] = backup
		}
		if err := copyFile(filepath.Join(srcDir, AgentsDirName, agent.File), target); err != nil {
			undo()
			return nil, nil, err
		}
		written = append(written, target)
	}
	return undo, func() {
		for _, backup := range backups {
			os.Remove(backup)
		}
	}, nil
}

// removeAgents deletes tracked agent files from agentsDir. Files that are
// already gone are not an error: the goal is that they no longer exist.
func removeAgents(files []string, agentsDir string) error {
	for _, file := range files {
		target := filepath.Join(agentsDir, filepath.Base(file))
		if err := withinDir(agentsDir, target); err != nil {
			return fmt.Errorf("refusing to remove agent %s: %w", file, err)
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// staleAgents lists files a skill installed previously but no longer ships.
func staleAgents(previous []string, current []Agent) []string {
	if len(previous) == 0 {
		return nil
	}
	keep := map[string]bool{}
	for _, agent := range current {
		keep[agent.File] = true
	}
	var stale []string
	for _, file := range previous {
		if !keep[file] {
			stale = append(stale, file)
		}
	}
	sort.Strings(stale)
	return stale
}

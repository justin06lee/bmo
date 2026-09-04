package bmo

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// AgentsDirName is the folder inside a skill that holds Claude Code subagent
// definitions. It is a plain convention: any skill may ship one.
const AgentsDirName = "agents"

// Agent is a subagent definition shipped inside a skill, authored in the
// Claude Code format that the SKILL.md ecosystem standardized on.
//
// Subagents are not skills. A skill is instructions loaded into the current
// context; a subagent is a separate worker with its own context window, model,
// and tool allowlist, spawned by name. A harness discovers subagents from its
// agents directory, which sits beside the skills directory rather than inside
// it, so a skill's agents/ folder has to be installed to a second destination.
type Agent struct {
	// File is the base filename as shipped, e.g. "seo-technical.md". It is
	// also the installed filename, so a skill's layout is predictable, and it
	// is how harnesses that derive an agent's name from its filename resolve
	// the same agent bmo recorded.
	File string
	// Name is the frontmatter name, or the filename stem when absent. This is
	// what a harness resolves when spawning the subagent.
	Name        string
	Description string
	// ExtraKeys are the frontmatter keys beyond name and description, sorted.
	// They are Claude's vocabulary, so exporting them to another harness is
	// what AgentFormat.Portable drops; recording them here lets the CLI say
	// which constraints an export left behind.
	ExtraKeys []string
}

// AgentFormat is how one harness expects a subagent file to be written.
//
// Every harness bmo installs agents for reads Claude-format Markdown with YAML
// frontmatter, but only Claude reads every key in it. `model: sonnet` and
// `tools: Read, Grep` name a model and tools that exist in Claude and nowhere
// else, and each harness resolves them differently: Grok Build accepts any
// model string and fails to resolve it when the subagent is spawned, and a tool
// allowlist naming Claude's tools matches none of another harness's, leaving an
// agent that can do nothing. Carrying those keys across would produce subagents
// that install cleanly and then misbehave.
type AgentFormat struct {
	// Verbatim writes the file through byte-for-byte. Claude Code defined the
	// format, so its own files need no translation.
	Verbatim bool
	// NameInFrontmatter is whether the harness resolves an agent's name from a
	// name: key. Harnesses that derive it from the filename instead have no
	// such key in their schema, and bmo preserves the filename for them.
	NameInFrontmatter bool
}

// portableAgentFrontmatter is the frontmatter bmo writes when translating an
// agent for a non-Claude harness: the two keys every supported harness agrees
// on. Struct order is the emitted order.
type portableAgentFrontmatter struct {
	Name        string `yaml:"name,omitempty"`
	Description string `yaml:"description"`
}

// DroppedKeys reports the frontmatter this agent would lose when written in the
// given format, so an install can say so instead of silently widening what the
// subagent is allowed to do.
func (a Agent) DroppedKeys(format AgentFormat) []string {
	if format.Verbatim {
		return nil
	}
	dropped := append([]string(nil), a.ExtraKeys...)
	if !format.NameInFrontmatter && a.Name != "" {
		dropped = append(dropped, "name")
		sort.Strings(dropped)
	}
	return dropped
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
	extra, err := extraAgentKeys(content)
	if err != nil {
		return Agent{}, err
	}
	return Agent{File: file, Name: name, Description: fm.Description, ExtraKeys: extra}, nil
}

// splitAgentFrontmatter separates an agent file's YAML frontmatter from the
// Markdown body that follows it. The body is returned exactly as written: it is
// the agent's prompt, and it is the one part every harness reads the same way.
func splitAgentFrontmatter(content []byte) (frontmatter, body []byte, err error) {
	content = bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})
	if !bytes.HasPrefix(content, []byte("---\n")) && !bytes.HasPrefix(content, []byte("---\r\n")) {
		return nil, nil, errors.New("agent file must start with YAML frontmatter")
	}
	lines := bytes.Split(content, []byte("\n"))
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(string(lines[i])) == "---" {
			rest := lines[i+1:]
			return bytes.Join(lines[1:i], []byte("\n")), bytes.Join(rest, []byte("\n")), nil
		}
	}
	return nil, nil, errors.New("agent frontmatter is not closed")
}

// extraAgentKeys lists the frontmatter keys beyond name and description, which
// are the keys a portable export cannot carry.
func extraAgentKeys(content []byte) ([]string, error) {
	frontmatter, _, err := splitAgentFrontmatter(content)
	if err != nil {
		return nil, err
	}
	var keys map[string]yaml.Node
	if err := yaml.Unmarshal(frontmatter, &keys); err != nil {
		return nil, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	var extra []string
	for key := range keys {
		if key != "name" && key != "description" {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	return extra, nil
}

// renderAgent produces the bytes bmo writes for one agent in one harness's
// format. Verbatim formats get the original file; every other harness gets
// frontmatter rebuilt from the keys it actually understands, with the prompt
// body preserved byte-for-byte.
func renderAgent(content []byte, agent Agent, format AgentFormat) ([]byte, error) {
	if format.Verbatim {
		return content, nil
	}
	_, body, err := splitAgentFrontmatter(content)
	if err != nil {
		return nil, err
	}
	front := portableAgentFrontmatter{Description: agent.Description}
	if format.NameInFrontmatter {
		front.Name = agent.Name
	}
	encoded, err := yaml.Marshal(front)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(encoded)
	out.WriteString("---\n")
	out.Write(body)
	return out.Bytes(), nil
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

// installAgents writes a skill's agent files into agentsDir in the destination
// harness's format.
//
// Existing files are moved aside first, so the returned rollback restores the
// directory exactly as it was if any later step of the install fails. Callers
// must invoke either rollback (on failure) or commit (on success); commit drops
// the backups.
func installAgents(agents []Agent, srcDir, agentsDir string, format AgentFormat) (rollback func(), commit func(), err error) {
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
		content, readErr := os.ReadFile(filepath.Join(srcDir, AgentsDirName, agent.File))
		if readErr != nil {
			undo()
			return nil, nil, readErr
		}
		rendered, renderErr := renderAgent(content, agent, format)
		if renderErr != nil {
			undo()
			return nil, nil, fmt.Errorf("%s: %w", filepath.Join(AgentsDirName, agent.File), renderErr)
		}
		if err := os.WriteFile(target, rendered, 0o644); err != nil {
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
	// An empty directory would make every path below relative to the process's
	// working directory, so a harness with no agent destination could delete
	// same-named files out of whatever repo bmo happened to be run in.
	if len(files) > 0 && strings.TrimSpace(agentsDir) == "" {
		return errors.New("refusing to remove agents: no agents directory resolved")
	}
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

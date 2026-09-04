package bmo

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"
)

// Harness identifies a coding agent's on-disk skill convention.
type Harness string

const (
	HarnessClaude   Harness = "claude"
	HarnessCodex    Harness = "codex"
	HarnessCursor   Harness = "cursor"
	HarnessGemini   Harness = "gemini"
	HarnessCopilot  Harness = "copilot"
	HarnessWindsurf Harness = "windsurf"
	HarnessOpenCode Harness = "opencode"
	HarnessAmp      Harness = "amp"
	HarnessCline    Harness = "cline"
	HarnessGrok     Harness = "grok"
	HarnessCustom   Harness = "custom"
)

// HarnessInfo describes a built-in installation preset.
type HarnessInfo struct {
	Name        Harness
	Description string
	ProjectDir  string
	GlobalDir   string
	// ProjectAgentsDir and GlobalAgentsDir are the harness's subagent
	// directories, which sit beside its skills directories rather than inside
	// them. Both are empty for a harness with no Markdown subagent convention:
	// Amp defines custom agents as TypeScript plugins, and a harness bmo has
	// not verified must not have a directory guessed for it.
	ProjectAgentsDir string
	GlobalAgentsDir  string
	// AgentNameInFrontmatter is whether this harness resolves a subagent's name
	// from a name: key. Harnesses that derive it from the filename have no such
	// key, so an export omits it rather than writing one they may reject.
	AgentNameInFrontmatter bool
	Executable             string
	DetectionDir           string
	DesktopApps            []string
}

var harnesses = map[Harness]HarnessInfo{
	HarnessClaude: {
		Name: HarnessClaude, Description: "Claude Code and Claude desktop",
		ProjectDir: ".claude/skills", GlobalDir: ".claude/skills",
		ProjectAgentsDir: ".claude/agents", GlobalAgentsDir: ".claude/agents",
		AgentNameInFrontmatter: true,
		Executable:             "claude", DetectionDir: ".claude", DesktopApps: []string{"Claude.app"},
	},
	HarnessCodex: {
		Name: HarnessCodex, Description: "ChatGPT and Codex (alias: chatgpt)",
		ProjectDir: ".agents/skills", GlobalDir: ".agents/skills",
		Executable: "codex", DetectionDir: ".codex", DesktopApps: []string{"ChatGPT.app", "Codex.app"},
	},
	HarnessCursor: {
		Name: HarnessCursor, Description: "Cursor",
		ProjectDir: ".cursor/skills", GlobalDir: ".cursor/skills",
		ProjectAgentsDir: ".cursor/agents", GlobalAgentsDir: ".cursor/agents",
		AgentNameInFrontmatter: true,
		Executable:             "cursor", DetectionDir: ".cursor", DesktopApps: []string{"Cursor.app"},
	},
	HarnessGemini: {
		Name: HarnessGemini, Description: "Gemini CLI",
		ProjectDir: ".gemini/skills", GlobalDir: ".gemini/skills",
		ProjectAgentsDir: ".gemini/agents", GlobalAgentsDir: ".gemini/agents",
		AgentNameInFrontmatter: true,
		Executable:             "gemini", DetectionDir: ".gemini",
	},
	HarnessCopilot: {
		Name: HarnessCopilot, Description: "GitHub Copilot",
		ProjectDir: ".github/skills", GlobalDir: ".copilot/skills",
		Executable: "copilot", DetectionDir: ".copilot",
	},
	HarnessWindsurf: {
		Name: HarnessWindsurf, Description: "Windsurf",
		ProjectDir: ".windsurf/skills", GlobalDir: ".codeium/windsurf/skills",
		Executable: "windsurf", DetectionDir: ".codeium/windsurf", DesktopApps: []string{"Windsurf.app"},
	},
	HarnessOpenCode: {
		Name: HarnessOpenCode, Description: "OpenCode",
		ProjectDir: ".opencode/skills", GlobalDir: ".config/opencode/skills",
		// OpenCode names an agent by its path below agents/, so its schema has
		// no name: key and an export leaves the filename to carry the name.
		ProjectAgentsDir: ".opencode/agents", GlobalAgentsDir: ".config/opencode/agents",
		Executable: "opencode", DetectionDir: ".config/opencode",
	},
	HarnessAmp: {
		Name: HarnessAmp, Description: "Amp",
		ProjectDir: ".agents/skills", GlobalDir: ".config/agents/skills",
		Executable: "amp", DetectionDir: ".config/amp",
	},
	HarnessCline: {
		Name: HarnessCline, Description: "Cline",
		ProjectDir: ".cline/skills", GlobalDir: ".cline/skills",
		Executable: "cline", DetectionDir: ".cline",
	},
	HarnessGrok: {
		Name: HarnessGrok, Description: "Grok Build",
		ProjectDir: ".grok/skills", GlobalDir: ".grok/skills",
		ProjectAgentsDir: ".grok/agents", GlobalAgentsDir: ".grok/agents",
		AgentNameInFrontmatter: true,
		Executable:             "grok", DetectionDir: ".grok",
	},
}

var harnessAliases = map[string]Harness{
	"chatgpt": HarnessCodex,
}

// Harnesses returns the built-in presets in stable name order.
func Harnesses() []HarnessInfo {
	result := make([]HarnessInfo, 0, len(harnesses))
	for _, info := range harnesses {
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// ParseHarness validates a built-in harness name. An empty value preserves
// bmo's original behavior and selects Claude Code.
func ParseHarness(name string) (Harness, error) {
	if name == "" {
		return HarnessClaude, nil
	}
	selector := strings.ToLower(strings.TrimSpace(name))
	if canonical, ok := harnessAliases[selector]; ok {
		return canonical, nil
	}
	harness := Harness(selector)
	if _, ok := harnesses[harness]; !ok {
		return "", fmt.Errorf("unknown harness %q (choose one of: %s, or use --skills-dir)", name, strings.Join(HarnessSelectors(), ", "))
	}
	return harness, nil
}

// HarnessNames returns the accepted built-in harness names.
func HarnessNames() []string {
	infos := Harnesses()
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, string(info.Name))
	}
	return names
}

// HarnessSelectors returns every accepted harness token, including aliases,
// without adding aliases to loops that must visit each physical preset once.
func HarnessSelectors() []string {
	selectors := HarnessNames()
	for alias := range harnessAliases {
		selectors = append(selectors, alias)
	}
	sort.Strings(selectors)
	return selectors
}

type detectionEnvironment struct {
	home            string
	applicationDirs []string
	lookPath        func(string) (string, error)
	getenv          func(string) string
	dirExists       func(string) bool
}

// DetectedHarnesses returns harnesses that appear to be installed on this
// machine. A harness is detected when its CLI is on PATH, its user config
// directory already exists, or a supported macOS desktop app is installed.
// The order is stable and favors Codex for the shared .agents/skills
// destination.
func DetectedHarnesses() []HarnessInfo {
	home, _ := os.UserHomeDir()
	applicationDirs := []string{}
	// The override keeps embedding applications and tests from accidentally
	// detecting app bundles outside the filesystem they intend to inspect.
	if configured, overridden := os.LookupEnv("BMO_APPLICATIONS_DIRS"); overridden {
		for _, dir := range filepath.SplitList(configured) {
			if dir != "" {
				applicationDirs = append(applicationDirs, dir)
			}
		}
	} else if runtime.GOOS == "darwin" {
		applicationDirs = append(applicationDirs, "/Applications")
		if home != "" {
			applicationDirs = append(applicationDirs, filepath.Join(home, "Applications"))
		}
	}
	return detectedHarnesses(detectionEnvironment{
		home:            home,
		applicationDirs: applicationDirs,
		lookPath:        exec.LookPath,
		getenv:          os.Getenv,
		dirExists: func(path string) bool {
			stat, err := os.Stat(path)
			return err == nil && stat.IsDir()
		},
	})
}

func detectedHarnesses(environment detectionEnvironment) []HarnessInfo {
	order := []Harness{
		HarnessCodex, HarnessClaude, HarnessCursor, HarnessGemini,
		HarnessCopilot, HarnessWindsurf, HarnessOpenCode, HarnessAmp, HarnessCline,
		HarnessGrok,
	}
	var detected []HarnessInfo
	for _, harness := range order {
		info := harnesses[harness]
		_, executableErr := environment.lookPath(info.Executable)
		configExists := false
		if environment.home != "" {
			configExists = environment.dirExists(filepath.Join(environment.home, filepath.FromSlash(info.DetectionDir)))
		}
		if harness == HarnessClaude {
			if dir := environment.getenv("CLAUDE_CONFIG_DIR"); dir != "" && environment.dirExists(dir) {
				configExists = true
			}
		}
		if harness == HarnessCodex {
			if dir := environment.getenv("CODEX_HOME"); dir != "" && environment.dirExists(dir) {
				configExists = true
			}
		}
		if harness == HarnessGrok {
			if dir := environment.getenv("GROK_HOME"); dir != "" && environment.dirExists(dir) {
				configExists = true
			}
		}
		desktopExists := false
		for _, applicationsDir := range environment.applicationDirs {
			for _, app := range info.DesktopApps {
				if environment.dirExists(filepath.Join(applicationsDir, app)) {
					desktopExists = true
					break
				}
			}
			if desktopExists {
				break
			}
		}
		if executableErr == nil || configExists || desktopExists {
			detected = append(detected, info)
		}
	}
	return detected
}

// Target is the fully resolved destination for one harness and scope.
// Keeping these paths together prevents an install from writing skill data to
// one harness while recording ownership in another harness's metadata.
type Target struct {
	Harness      Harness
	RequestedAs  string
	Scope        Scope
	SkillsDir    string
	MetadataPath string
	AgentsDir    string
}

// ResolveTarget resolves a built-in harness preset. skillsDirOverride selects
// a custom harness and is interpreted relative to cwd when it is not absolute.
func ResolveTarget(harnessName string, scope Scope, cwd, skillsDirOverride string) (Target, error) {
	if scope != ScopeGlobal && scope != ScopeProject {
		return Target{}, fmt.Errorf("invalid scope: %s", scope)
	}
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return Target{}, err
		}
	}
	if skillsDirOverride != "" {
		if harnessName != "" && harnessName != string(HarnessCustom) {
			return Target{}, errors.New("--skills-dir cannot be combined with --harness")
		}
		dir := skillsDirOverride
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		dir, err := filepath.Abs(dir)
		if err != nil {
			return Target{}, err
		}
		return Target{
			Harness:      HarnessCustom,
			Scope:        scope,
			SkillsDir:    dir,
			MetadataPath: filepath.Join(filepath.Dir(dir), ProjectLockFileName),
		}, nil
	}

	harness, err := ParseHarness(harnessName)
	if err != nil {
		return Target{}, err
	}
	info := harnesses[harness]
	requestedAs := strings.ToLower(strings.TrimSpace(harnessName))
	if requestedAs == "" {
		requestedAs = string(harness)
	}
	var skillsDir string
	if scope == ScopeProject {
		skillsDir = filepath.Join(cwd, filepath.FromSlash(info.ProjectDir))
	} else if harness == HarnessClaude {
		skillsDir, err = ClaudeGlobalSkillsDir()
	} else if harness == HarnessGrok {
		skillsDir, err = GrokGlobalSkillsDir()
	} else {
		var home string
		home, err = os.UserHomeDir()
		if err == nil {
			skillsDir = filepath.Join(home, filepath.FromSlash(info.GlobalDir))
		}
	}
	if err != nil {
		return Target{}, err
	}

	var metadataPath string
	if harness == HarnessClaude {
		if scope == ScopeProject {
			metadataPath = ClaudeProjectMetadataPath(cwd)
		} else {
			metadataPath, err = ClaudeGlobalMetadataPath()
		}
	} else if scope == ScopeProject {
		metadataPath = filepath.Join(filepath.Dir(skillsDir), ProjectLockFileName)
	} else {
		var home string
		home, err = os.UserHomeDir()
		if err == nil {
			metadataPath = filepath.Join(home, ".bmo", string(harness)+"-skills.json")
		}
	}
	if err != nil {
		return Target{}, err
	}

	target := Target{Harness: harness, RequestedAs: requestedAs, Scope: scope, SkillsDir: skillsDir, MetadataPath: metadataPath}
	// A preset with no agents directory has no Markdown subagent convention
	// bmo has verified, so its skills' agents/ folder stays a skill resource
	// rather than being written somewhere the harness will not read.
	if info.ProjectAgentsDir != "" {
		agentsDir, err := resolveAgentsDir(harness, info, scope, cwd)
		// A resolved target with an agent convention but an empty AgentsDir
		// would silently stop supporting agents, so an unresolvable home
		// directory fails outright rather than handing back a half-populated
		// target.
		if err != nil {
			return Target{}, err
		}
		target.AgentsDir = agentsDir
	}
	return target, nil
}

// resolveAgentsDir resolves one preset's subagent directory for a scope. The
// two harnesses whose configuration home moves with an environment variable
// resolve through the same helpers their skills directories use, so agents and
// skills never land in two different homes.
func resolveAgentsDir(harness Harness, info HarnessInfo, scope Scope, cwd string) (string, error) {
	if scope == ScopeProject {
		return filepath.Join(cwd, filepath.FromSlash(info.ProjectAgentsDir)), nil
	}
	switch harness {
	case HarnessClaude:
		return ClaudeGlobalAgentsDir()
	case HarnessGrok:
		return GrokGlobalAgentsDir()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, filepath.FromSlash(info.GlobalAgentsDir)), nil
}

// DisplayHarness returns the user-selected alias when one was used while
// keeping Target.Harness canonical for metadata and destination ownership.
func (t Target) DisplayHarness() string {
	if t.RequestedAs != "" {
		return t.RequestedAs
	}
	if t.Harness == "" {
		return string(HarnessClaude)
	}
	return string(t.Harness)
}

// SupportsAgents reports whether bmo can safely export bundled agent files for
// this target. ResolveTarget populates AgentsDir only for a preset whose
// subagent directory bmo has verified, so the resolved path is the whole
// answer: a target without one must not have subagents recorded against it,
// or metadata would track files that were never installed.
func (t Target) SupportsAgents() bool {
	return t.AgentsDir != ""
}

// AgentFormat returns how this target's harness expects a subagent file to be
// written. A zero-value harness is Claude Code, matching the rest of this
// package, and Claude is the format bmo reads, so its files pass through
// untouched and existing installs stay byte-identical.
func (t Target) AgentFormat() AgentFormat {
	if t.Harness == HarnessClaude || t.Harness == "" {
		return AgentFormat{Verbatim: true, NameInFrontmatter: true}
	}
	return AgentFormat{NameInFrontmatter: harnesses[t.Harness].AgentNameInFrontmatter}
}

// RelaxesSkillValidation reports whether this target accepts skills the
// portable rules would refuse. Only Claude does, and only for backward
// compatibility: it accepted skills published before those rules existed, and
// tightening it now would orphan installs people already depend on. A
// zero-value harness is Claude Code, matching the rest of this package.
//
// The exemption is not free — a skill that only ever installed to Claude can
// be refused the first time it is shared anywhere else — so `bmo doctor`
// reports the skills it is currently covering.
func (t Target) RelaxesSkillValidation() bool {
	return t.Harness == HarnessClaude || t.Harness == ""
}

// ValidateSkillForTarget enforces the portable common denominator for every
// harness that does not relax it.
func ValidateSkillForTarget(skill Skill, target Target) error {
	if target.RelaxesSkillValidation() {
		return nil
	}
	return ValidatePortableSkill(skill)
}

// ValidatePortableSkill applies the rules every harness bmo installs to has to
// agree on, so one skill folder can be copied between them unchanged.
func ValidatePortableSkill(skill Skill) error {
	if skill.DeclaredName == "" {
		return errors.New("portable harnesses require an explicit name in SKILL.md frontmatter")
	}
	if skill.DeclaredName != skill.Name {
		return fmt.Errorf("portable harnesses require the frontmatter name %q to match the installed folder %q", skill.DeclaredName, skill.Name)
	}
	if !PortableSkillNameRE.MatchString(skill.Name) {
		return fmt.Errorf("portable harnesses require the skill name to use lowercase letters and digits separated by single hyphens: %q", skill.Name)
	}
	if utf8.RuneCountInString(skill.Description) > 1024 {
		return errors.New("portable harnesses require the SKILL.md description to be 1024 characters or fewer")
	}
	return nil
}

// InvocationHint returns a concise harness-specific way to invoke a skill.
func (t Target) InvocationHint(name string) string {
	if t.RequestedAs == "chatgpt" {
		return "@" + name
	}
	switch t.Harness {
	case HarnessCodex:
		return "$" + name
	case HarnessWindsurf:
		return "@" + name
	case HarnessClaude, HarnessCursor, HarnessCopilot, HarnessAmp, HarnessGrok:
		return "/" + name
	default:
		return "ask the agent to use " + name
	}
}

// ProjectLockFileName is the per-project metadata file bmo writes beside a
// harness's project skills directory.
const ProjectLockFileName = "bmo-lock.json"

// ProjectConfigDir returns the harness's project configuration directory: the
// parent of its project skills directory, and the folder holding that
// project's lock file. It is the on-disk marker `bmo scout` looks for when
// sweeping a tree for projects bmo has installed into.
func (h HarnessInfo) ProjectConfigDir() string {
	return path.Dir(h.ProjectDir)
}

// ProjectLockRel returns this harness's project metadata file relative to a
// project root, in slash form. ResolveTarget stays authoritative for absolute
// paths; harness_test pins the two together so a preset cannot gain a lock
// file location that a scan would miss.
func (h HarnessInfo) ProjectLockRel() string {
	return path.Join(h.ProjectConfigDir(), ProjectLockFileName)
}

// HarnessPreferenceOrder returns every preset in the order bmo prefers when
// more than one of them resolves to the same destination: Claude first for
// backward-compatible familiarity, then Codex so the shared .agents
// destination is credited to it, then the rest in name order.
func HarnessPreferenceOrder() []Harness {
	order := []Harness{HarnessClaude, HarnessCodex}
	for _, name := range HarnessNames() {
		if harness := Harness(name); harness != HarnessClaude && harness != HarnessCodex {
			order = append(order, harness)
		}
	}
	return order
}

// HarnessesByProjectConfigDir groups the presets by the project configuration
// directory they share, so a single pass over a directory tree can attribute
// one lock file to every harness that writes it (.agents is both Codex's and
// Amp's). Values follow HarnessPreferenceOrder.
func HarnessesByProjectConfigDir() map[string][]HarnessInfo {
	grouped := map[string][]HarnessInfo{}
	for _, harness := range HarnessPreferenceOrder() {
		info := harnesses[harness]
		dir := info.ProjectConfigDir()
		grouped[dir] = append(grouped[dir], info)
	}
	return grouped
}

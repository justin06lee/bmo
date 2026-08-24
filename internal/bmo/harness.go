package bmo

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	HarnessCustom   Harness = "custom"
)

// HarnessInfo describes a built-in installation preset.
type HarnessInfo struct {
	Name         Harness
	Description  string
	ProjectDir   string
	GlobalDir    string
	Executable   string
	DetectionDir string
}

var harnesses = map[Harness]HarnessInfo{
	HarnessClaude:   {HarnessClaude, "Claude Code", ".claude/skills", ".claude/skills", "claude", ".claude"},
	HarnessCodex:    {HarnessCodex, "Codex and cross-harness Agent Skills", ".agents/skills", ".agents/skills", "codex", ".codex"},
	HarnessCursor:   {HarnessCursor, "Cursor", ".cursor/skills", ".cursor/skills", "cursor", ".cursor"},
	HarnessGemini:   {HarnessGemini, "Gemini CLI", ".gemini/skills", ".gemini/skills", "gemini", ".gemini"},
	HarnessCopilot:  {HarnessCopilot, "GitHub Copilot", ".github/skills", ".copilot/skills", "copilot", ".copilot"},
	HarnessWindsurf: {HarnessWindsurf, "Windsurf", ".windsurf/skills", ".codeium/windsurf/skills", "windsurf", ".codeium/windsurf"},
	HarnessOpenCode: {HarnessOpenCode, "OpenCode", ".opencode/skills", ".config/opencode/skills", "opencode", ".config/opencode"},
	HarnessAmp:      {HarnessAmp, "Amp", ".agents/skills", ".config/agents/skills", "amp", ".config/amp"},
	HarnessCline:    {HarnessCline, "Cline", ".cline/skills", ".cline/skills", "cline", ".cline"},
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
	harness := Harness(strings.ToLower(strings.TrimSpace(name)))
	if _, ok := harnesses[harness]; !ok {
		return "", fmt.Errorf("unknown harness %q (choose one of: %s, or use --skills-dir)", name, strings.Join(HarnessNames(), ", "))
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

// DetectedHarnesses returns harnesses that appear to be installed on this
// machine. A harness is detected when its CLI is on PATH or its user config
// directory already exists. The order is stable and favors Codex for the
// shared .agents/skills destination.
func DetectedHarnesses() []HarnessInfo {
	home, _ := os.UserHomeDir()
	order := []Harness{
		HarnessCodex, HarnessClaude, HarnessCursor, HarnessGemini,
		HarnessCopilot, HarnessWindsurf, HarnessOpenCode, HarnessAmp, HarnessCline,
	}
	var detected []HarnessInfo
	for _, harness := range order {
		info := harnesses[harness]
		_, executableErr := exec.LookPath(info.Executable)
		configExists := false
		if home != "" {
			if stat, err := os.Stat(filepath.Join(home, filepath.FromSlash(info.DetectionDir))); err == nil && stat.IsDir() {
				configExists = true
			}
		}
		if harness == HarnessClaude {
			if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
				if stat, err := os.Stat(dir); err == nil && stat.IsDir() {
					configExists = true
				}
			}
		}
		if harness == HarnessCodex {
			if dir := os.Getenv("CODEX_HOME"); dir != "" {
				if stat, err := os.Stat(dir); err == nil && stat.IsDir() {
					configExists = true
				}
			}
		}
		if executableErr == nil || configExists {
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
			MetadataPath: filepath.Join(filepath.Dir(dir), "bmo-lock.json"),
		}, nil
	}

	harness, err := ParseHarness(harnessName)
	if err != nil {
		return Target{}, err
	}
	info := harnesses[harness]
	var skillsDir string
	if scope == ScopeProject {
		skillsDir = filepath.Join(cwd, filepath.FromSlash(info.ProjectDir))
	} else if harness == HarnessClaude {
		skillsDir, err = GlobalSkillsDir()
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
			metadataPath = ProjectMetadataPath(cwd)
		} else {
			metadataPath, err = GlobalMetadataPath()
		}
	} else if scope == ScopeProject {
		metadataPath = filepath.Join(filepath.Dir(skillsDir), "bmo-lock.json")
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

	target := Target{Harness: harness, Scope: scope, SkillsDir: skillsDir, MetadataPath: metadataPath}
	// Claude Code is the only preset whose bundled agent format bmo currently
	// validates. Other harnesses use different schemas, so their agents/ folder
	// remains a skill resource instead of being exported as live configuration.
	if harness == HarnessClaude {
		target.AgentsDir, err = AgentsDir(scope, cwd)
	}
	return target, err
}

// SupportsAgents reports whether bmo can safely export bundled agent files for
// this target.
func (t Target) SupportsAgents() bool {
	return t.Harness == HarnessClaude && t.AgentsDir != ""
}

// ValidateSkillForTarget enforces the portable common denominator for every
// non-Claude harness while retaining Claude's historical extensions.
func ValidateSkillForTarget(skill Skill, target Target) error {
	if target.Harness == HarnessClaude {
		return nil
	}
	if skill.DeclaredName == "" {
		return errors.New("portable harnesses require an explicit name in SKILL.md frontmatter")
	}
	if skill.DeclaredName != skill.Name {
		return fmt.Errorf("portable harnesses require the frontmatter name %q to match the installed folder %q", skill.DeclaredName, skill.Name)
	}
	if utf8.RuneCountInString(skill.Description) > 1024 {
		return errors.New("portable harnesses require the SKILL.md description to be 1024 characters or fewer")
	}
	return nil
}

// InvocationHint returns a concise harness-specific way to invoke a skill.
func (t Target) InvocationHint(name string) string {
	switch t.Harness {
	case HarnessCodex:
		return "$" + name
	case HarnessWindsurf:
		return "@" + name
	case HarnessClaude, HarnessCursor, HarnessCopilot, HarnessAmp:
		return "/" + name
	default:
		return "ask the agent to use " + name
	}
}

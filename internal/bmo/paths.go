package bmo

import (
	"os"
	"path/filepath"
)

type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
)

// ClaudeGlobalSkillsDir returns Claude Code's user-level skills directory.
// Claude is the only preset whose global destination is not derived from
// HarnessInfo.GlobalDir, because CLAUDE_CONFIG_DIR may move it.
func ClaudeGlobalSkillsDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "skills"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "skills"), nil
}

// ClaudeProjectSkillsDir returns Claude Code's project skills directory.
func ClaudeProjectSkillsDir(cwd string) string {
	return filepath.Join(cwd, ".claude", "skills")
}

// GrokGlobalSkillsDir returns the user-level skills directory Grok Build
// scans. Grok resolves its home as $GROK_HOME, else ~/.grok, and unlike its
// subagent lookup it does not also fall back to the literal ~/.grok for
// skills. Installing to a fixed ~/.grok/skills would therefore be invisible to
// anyone who sets GROK_HOME.
func GrokGlobalSkillsDir() (string, error) {
	if dir := os.Getenv("GROK_HOME"); dir != "" {
		return filepath.Join(dir, "skills"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".grok", "skills"), nil
}

// ClaudeGlobalAgentsDir returns the directory Claude Code scans for global
// subagent definitions. It sits beside the skills directory, which is why a
// skill's bundled agents/ folder cannot simply be copied in place with the
// skill. Every other harness derives this from HarnessInfo.GlobalAgentsDir;
// Claude needs its own because CLAUDE_CONFIG_DIR may move it.
func ClaudeGlobalAgentsDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "agents"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "agents"), nil
}

// ClaudeProjectAgentsDir returns Claude Code's project subagent directory.
func ClaudeProjectAgentsDir(cwd string) string {
	return filepath.Join(cwd, ".claude", "agents")
}

// GrokGlobalAgentsDir returns the user-level subagent directory Grok Build
// scans. It follows GROK_HOME for the same reason GrokGlobalSkillsDir does, so
// a relocated Grok home receives a skill and its subagents together.
func GrokGlobalAgentsDir() (string, error) {
	if dir := os.Getenv("GROK_HOME"); dir != "" {
		return filepath.Join(dir, "agents"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".grok", "agents"), nil
}

// ClaudeAgentsDir resolves Claude Code's subagent directory for a scope.
func ClaudeAgentsDir(scope Scope, cwd string) (string, error) {
	if scope == ScopeProject {
		return ClaudeProjectAgentsDir(cwd), nil
	}
	return ClaudeGlobalAgentsDir()
}

// ClaudeGlobalMetadataPath is Claude's global metadata file. Every other
// harness uses ~/.bmo/<harness>-skills.json; this unprefixed path predates the
// harness abstraction and is preserved so existing installs stay tracked.
func ClaudeGlobalMetadataPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bmo", "skills.json"), nil
}

// ClaudeProjectMetadataPath is Claude's project lock file. Every other harness
// writes ProjectLockFileName beside its own project skills directory; this path
// is preserved for the same backward-compatibility reason.
func ClaudeProjectMetadataPath(cwd string) string {
	return filepath.Join(cwd, ".claude", ProjectLockFileName)
}

// BootstrapMarkerPathFor returns the sentinel file that records the one-time
// first-run install of the bundled bmo skill for a harness. Its presence stops
// bmo from re-installing the skill on every invocation (so `bmo remove bmo`
// sticks). The Claude path is kept unchanged so upgrades do not repeat the
// historical first-run install.
func BootstrapMarkerPathFor(harness Harness) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	name := ".bootstrapped-" + string(harness)
	if harness == "" || harness == HarnessClaude {
		name = ".bootstrapped"
	}
	return filepath.Join(home, ".bmo", name), nil
}

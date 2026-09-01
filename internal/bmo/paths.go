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

func GlobalSkillsDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "skills"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "skills"), nil
}

func ProjectSkillsDir(cwd string) string {
	return filepath.Join(cwd, ".claude", "skills")
}

// GlobalAgentsDir returns the directory Claude Code scans for global subagent
// definitions. It sits beside the skills directory, which is why a skill's
// bundled agents/ folder cannot simply be copied in place with the skill.
func GlobalAgentsDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "agents"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "agents"), nil
}

func ProjectAgentsDir(cwd string) string {
	return filepath.Join(cwd, ".claude", "agents")
}

// AgentsDir resolves the subagent directory for a scope.
func AgentsDir(scope Scope, cwd string) (string, error) {
	if scope == ScopeProject {
		return ProjectAgentsDir(cwd), nil
	}
	return GlobalAgentsDir()
}

func GlobalMetadataPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bmo", "skills.json"), nil
}

func ProjectMetadataPath(cwd string) string {
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

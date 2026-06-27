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

func GlobalMetadataPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bmo", "skills.json"), nil
}

func ProjectMetadataPath(cwd string) string {
	return filepath.Join(cwd, ".claude", "bmo-lock.json")
}

// BootstrapMarkerPath returns the sentinel file that records the one-time
// first-run install of the bundled bmo skill. Its presence stops bmo from
// re-installing the skill on every invocation (so `bmo remove bmo` sticks).
func BootstrapMarkerPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bmo", ".bootstrapped"), nil
}

func ScopePaths(scope Scope, cwd string) (skillsDir string, metadataPath string, err error) {
	if scope == ScopeProject {
		return ProjectSkillsDir(cwd), ProjectMetadataPath(cwd), nil
	}
	skillsDir, err = GlobalSkillsDir()
	if err != nil {
		return "", "", err
	}
	metadataPath, err = GlobalMetadataPath()
	return skillsDir, metadataPath, err
}

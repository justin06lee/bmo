package bmo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// projectRegistry is the on-disk shape of ~/.bmo/projects.json: every project
// directory bmo has installed skills into. It exists so `bmo update everywhere`
// can reach project-scope installs without being run inside each repo.
type projectRegistry struct {
	Version  int      `json:"version"`
	Projects []string `json:"projects"`
}

// ProjectRegistryPath returns the file recording every project bmo has
// installed skills into.
func ProjectRegistryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bmo", "projects.json"), nil
}

// RegisteredProjects returns the recorded project directories, sorted. A
// missing registry is an empty list, not an error. Entries are returned even
// if the directory no longer exists — callers decide how to report those.
func RegisteredProjects() ([]string, error) {
	path, err := ProjectRegistryPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var reg projectRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, err
	}
	sort.Strings(reg.Projects)
	return reg.Projects, nil
}

// RecordProject adds a project directory to the registry, absolutized and
// deduplicated. Recording the same project again is a cheap no-op.
func RecordProject(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	projects, err := RegisteredProjects()
	if err != nil {
		return err
	}
	for _, p := range projects {
		if p == abs {
			return nil
		}
	}
	projects = append(projects, abs)
	sort.Strings(projects)
	path, err := ProjectRegistryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(projectRegistry{Version: 1, Projects: projects}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".projects-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

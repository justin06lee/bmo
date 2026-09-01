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
	_, err := RecordProjects([]string{dir})
	return err
}

// RecordProjects adds every directory to the registry in one write and returns
// the ones that were not already recorded, sorted. Batching matters for
// `bmo scout`, which can discover dozens of projects in a single sweep and
// would otherwise rewrite the registry once per hit.
func RecordProjects(dirs []string) ([]string, error) {
	projects, err := RegisteredProjects()
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(projects))
	for _, project := range projects {
		known[project] = true
	}
	var added []string
	for _, dir := range dirs {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, err
		}
		if known[abs] {
			continue
		}
		known[abs] = true
		added = append(added, abs)
		projects = append(projects, abs)
	}
	if len(added) == 0 {
		return nil, nil
	}
	sort.Strings(added)
	sort.Strings(projects)
	if err := writeProjectRegistry(projects); err != nil {
		return nil, err
	}
	return added, nil
}

// PruneProjects drops registry entries whose directory no longer exists,
// returning what it removed. The registry only tells `bmo update everywhere`
// where to look, so a wrongly pruned entry costs a re-run of `bmo scout` and
// never any installed skill. Nothing on disk is deleted.
func PruneProjects() ([]string, error) {
	projects, err := RegisteredProjects()
	if err != nil {
		return nil, err
	}
	var kept, removed []string
	for _, dir := range projects {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			kept = append(kept, dir)
			continue
		}
		removed = append(removed, dir)
	}
	if len(removed) == 0 {
		return nil, nil
	}
	if err := writeProjectRegistry(kept); err != nil {
		return nil, err
	}
	return removed, nil
}

// writeProjectRegistry replaces the registry atomically.
func writeProjectRegistry(projects []string) error {
	path, err := ProjectRegistryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if projects == nil {
		projects = []string{}
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

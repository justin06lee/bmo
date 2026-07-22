package bmo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DoctorStatus string

const (
	DoctorOK      DoctorStatus = "OK"
	DoctorWarning DoctorStatus = "WARNING"
	DoctorError   DoctorStatus = "ERROR"
)

type DoctorCheck struct {
	Status  DoctorStatus
	Message string
}

func RunDoctor(cwd string) []DoctorCheck {
	var checks []DoctorCheck
	globalSkills, err := GlobalSkillsDir()
	if err != nil {
		checks = append(checks, DoctorCheck{DoctorError, fmt.Sprintf("Global skills dir: %v", err)})
	} else {
		checks = append(checks, checkWritableDir("Global skills dir", globalSkills))
	}
	checks = append(checks, checkWritableDir("Project skills dir", ProjectSkillsDir(cwd)))
	globalMeta, err := GlobalMetadataPath()
	if err != nil {
		checks = append(checks, DoctorCheck{DoctorError, fmt.Sprintf("Metadata: %v", err)})
	} else {
		checks = append(checks, checkMetadata("Metadata", globalMeta))
		checks = append(checks, checkMetadataEntries(globalMeta)...)
	}
	projectMeta := ProjectMetadataPath(cwd)
	checks = append(checks, checkMetadata("Project metadata", projectMeta))
	checks = append(checks, checkMetadataEntries(projectMeta)...)
	checks = append(checks, checkDuplicates(cwd)...)
	checks = append(checks, checkAgents(cwd)...)
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		checks = append(checks, DoctorCheck{DoctorOK, "CLAUDE_CONFIG_DIR is set"})
	}
	return checks
}

func checkWritableDir(label, dir string) DoctorCheck {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return DoctorCheck{DoctorOK, fmt.Sprintf("%s does not exist yet (created on first install): %s", label, dir)}
	} else if err != nil {
		return DoctorCheck{DoctorError, fmt.Sprintf("%s: %v", label, err)}
	}
	tmp, err := os.CreateTemp(dir, ".bmo-write-*")
	if err != nil {
		return DoctorCheck{DoctorError, fmt.Sprintf("%s is not writable: %v", label, err)}
	}
	name := tmp.Name()
	tmp.Close()
	os.Remove(name)
	return DoctorCheck{DoctorOK, fmt.Sprintf("%s: %s", label, dir)}
}

func checkMetadata(label, path string) DoctorCheck {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return DoctorCheck{DoctorOK, fmt.Sprintf("%s does not exist yet: %s", label, path)}
	}
	if _, err := ReadMetadata(path); err != nil {
		return DoctorCheck{DoctorError, fmt.Sprintf("%s is invalid JSON: %v", label, err)}
	}
	return DoctorCheck{DoctorOK, fmt.Sprintf("%s: %s", label, path)}
}

func checkMetadataEntries(path string) []DoctorCheck {
	meta, err := ReadMetadata(path)
	if err != nil {
		return nil
	}
	var checks []DoctorCheck
	valid := 0
	for name, entry := range meta.Skills {
		if _, err := os.Stat(entry.InstalledPath); err != nil {
			checks = append(checks, DoctorCheck{DoctorWarning, fmt.Sprintf("Skill %s metadata points to missing path: %s", name, entry.InstalledPath)})
			continue
		}
		if _, err := os.Stat(filepath.Join(entry.InstalledPath, "SKILL.md")); err != nil {
			checks = append(checks, DoctorCheck{DoctorWarning, fmt.Sprintf("Skill %s is missing SKILL.md", name)})
			continue
		}
		valid++
	}
	if len(meta.Skills) > 0 {
		checks = append(checks, DoctorCheck{DoctorOK, fmt.Sprintf("%d tracked skills are valid", valid)})
	}
	return checks
}

// checkAgents verifies that every subagent bmo installed on a skill's behalf is
// still on disk, and that no two skills in the same scope claim the same
// subagent file — a collision means one skill's specialist silently answers for
// the other.
func checkAgents(cwd string) []DoctorCheck {
	var checks []DoctorCheck
	globalMeta, err := GlobalMetadataPath()
	if err != nil {
		return nil
	}
	globalAgents, err := GlobalAgentsDir()
	if err != nil {
		return nil
	}
	scopes := []struct {
		label     string
		metaPath  string
		agentsDir string
	}{
		{"global", globalMeta, globalAgents},
		{"project", ProjectMetadataPath(cwd), ProjectAgentsDir(cwd)},
	}
	for _, scope := range scopes {
		meta, err := ReadMetadata(scope.metaPath)
		if err != nil {
			continue
		}
		owners := map[string][]string{}
		installed := 0
		for name, entry := range meta.Skills {
			for _, file := range entry.Agents {
				owners[file] = append(owners[file], name)
				if _, err := os.Stat(filepath.Join(scope.agentsDir, file)); err != nil {
					checks = append(checks, DoctorCheck{DoctorWarning, fmt.Sprintf(
						"Skill %s is missing its installed subagent (%s scope): %s", name, scope.label, file)})
					continue
				}
				installed++
			}
		}
		for file, names := range owners {
			if len(names) > 1 {
				sort.Strings(names)
				checks = append(checks, DoctorCheck{DoctorWarning, fmt.Sprintf(
					"Subagent %s is claimed by more than one %s skill: %s", file, scope.label, strings.Join(names, ", "))})
			}
		}
		if installed > 0 {
			checks = append(checks, DoctorCheck{DoctorOK, fmt.Sprintf(
				"%d installed subagents are present (%s scope): %s", installed, scope.label, scope.agentsDir)})
		}
	}
	return checks
}

func checkDuplicates(cwd string) []DoctorCheck {
	globalMetaPath, err := GlobalMetadataPath()
	if err != nil {
		return nil
	}
	globalMeta, _ := ReadMetadata(globalMetaPath)
	projectMeta, _ := ReadMetadata(ProjectMetadataPath(cwd))
	var checks []DoctorCheck
	for name := range projectMeta.Skills {
		if _, ok := globalMeta.Skills[name]; ok {
			checks = append(checks, DoctorCheck{DoctorWarning, fmt.Sprintf("Duplicate skill name in project and global metadata: %s", name)})
		}
	}
	return checks
}

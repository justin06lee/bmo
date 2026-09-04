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

// RunDoctorForHarness checks both global and project destinations for a
// built-in harness. Doctor exists to diagnose broken environments, so an
// unresolvable destination (for example, no home directory) becomes an ERROR
// check while every other diagnostic still runs.
func RunDoctorForHarness(cwd, harnessName string) ([]DoctorCheck, error) {
	harness, err := ParseHarness(harnessName)
	if err != nil {
		return nil, err
	}
	var checks []DoctorCheck
	global, globalErr := ResolveTarget(string(harness), ScopeGlobal, cwd, "")
	if globalErr != nil {
		checks = append(checks, DoctorCheck{DoctorError, fmt.Sprintf("Global destination (%s): %v", harness, globalErr)})
	} else {
		checks = append(checks, scopeChecks("Global", global)...)
	}
	project, projectErr := ResolveTarget(string(harness), ScopeProject, cwd, "")
	if projectErr != nil {
		checks = append(checks, DoctorCheck{DoctorError, fmt.Sprintf("Project destination (%s): %v", harness, projectErr)})
	} else {
		checks = append(checks, scopeChecks("Project", project)...)
	}
	if globalErr == nil && projectErr == nil {
		checks = append(checks, checkDuplicatesForTargets(global, project)...)
	}
	checks = append(checks, checkProjectRegistry()...)
	checks = append(checks, checkHarnessHomeOverride(harness)...)
	return checks, nil
}

// RunDoctorForHarnessScope checks a single scope of a built-in harness, for
// `bmo doctor here` / `bmo doctor --project` style invocations.
func RunDoctorForHarnessScope(cwd, harnessName string, scope Scope) ([]DoctorCheck, error) {
	harness, err := ParseHarness(harnessName)
	if err != nil {
		return nil, err
	}
	label := "Project"
	if scope == ScopeGlobal {
		label = "Global"
	}
	target, resolveErr := ResolveTarget(string(harness), scope, cwd, "")
	if resolveErr != nil {
		return []DoctorCheck{{DoctorError, fmt.Sprintf("%s destination (%s): %v", label, harness, resolveErr)}}, nil
	}
	checks := scopeChecks(label, target)
	if scope == ScopeGlobal {
		checks = append(checks, checkProjectRegistry()...)
	}
	checks = append(checks, checkHarnessHomeOverride(harness)...)
	return checks, nil
}

// checkHarnessHomeOverride reports the environment variable a harness uses to
// relocate its configuration home, when that variable is set. It is the
// difference between the destination bmo resolved and the one the user
// expected, so doctor names it rather than leaving the path unexplained.
func checkHarnessHomeOverride(harness Harness) []DoctorCheck {
	variable := ""
	switch harness {
	case HarnessClaude:
		variable = "CLAUDE_CONFIG_DIR"
	case HarnessGrok:
		variable = "GROK_HOME"
	}
	if variable == "" || os.Getenv(variable) == "" {
		return nil
	}
	return []DoctorCheck{{DoctorOK, variable + " is set"}}
}

// scopeChecks runs the per-destination diagnostics for one resolved target.
func scopeChecks(label string, target Target) []DoctorCheck {
	checks := []DoctorCheck{
		checkWritableDir(label+" skills dir ("+string(target.Harness)+")", target.SkillsDir),
		checkMetadata(label+" metadata", target.MetadataPath),
	}
	checks = append(checks, checkMetadataEntries(target.MetadataPath)...)
	checks = append(checks, checkPortability(target)...)
	checks = append(checks, checkAgentsForTargets(target)...)
	return checks
}

// RunDoctorForTarget checks one explicit destination, including custom skill
// directories that do not have a built-in harness preset.
func RunDoctorForTarget(target Target) []DoctorCheck {
	checks := []DoctorCheck{
		checkWritableDir("Skills dir ("+string(target.Harness)+")", target.SkillsDir),
		checkMetadata("Metadata", target.MetadataPath),
	}
	checks = append(checks, checkMetadataEntries(target.MetadataPath)...)
	checks = append(checks, checkPortability(target)...)
	checks = append(checks, checkAgentsForTargets(target)...)
	return checks
}

// checkPortability reports skills a relaxed target accepted that no other
// harness would. The install succeeded and the skill works where it is, but
// `bmo share` skips exactly these, so naming them here turns a silent
// difference into something the user runs into later without explanation.
func checkPortability(target Target) []DoctorCheck {
	if !target.RelaxesSkillValidation() {
		return nil
	}
	meta, err := ReadMetadata(target.MetadataPath)
	if err != nil {
		return nil
	}
	var names []string
	reasons := map[string]string{}
	for name, entry := range meta.Skills {
		skill, err := ValidateSkill(entry.InstalledPath, "")
		if err != nil {
			// A skill that will not load at all is already reported by
			// checkMetadataEntries; do not say it twice in different words.
			continue
		}
		if err := ValidatePortableSkill(skill); err != nil {
			names = append(names, name)
			reasons[name] = err.Error()
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	checks := make([]DoctorCheck, 0, len(names))
	for _, name := range names {
		checks = append(checks, DoctorCheck{DoctorWarning, fmt.Sprintf(
			"Skill %s works in %s but no other harness will accept it (%s scope): %s",
			name, target.DisplayHarness(), target.Scope, reasons[name])})
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

// checkAgentsForTargets verifies that every subagent bmo installed on a skill's
// behalf is still on disk, and that no two skills in the same scope claim the
// same subagent file — a collision means one skill's specialist silently
// answers for the other. Targets with no agent destination are skipped.
func checkAgentsForTargets(targets ...Target) []DoctorCheck {
	var checks []DoctorCheck
	for _, target := range targets {
		if !target.SupportsAgents() {
			// Metadata claiming subagents here is the state `bmo remove`
			// refuses; surface it so doctor can diagnose that refusal.
			checks = append(checks, checkOrphanedAgentMetadata(target)...)
			continue
		}
		meta, err := ReadMetadata(target.MetadataPath)
		if err != nil {
			continue
		}
		owners := map[string][]string{}
		installed := 0
		for name, entry := range meta.Skills {
			for _, file := range entry.Agents {
				owners[file] = append(owners[file], name)
				if _, err := os.Stat(filepath.Join(target.AgentsDir, file)); err != nil {
					checks = append(checks, DoctorCheck{DoctorWarning, fmt.Sprintf(
						"Skill %s is missing its installed subagent (%s scope): %s", name, target.Scope, file)})
					continue
				}
				installed++
			}
		}
		for file, names := range owners {
			if len(names) > 1 {
				sort.Strings(names)
				checks = append(checks, DoctorCheck{DoctorWarning, fmt.Sprintf(
					"Subagent %s is claimed by more than one %s skill: %s", file, target.Scope, strings.Join(names, ", "))})
			}
		}
		if installed > 0 {
			checks = append(checks, DoctorCheck{DoctorOK, fmt.Sprintf(
				"%d installed subagents are present (%s scope): %s", installed, target.Scope, target.AgentsDir)})
		}
	}
	return checks
}

// checkOrphanedAgentMetadata warns when a harness with no agent destination
// nevertheless tracks subagent files — usually metadata written by hand or by
// an older bmo. `bmo remove` refuses such entries, so the fix is named here.
func checkOrphanedAgentMetadata(target Target) []DoctorCheck {
	meta, err := ReadMetadata(target.MetadataPath)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(meta.Skills))
	for name, entry := range meta.Skills {
		if len(entry.Agents) > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var checks []DoctorCheck
	for _, name := range names {
		checks = append(checks, DoctorCheck{DoctorWarning, fmt.Sprintf(
			"Skill %s tracks subagents but harness %s has no agent destination; edit %s to drop its agents list before removing",
			name, target.Harness, target.MetadataPath)})
	}
	return checks
}

func checkDuplicatesForTargets(global, project Target) []DoctorCheck {
	globalMeta, _ := ReadMetadata(global.MetadataPath)
	projectMeta, _ := ReadMetadata(project.MetadataPath)
	return checkDuplicateMetadata(globalMeta, projectMeta)
}

func checkDuplicateMetadata(globalMeta, projectMeta Metadata) []DoctorCheck {
	var checks []DoctorCheck
	for name := range projectMeta.Skills {
		if _, ok := globalMeta.Skills[name]; ok {
			checks = append(checks, DoctorCheck{DoctorWarning, fmt.Sprintf("Duplicate skill name in project and global metadata: %s", name)})
		}
	}
	return checks
}

// checkProjectRegistry reports the projects recorded for `bmo update
// everywhere`, warning about entries whose directory no longer exists.
func checkProjectRegistry() []DoctorCheck {
	projects, err := RegisteredProjects()
	if err != nil {
		return []DoctorCheck{{DoctorError, fmt.Sprintf("Project registry: %v", err)}}
	}
	if len(projects) == 0 {
		// Not a fault: a machine with only global installs has nothing to
		// register. Naming the discovery command here is what turns an empty
		// `bmo update everywhere` into something the user can act on.
		return []DoctorCheck{{DoctorOK, "No registered projects yet; run `bmo scout` to find project installs"}}
	}
	var checks []DoctorCheck
	live := 0
	for _, dir := range projects {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			checks = append(checks, DoctorCheck{DoctorWarning, fmt.Sprintf("Registered project no longer exists: %s", dir)})
			continue
		}
		live++
	}
	checks = append(checks, DoctorCheck{DoctorOK, fmt.Sprintf("%d registered projects for `bmo update everywhere`", live)})
	return checks
}

package bmo

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Skill struct {
	Path            string
	Name            string
	Description     string
	FileCount       int
	NotableFiles    []string
	ExecutableFiles []string
	Agents          []Agent
	// IgnoreRules counts the .bmoignore patterns that shaped FileCount, so the
	// install preview can say why a large repository installs as few files.
	IgnoreRules int
	Warnings    []string
}

type Frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

var SkillNameRE = regexp.MustCompile(`^[a-z0-9-]+$`)

var ignoredDirs = map[string]bool{
	".git":         true,
	".venv":        true,
	"node_modules": true,
	"__pycache__":  true,
}

var executableExts = map[string]bool{
	".py": true, ".sh": true, ".bash": true, ".zsh": true, ".js": true, ".ts": true,
	".mjs": true, ".cjs": true, ".rb": true, ".go": true, ".rs": true, ".php": true,
	".pl": true, ".ps1": true,
}

var notableNames = map[string]bool{
	"requirements.txt": true,
	"pyproject.toml":   true,
	"package.json":     true,
	"Cargo.toml":       true,
	"Makefile":         true,
	"Dockerfile":       true,
}

func DiscoverSkills(root string) ([]Skill, error) {
	if _, err := os.Stat(filepath.Join(root, "SKILL.md")); err == nil {
		skill, err := ValidateSkill(root, "")
		if err != nil {
			return []Skill{{Path: root, Warnings: []string{err.Error()}}}, nil
		}
		return []Skill{skill}, nil
	}
	// A repository-level .bmoignore keeps discovery away from fixture or
	// example skills that are not meant to be installable.
	ignore, err := LoadIgnore(root)
	if err != nil {
		return nil, err
	}
	var skills []Skill
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			if path != root && ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			if path != root && ignore.Match(rel, true) && ignore.CanPrune() {
				return filepath.SkipDir
			}
			return nil
		}
		if ignore.Match(rel, false) {
			return nil
		}
		if d.Name() == "SKILL.md" {
			dir := filepath.Dir(path)
			skill, err := ValidateSkill(dir, "")
			if err != nil {
				skills = append(skills, Skill{Path: dir, Warnings: []string{err.Error()}})
				return nil
			}
			skills = append(skills, skill)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Path < skills[j].Path })
	return skills, nil
}

func ValidateSkill(dir, nameOverride string) (Skill, error) {
	content, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Skill{}, errors.New("SKILL.md is missing or unreadable")
	}
	fm, err := parseFrontmatter(content)
	if err != nil {
		return Skill{}, err
	}
	if strings.TrimSpace(fm.Description) == "" {
		return Skill{}, errors.New("SKILL.md frontmatter must include a non-empty description")
	}
	if fm.Name != "" {
		if err := ValidateSkillName(fm.Name); err != nil {
			return Skill{}, fmt.Errorf("invalid frontmatter name: %w", err)
		}
	}
	name := nameOverride
	if name == "" {
		name = fm.Name
	}
	if name == "" {
		name = NormalizeSkillName(filepath.Base(dir))
	}
	if err := ValidateSkillName(name); err != nil {
		return Skill{}, err
	}
	skill := Skill{Path: dir, Name: name, Description: fm.Description}
	if len(fm.Description) > 1024 {
		skill.Warnings = append(skill.Warnings, "description is longer than 1024 characters")
	}
	ignore, err := LoadIgnore(dir)
	if err != nil {
		return Skill{}, err
	}
	skill.IgnoreRules = ignore.Len()
	fileCount, notable, executable, err := scanSkillFiles(dir)
	if err != nil {
		return Skill{}, err
	}
	skill.FileCount = fileCount
	skill.NotableFiles = notable
	skill.ExecutableFiles = executable
	if len(executable) > 0 {
		skill.Warnings = append(skill.Warnings, "Skills may include executable code. Review third-party skills before use.")
	}
	// A malformed agent file fails validation rather than being skipped: a
	// subagent that never resolves is a silent hole in the skill's behavior.
	agents, err := DiscoverAgents(dir)
	if err != nil {
		return Skill{}, err
	}
	skill.Agents = agents
	return skill, nil
}

func parseFrontmatter(content []byte) (Frontmatter, error) {
	content = bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})
	if !bytes.HasPrefix(content, []byte("---\n")) && !bytes.HasPrefix(content, []byte("---\r\n")) {
		return Frontmatter{}, errors.New("SKILL.md must start with YAML frontmatter")
	}
	lines := bytes.Split(content, []byte("\n"))
	var body [][]byte
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(string(lines[i])) == "---" {
			body = lines[1:i]
			var fm Frontmatter
			if err := yaml.Unmarshal(bytes.Join(body, []byte("\n")), &fm); err != nil {
				return Frontmatter{}, fmt.Errorf("invalid YAML frontmatter: %w", err)
			}
			return fm, nil
		}
	}
	return Frontmatter{}, errors.New("SKILL.md frontmatter is not closed")
}

func ValidateSkillName(name string) error {
	if name == "" {
		return errors.New("skill name is required")
	}
	if len(name) > 64 {
		return errors.New("skill name must be 64 characters or fewer")
	}
	if !SkillNameRE.MatchString(name) {
		return errors.New("skill name must match ^[a-z0-9-]+$")
	}
	return nil
}

func NormalizeSkillName(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func scanSkillFiles(dir string) (int, []string, []string, error) {
	count := 0
	var notable []string
	var executable []string
	ignore, err := LoadIgnore(dir)
	if err != nil {
		return 0, nil, nil, err
	}
	err = walkIgnored(dir, ignore, func(path, rel string, d fs.DirEntry) error {
		count++
		if executableExts[strings.ToLower(filepath.Ext(d.Name()))] {
			executable = append(executable, rel)
		}
		if notableNames[d.Name()] {
			notable = append(notable, rel)
			if !slices.Contains(executable, rel) {
				executable = append(executable, rel)
			}
		}
		return nil
	})
	sort.Strings(notable)
	sort.Strings(executable)
	return count, notable, executable, err
}

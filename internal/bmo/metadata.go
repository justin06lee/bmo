package bmo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Metadata struct {
	Version int                  `json:"version"`
	Skills  map[string]SkillMeta `json:"skills"`
}

type SkillMeta struct {
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	Scope         Scope   `json:"scope"`
	Harness       Harness `json:"harness,omitempty"`
	Source        string  `json:"source"`
	InstalledPath string  `json:"installed_path"`
	InstalledAt   string  `json:"installed_at"`
	UpdatedAt     string  `json:"updated_at"`
	SourceRef     string  `json:"source_ref,omitempty"`
	SourceType    string  `json:"source_type"`
	// Agents lists the subagent filenames this skill installed into the
	// scope's agents directory. Tracking them by name is what makes removal
	// exact: bmo deletes only the files it wrote.
	Agents []string `json:"agents,omitempty"`
}

func EmptyMetadata() Metadata {
	return Metadata{Version: 1, Skills: map[string]SkillMeta{}}
}

func ReadMetadata(path string) (Metadata, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return EmptyMetadata(), nil
	}
	if err != nil {
		return Metadata{}, err
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return Metadata{}, err
	}
	if meta.Version == 0 {
		meta.Version = 1
	}
	if meta.Skills == nil {
		meta.Skills = map[string]SkillMeta{}
	}
	return meta, nil
}

func WriteMetadata(path string, meta Metadata) error {
	if meta.Version == 0 {
		meta.Version = 1
	}
	if meta.Skills == nil {
		meta.Skills = map[string]SkillMeta{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".skills-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
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

// NewSkillMetaForTarget records the harness alongside the original metadata.
// The omitempty field keeps old Claude metadata readable and diff-friendly.
func NewSkillMetaForTarget(skill Skill, target Target, source Source, installedPath string, existing *SkillMeta) SkillMeta {
	now := time.Now().UTC().Format(time.RFC3339)
	installedAt := now
	if existing != nil && existing.InstalledAt != "" {
		installedAt = existing.InstalledAt
	}
	// Record agents exactly when the install writes them (SupportsAgents), so
	// metadata never tracks subagents that were never installed.
	var agents []string
	if target.SupportsAgents() {
		agents = AgentFiles(skill.Agents)
	}
	return SkillMeta{
		Name:          skill.Name,
		Description:   skill.Description,
		Agents:        agents,
		Scope:         target.Scope,
		Harness:       target.Harness,
		Source:        source.Raw,
		InstalledPath: installedPath,
		InstalledAt:   installedAt,
		UpdatedAt:     now,
		SourceRef:     source.Ref,
		SourceType:    string(source.Type),
	}
}

// SourceFromMeta reconstructs the source a tracked skill came from. Copying a
// skill between harnesses must carry its original provenance across, or the
// destination's `bmo update` would start pulling from the local copy it was
// seeded with instead of the upstream it really came from.
func SourceFromMeta(entry SkillMeta) Source {
	return Source{Raw: entry.Source, Type: SourceType(entry.SourceType), Ref: entry.SourceRef}
}

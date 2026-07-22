package bmo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAgent drops a subagent definition into a skill's agents/ folder.
func writeAgent(t *testing.T, skillDir, file, body string) {
	t.Helper()
	dir := filepath.Join(skillDir, AgentsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func agentDoc(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\nmodel: sonnet\n---\nYou are a specialist.\n"
}

func TestDiscoverAgentsReadsTopLevelMarkdown(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "demo")
	writeAgent(t, dir, "beta.md", agentDoc("beta", "Second specialist."))
	writeAgent(t, dir, "alpha.md", agentDoc("alpha", "First specialist."))
	writeAgent(t, dir, "notes.txt", "not an agent")
	if err := os.MkdirAll(filepath.Join(dir, AgentsDirName, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, AgentsDirName, "nested", "deep.md"), []byte(agentDoc("deep", "Nested.")), 0o644); err != nil {
		t.Fatal(err)
	}
	agents, err := DiscoverAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := AgentNames(agents)
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("expected [alpha beta], got %v", got)
	}
}

func TestDiscoverAgentsWithoutFolder(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "demo")
	agents, err := DiscoverAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 0 {
		t.Fatalf("expected no agents, got %v", agents)
	}
}

func TestDiscoverAgentsFallsBackToFilename(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "demo")
	writeAgent(t, dir, "Code Reviewer.md", "---\ndescription: Reviews code.\n---\nBody\n")
	agents, err := DiscoverAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Name != "code-reviewer" {
		t.Fatalf("expected derived name code-reviewer, got %v", agents)
	}
	if agents[0].File != "Code Reviewer.md" {
		t.Fatalf("expected the shipped filename to be preserved, got %q", agents[0].File)
	}
}

func TestValidateSkillRejectsMalformedAgent(t *testing.T) {
	cases := map[string]string{
		"no frontmatter":  "You are a specialist.\n",
		"no description":  "---\nname: alpha\n---\nBody\n",
		"bad agent name":  "---\nname: Alpha_One\ndescription: Bad name.\n---\nBody\n",
		"unclosed matter": "---\nname: alpha\ndescription: Unclosed.\nBody\n",
	}
	for label, body := range cases {
		t.Run(label, func(t *testing.T) {
			dir := t.TempDir()
			writeSkill(t, dir, "demo")
			writeAgent(t, dir, "alpha.md", body)
			if _, err := ValidateSkill(dir, ""); err == nil {
				t.Fatal("expected validation to fail")
			}
		})
	}
}

func TestInstallPlacesAgentsInAgentsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	writeAgent(t, srcDir, "alpha.md", agentDoc("alpha", "First specialist."))
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	agentsDir, err := AgentsDir(ScopeGlobal, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "alpha.md")); err != nil {
		t.Fatalf("expected agent installed to the agents dir: %v", err)
	}
	if len(meta.Agents) != 1 || meta.Agents[0] != "alpha.md" {
		t.Fatalf("expected the agent tracked in metadata, got %v", meta.Agents)
	}
	// The agents folder still ships inside the skill, so content hashing and
	// update detection see the same tree the author published.
	if _, err := os.Stat(filepath.Join(meta.InstalledPath, AgentsDirName, "alpha.md")); err != nil {
		t.Fatalf("expected agents/ to remain inside the skill: %v", err)
	}
}

func TestInstallScopesAgentsToProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	writeAgent(t, srcDir, "alpha.md", agentDoc("alpha", "First specialist."))
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkill(InstallOptions{Scope: ScopeProject, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ProjectAgentsDir(cwd), "alpha.md")); err != nil {
		t.Fatalf("expected project-scoped agent: %v", err)
	}
	globalAgents, err := GlobalAgentsDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(globalAgents, "alpha.md")); !os.IsNotExist(err) {
		t.Fatal("project install must not write to the global agents dir")
	}
}

func TestInstallRefusesForeignAgentWithoutForce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	agentsDir, err := AgentsDir(ScopeGlobal, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A subagent the user wrote by hand, or one another skill installed.
	if err := os.WriteFile(filepath.Join(agentsDir, "alpha.md"), []byte("hand written\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	writeAgent(t, srcDir, "alpha.md", agentDoc("alpha", "First specialist."))
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	opts := InstallOptions{Scope: ScopeGlobal, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd}
	_, err = InstallSkill(opts)
	if err == nil || !strings.Contains(err.Error(), "alpha.md") {
		t.Fatalf("expected a conflict error naming the agent, got %v", err)
	}
	// The skill must not be half-installed after a refused conflict.
	skillsDir, _, err := ScopePaths(ScopeGlobal, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "demo")); !os.IsNotExist(err) {
		t.Fatal("expected no skill directory after a refused install")
	}
	data, err := os.ReadFile(filepath.Join(agentsDir, "alpha.md"))
	if err != nil || string(data) != "hand written\n" {
		t.Fatalf("expected the existing agent untouched, got %q / %v", data, err)
	}
	opts.Force = true
	if _, err := InstallSkill(opts); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(agentsDir, "alpha.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "First specialist.") {
		t.Fatalf("expected --force to replace the agent, got %q", data)
	}
}

func TestReinstallOwnAgentsIsNotAConflict(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	writeAgent(t, srcDir, "alpha.md", agentDoc("alpha", "First specialist."))
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	opts := InstallOptions{Scope: ScopeGlobal, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd}
	if _, err := InstallSkill(opts); err != nil {
		t.Fatal(err)
	}
	// --force covers the skill directory; the skill's own agent must not add a
	// second reason to refuse.
	opts.Force = true
	if _, err := InstallSkill(opts); err != nil {
		t.Fatalf("reinstalling a skill over its own agents should succeed: %v", err)
	}
}

func TestUpdateDropsAgentsTheSkillNoLongerShips(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	writeAgent(t, srcDir, "alpha.md", agentDoc("alpha", "First specialist."))
	writeAgent(t, srcDir, "beta.md", agentDoc("beta", "Second specialist."))
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(srcDir, AgentsDirName, "beta.md")); err != nil {
		t.Fatal(err)
	}
	skill, err = ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Force: true, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	agentsDir, err := AgentsDir(ScopeGlobal, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "beta.md")); !os.IsNotExist(err) {
		t.Fatal("expected the dropped agent to be removed on update")
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "alpha.md")); err != nil {
		t.Fatalf("expected the retained agent to survive: %v", err)
	}
	if len(meta.Agents) != 1 || meta.Agents[0] != "alpha.md" {
		t.Fatalf("expected metadata to track only alpha.md, got %v", meta.Agents)
	}
}

func TestRemoveSkillRemovesItsAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	writeAgent(t, srcDir, "alpha.md", agentDoc("alpha", "First specialist."))
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd}); err != nil {
		t.Fatal(err)
	}
	agentsDir, err := AgentsDir(ScopeGlobal, cwd)
	if err != nil {
		t.Fatal(err)
	}
	// A subagent bmo did not install must survive the removal.
	if err := os.WriteFile(filepath.Join(agentsDir, "unrelated.md"), []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveSkill("demo", ScopeGlobal, cwd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "alpha.md")); !os.IsNotExist(err) {
		t.Fatal("expected the skill's agent to be removed")
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "unrelated.md")); err != nil {
		t.Fatalf("expected unrelated agents untouched: %v", err)
	}
}

func TestDryRunInstallsNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	writeAgent(t, srcDir, "alpha.md", agentDoc("alpha", "First specialist."))
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, DryRun: true, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Agents) != 1 {
		t.Fatalf("dry run should still report the agents it would install, got %v", meta.Agents)
	}
	agentsDir, err := AgentsDir(ScopeGlobal, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "alpha.md")); !os.IsNotExist(err) {
		t.Fatal("dry run must not write agent files")
	}
}

func TestDoctorFlagsMissingAndDuplicateAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	cwd := t.TempDir()
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "demo")
	writeAgent(t, srcDir, "alpha.md", agentDoc("alpha", "First specialist."))
	skill, err := ValidateSkill(srcDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Source: Source{Raw: "./demo", Type: SourceLocal}, Skill: skill, CWD: cwd}); err != nil {
		t.Fatal(err)
	}
	if healthy := doctorMessages(RunDoctor(cwd), DoctorWarning); len(healthy) > 0 {
		t.Fatalf("expected no warnings for a clean install, got %v", healthy)
	}
	agentsDir, err := AgentsDir(ScopeGlobal, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(agentsDir, "alpha.md")); err != nil {
		t.Fatal(err)
	}
	warnings := strings.Join(doctorMessages(RunDoctor(cwd), DoctorWarning), "\n")
	if !strings.Contains(warnings, "missing its installed subagent") {
		t.Fatalf("expected a missing-subagent warning, got %v", warnings)
	}

	// Two skills claiming one subagent file: whichever installed last wins,
	// which is exactly the ambiguity doctor should surface.
	metaPath, err := GlobalMetadataPath()
	if err != nil {
		t.Fatal(err)
	}
	meta, err := ReadMetadata(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	other := meta.Skills["demo"]
	other.Name = "other"
	meta.Skills["other"] = other
	if err := WriteMetadata(metaPath, meta); err != nil {
		t.Fatal(err)
	}
	warnings = strings.Join(doctorMessages(RunDoctor(cwd), DoctorWarning), "\n")
	if !strings.Contains(warnings, "claimed by more than one") {
		t.Fatalf("expected a duplicate-subagent warning, got %v", warnings)
	}
}

func doctorMessages(checks []DoctorCheck, status DoctorStatus) []string {
	var out []string
	for _, check := range checks {
		if check.Status == status {
			out = append(out, check.Message)
		}
	}
	return out
}

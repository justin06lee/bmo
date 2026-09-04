package bmo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarnessTargetsResolveDocumentedPaths(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	cases := []struct {
		name          string
		projectSuffix string
		globalSuffix  string
	}{
		{"claude", ".claude/skills", ".claude/skills"},
		{"codex", ".agents/skills", ".agents/skills"},
		{"cursor", ".cursor/skills", ".cursor/skills"},
		{"gemini", ".gemini/skills", ".gemini/skills"},
		{"copilot", ".github/skills", ".copilot/skills"},
		{"windsurf", ".windsurf/skills", ".codeium/windsurf/skills"},
		{"opencode", ".opencode/skills", ".config/opencode/skills"},
		{"amp", ".agents/skills", ".config/agents/skills"},
		{"cline", ".cline/skills", ".cline/skills"},
		{"grok", ".grok/skills", ".grok/skills"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectTarget, err := ResolveTarget(tc.name, ScopeProject, project, "")
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(project, filepath.FromSlash(tc.projectSuffix)); projectTarget.SkillsDir != want {
				t.Fatalf("project skills dir = %q, want %q", projectTarget.SkillsDir, want)
			}
			globalTarget, err := ResolveTarget(tc.name, ScopeGlobal, project, "")
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(home, filepath.FromSlash(tc.globalSuffix)); globalTarget.SkillsDir != want {
				t.Fatalf("global skills dir = %q, want %q", globalTarget.SkillsDir, want)
			}
		})
	}
}

func TestResolveCustomTarget(t *testing.T) {
	project := t.TempDir()
	target, err := ResolveTarget("", ScopeProject, project, "tools/my-agent/skills")
	if err != nil {
		t.Fatal(err)
	}
	if target.Harness != HarnessCustom {
		t.Fatalf("harness = %q, want custom", target.Harness)
	}
	wantDir := filepath.Join(project, "tools", "my-agent", "skills")
	if target.SkillsDir != wantDir {
		t.Fatalf("skills dir = %q, want %q", target.SkillsDir, wantDir)
	}
	if target.MetadataPath != filepath.Join(project, "tools", "my-agent", "bmo-lock.json") {
		t.Fatalf("unexpected metadata path: %q", target.MetadataPath)
	}
	if _, err := ResolveTarget("codex", ScopeProject, project, wantDir); err == nil {
		t.Fatal("expected --harness with --skills-dir to fail")
	}
}

func TestChatGPTAliasResolvesToCodexTarget(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)

	harness, err := ParseHarness("ChatGPT")
	if err != nil {
		t.Fatal(err)
	}
	if harness != HarnessCodex {
		t.Fatalf("ParseHarness(chatgpt) = %q, want %q", harness, HarnessCodex)
	}

	target, err := ResolveTarget("chatgpt", ScopeGlobal, project, "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Harness != HarnessCodex {
		t.Fatalf("chatgpt target harness = %q, want canonical %q", target.Harness, HarnessCodex)
	}
	if want := filepath.Join(home, ".agents", "skills"); target.SkillsDir != want {
		t.Fatalf("chatgpt skills dir = %q, want %q", target.SkillsDir, want)
	}
}

func TestDetectedHarnessesUsesExecutablesAndConfigDirectories(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".gemini"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	detected := detectedHarnesses(detectionEnvironment{
		home:            home,
		applicationDirs: []string{t.TempDir()},
		lookPath: func(name string) (string, error) {
			if name == "codex" {
				return filepath.Join(bin, name), nil
			}
			return "", errors.New("not found")
		},
		getenv: func(string) string { return "" },
		dirExists: func(path string) bool {
			stat, err := os.Stat(path)
			return err == nil && stat.IsDir()
		},
	})
	if len(detected) != 2 || detected[0].Name != HarnessCodex || detected[1].Name != HarnessGemini {
		t.Fatalf("detected harnesses = %+v, want codex then gemini", detected)
	}
}

func TestDetectedHarnessesTreatsChatGPTDesktopAsCodex(t *testing.T) {
	applications := t.TempDir()
	if err := os.Mkdir(filepath.Join(applications, "ChatGPT.app"), 0o755); err != nil {
		t.Fatal(err)
	}

	detected := detectedHarnesses(detectionEnvironment{
		home:            t.TempDir(),
		applicationDirs: []string{applications},
		lookPath:        func(string) (string, error) { return "", errors.New("not found") },
		getenv:          func(string) string { return "" },
		dirExists: func(path string) bool {
			stat, err := os.Stat(path)
			return err == nil && stat.IsDir()
		},
	})
	if len(detected) != 1 || detected[0].Name != HarnessCodex {
		t.Fatalf("detected harnesses = %+v, want desktop ChatGPT to select codex", detected)
	}
}

func TestInstallForCodexDoesNotExportClaudeAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	skillDir := filepath.Join(t.TempDir(), "portable")
	if err := os.MkdirAll(filepath.Join(skillDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: portable\ndescription: Portable test skill.\n---\n\nUse this skill.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "agents", "worker.md"), []byte("---\nname: worker\ndescription: Worker.\n---\n\nWork.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, err := ValidateSkill(skillDir, "")
	if err != nil {
		t.Fatal(err)
	}
	target, err := ResolveTarget("codex", ScopeGlobal, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := InstallSkill(InstallOptions{
		Scope: ScopeGlobal, Target: target, CWD: t.TempDir(),
		Source: Source{Raw: skillDir, Type: SourceLocal}, Skill: skill,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Agents) != 0 {
		t.Fatalf("Codex metadata should not claim Claude agents: %v", meta.Agents)
	}
	if meta.Harness != HarnessCodex {
		t.Fatalf("metadata harness = %q, want codex", meta.Harness)
	}
	if !strings.Contains(meta.InstalledPath, filepath.Join(".agents", "skills", "portable")) {
		t.Fatalf("installed path is not Codex-compatible: %s", meta.InstalledPath)
	}
	if _, err := os.Stat(filepath.Join(meta.InstalledPath, "agents", "worker.md")); err != nil {
		t.Fatalf("bundled agent should remain available as a skill resource: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "agents", "worker.md")); !os.IsNotExist(err) {
		t.Fatalf("Claude agent definition was unexpectedly exported: %v", err)
	}
}

// Agent support follows the resolved destination, which only ResolveTarget
// hands out and only for a preset whose subagent directory bmo has verified.
func TestSupportsAgentsFollowsResolvedDestination(t *testing.T) {
	agentsDir := filepath.Join(t.TempDir(), "agents")
	cases := []struct {
		name   string
		target Target
		want   bool
	}{
		{"claude", Target{Harness: HarnessClaude, AgentsDir: agentsDir}, true},
		{"zero harness is historical claude", Target{AgentsDir: agentsDir}, true},
		{"grok", Target{Harness: HarnessGrok, AgentsDir: agentsDir}, true},
		{"claude without destination", Target{Harness: HarnessClaude}, false},
		{"zero target", Target{}, false},
		{"codex has no verified agent convention", Target{Harness: HarnessCodex}, false},
		{"custom", Target{Harness: HarnessCustom}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.target.SupportsAgents(); got != tc.want {
				t.Fatalf("SupportsAgents() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A zero-harness target records subagents in metadata, so it has to install
// them too; disagreeing would track subagents that were never written.
func TestSupportsAgentsMatchesMetadataAgentTracking(t *testing.T) {
	skill := Skill{Name: "demo", Agents: []Agent{{File: "worker.md", Name: "worker"}}}
	agentsDir := filepath.Join(t.TempDir(), "agents")
	for _, target := range []Target{
		{Scope: ScopeGlobal, AgentsDir: agentsDir},
		{Harness: HarnessClaude, Scope: ScopeGlobal, AgentsDir: agentsDir},
		// Claude-shaped targets with no agent destination install nothing,
		// so they must record nothing either.
		{Scope: ScopeGlobal},
		{Harness: HarnessClaude, Scope: ScopeGlobal},
		{Harness: HarnessCodex, Scope: ScopeGlobal},
	} {
		meta := NewSkillMetaForTarget(skill, target, Source{}, "/tmp/demo", nil)
		if len(meta.Agents) > 0 && !target.SupportsAgents() {
			t.Fatalf("target %+v tracks agents it would never install", target)
		}
		if len(meta.Agents) == 0 && target.SupportsAgents() {
			t.Fatalf("target %+v installs agents it never records", target)
		}
	}
}

func TestResolveTargetAgentDestinations(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	for _, scope := range []Scope{ScopeGlobal, ScopeProject} {
		target, err := ResolveTarget("claude", scope, project, "")
		if err != nil {
			t.Fatal(err)
		}
		if target.AgentsDir == "" || !target.SupportsAgents() {
			t.Fatalf("resolved claude target (%s) must support agents: %+v", scope, target)
		}
	}
	// Every preset resolves the destination it declares, and only that one. A
	// preset that declares none must resolve nothing rather than inheriting
	// another harness's directory.
	for _, info := range Harnesses() {
		for _, scope := range []Scope{ScopeGlobal, ScopeProject} {
			target, err := ResolveTarget(string(info.Name), scope, project, "")
			if err != nil {
				t.Fatal(err)
			}
			if info.ProjectAgentsDir == "" {
				if target.AgentsDir != "" || target.SupportsAgents() {
					t.Fatalf("%s declares no agent convention but resolved %q", info.Name, target.AgentsDir)
				}
				continue
			}
			if !target.SupportsAgents() {
				t.Fatalf("%s (%s) declares an agent convention but resolved none", info.Name, scope)
			}
			want := filepath.Join(home, filepath.FromSlash(info.GlobalAgentsDir))
			if scope == ScopeProject {
				want = filepath.Join(project, filepath.FromSlash(info.ProjectAgentsDir))
			}
			if target.AgentsDir != want {
				t.Fatalf("%s (%s) agents dir = %q, want %q", info.Name, scope, target.AgentsDir, want)
			}
			// Agents live beside skills, never inside them: a skills directory
			// that also held agent files would install each agent twice.
			if strings.HasPrefix(target.AgentsDir, target.SkillsDir+string(filepath.Separator)) {
				t.Fatalf("%s (%s) agents dir %q is inside its skills dir", info.Name, scope, target.AgentsDir)
			}
		}
	}
	custom, err := ResolveTarget("", ScopeProject, project, "tools/skills")
	if err != nil {
		t.Fatal(err)
	}
	if custom.AgentsDir != "" || custom.SupportsAgents() {
		t.Fatalf("custom target must not resolve an agents destination: %+v", custom)
	}
}

// Without a home directory there is no valid global destination, so resolution
// must fail rather than hand back a target with empty paths.
func TestResolveTargetFailsClosedWithoutHomeDirectory(t *testing.T) {
	project := t.TempDir()
	t.Setenv("HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	for _, name := range HarnessNames() {
		target, err := ResolveTarget(name, ScopeGlobal, project, "")
		if err == nil {
			t.Fatalf("%s: expected an error without a home directory, got %+v", name, target)
		}
		if target != (Target{}) {
			t.Fatalf("%s: expected a zero target on error, got %+v", name, target)
		}
	}
}

func TestPortableHarnessRequiresDeclaredMatchingName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	target, err := ResolveTarget("codex", ScopeGlobal, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}

	missing := filepath.Join(t.TempDir(), "missing-name")
	if err := os.MkdirAll(missing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missing, "SKILL.md"), []byte("---\ndescription: Missing declared name.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, err := ValidateSkill(missing, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Target: target, Source: Source{Raw: missing, Type: SourceLocal}, Skill: skill}); err == nil || !strings.Contains(err.Error(), "explicit name") {
		t.Fatalf("missing portable name error = %v", err)
	}

	declared := filepath.Join(t.TempDir(), "declared")
	if err := os.MkdirAll(declared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(declared, "SKILL.md"), []byte("---\nname: declared\ndescription: Declared name.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, err = ValidateSkill(declared, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkill(InstallOptions{Scope: ScopeGlobal, Target: target, Name: "renamed", Source: Source{Raw: declared, Type: SourceLocal}, Skill: skill}); err == nil || !strings.Contains(err.Error(), "match the installed folder") {
		t.Fatalf("renamed portable skill error = %v", err)
	}
}

func TestInvocationHintPerHarness(t *testing.T) {
	// The hint is printed after every install, so a wrong prefix teaches the
	// user an invocation their harness does not understand.
	cases := map[Harness]string{
		HarnessClaude:   "/demo",
		HarnessCursor:   "/demo",
		HarnessCopilot:  "/demo",
		HarnessAmp:      "/demo",
		HarnessGrok:     "/demo",
		HarnessCodex:    "$demo",
		HarnessWindsurf: "@demo",
	}
	for harness, want := range cases {
		if got := (Target{Harness: harness}).InvocationHint("demo"); got != want {
			t.Errorf("InvocationHint(%s) = %q, want %q", harness, got, want)
		}
	}
	for _, harness := range []Harness{HarnessGemini, HarnessOpenCode, HarnessCline, HarnessCustom, ""} {
		got := (Target{Harness: harness}).InvocationHint("demo")
		if !strings.Contains(got, "demo") {
			t.Errorf("InvocationHint(%q) = %q, want it to name the skill", harness, got)
		}
	}
}

// TestProjectLockRelMatchesResolveTarget pins the relative lock path a scan
// looks for to the absolute one an install writes. A preset that gained a lock
// file location ResolveTarget alone knew about would install skills `bmo scout`
// could never find again.
func TestProjectLockRelMatchesResolveTarget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	for _, info := range Harnesses() {
		target, err := ResolveTarget(string(info.Name), ScopeProject, project, "")
		if err != nil {
			t.Fatalf("ResolveTarget(%s): %v", info.Name, err)
		}
		want := filepath.Join(project, filepath.FromSlash(info.ProjectLockRel()))
		if target.MetadataPath != want {
			t.Fatalf("%s project metadata = %s, want %s", info.Name, target.MetadataPath, want)
		}
		if got := filepath.Join(project, filepath.FromSlash(info.ProjectConfigDir())); got != filepath.Dir(target.SkillsDir) {
			t.Fatalf("%s config dir = %s, want %s", info.Name, got, filepath.Dir(target.SkillsDir))
		}
	}
}

// TestHarnessGroupingsCoverEveryPreset keeps the scan grouping and the
// preference order complete: a new preset must appear in both, or fan-out
// commands would silently skip it.
func TestHarnessGroupingsCoverEveryPreset(t *testing.T) {
	order := HarnessPreferenceOrder()
	if len(order) != len(Harnesses()) {
		t.Fatalf("preference order covers %d presets, want %d", len(order), len(Harnesses()))
	}
	if order[0] != HarnessClaude || order[1] != HarnessCodex {
		t.Fatalf("preference order = %v, want claude then codex first", order)
	}
	grouped := HarnessesByProjectConfigDir()
	seen := map[Harness]bool{}
	for dir, infos := range grouped {
		if strings.Contains(dir, "/") {
			t.Fatalf("project config dir %q must be a single path segment for a directory scan to match it", dir)
		}
		for _, info := range infos {
			if seen[info.Name] {
				t.Fatalf("%s appears in more than one grouping", info.Name)
			}
			seen[info.Name] = true
		}
	}
	for _, info := range Harnesses() {
		if !seen[info.Name] {
			t.Fatalf("%s is missing from the project config dir grouping", info.Name)
		}
	}
	if agents := grouped[".agents"]; len(agents) != 2 || agents[0].Name != HarnessCodex {
		t.Fatalf(".agents grouping = %v, want codex first then amp", agents)
	}
}

// TestDetectionOrderCoversEveryPreset keeps `everyone` complete. The detection
// sweep walks its own hard-coded order rather than deriving one from the
// preset map, so a harness added to the map alone would resolve fine by name
// and still never be detected.
func TestDetectionOrderCoversEveryPreset(t *testing.T) {
	home := t.TempDir()
	for _, info := range Harnesses() {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(info.DetectionDir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	detected := detectedHarnesses(detectionEnvironment{
		home:            home,
		applicationDirs: []string{t.TempDir()},
		lookPath:        func(string) (string, error) { return "", errors.New("not found") },
		getenv:          func(string) string { return "" },
		dirExists: func(path string) bool {
			stat, err := os.Stat(path)
			return err == nil && stat.IsDir()
		},
	})
	seen := map[Harness]bool{}
	for _, info := range detected {
		seen[info.Name] = true
	}
	for _, info := range Harnesses() {
		if !seen[info.Name] {
			t.Errorf("%s has a preset but is missing from the detection order", info.Name)
		}
	}
}

// TestGrokGlobalTargetFollowsGrokHome pins bmo's global Grok destination to the
// home Grok Build itself resolves. Grok reads user skills from $GROK_HOME/skills
// with no fallback to the literal ~/.grok, so writing there regardless would
// install skills Grok never loads.
func TestGrokGlobalTargetFollowsGrokHome(t *testing.T) {
	grokHome := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GROK_HOME", grokHome)

	target, err := ResolveTarget("grok", ScopeGlobal, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(grokHome, "skills"); target.SkillsDir != want {
		t.Fatalf("global skills dir = %q, want %q", target.SkillsDir, want)
	}

	t.Setenv("GROK_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	target, err = ResolveTarget("grok", ScopeGlobal, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".grok", "skills"); target.SkillsDir != want {
		t.Fatalf("global skills dir without GROK_HOME = %q, want %q", target.SkillsDir, want)
	}
}

// TestGrokDetectedFromGrokHome covers the relocated-home case for `everyone`:
// a machine whose Grok config lives outside ~/.grok must still be detected.
func TestGrokDetectedFromGrokHome(t *testing.T) {
	grokHome := t.TempDir()
	detected := detectedHarnesses(detectionEnvironment{
		home:            t.TempDir(),
		applicationDirs: []string{t.TempDir()},
		lookPath:        func(string) (string, error) { return "", errors.New("not found") },
		getenv: func(name string) string {
			if name == "GROK_HOME" {
				return grokHome
			}
			return ""
		},
		dirExists: func(path string) bool {
			stat, err := os.Stat(path)
			return err == nil && stat.IsDir()
		},
	})
	if len(detected) != 1 || detected[0].Name != HarnessGrok {
		t.Fatalf("detected harnesses = %+v, want grok from GROK_HOME", detected)
	}
}

// The end-to-end shape of a non-Claude agent export: the file lands in that
// harness's own agents directory, translated, and metadata tracks it so remove
// and doctor can find it again.
func TestInstallExportsAgentsToNonClaudeHarness(t *testing.T) {
	home := t.TempDir()
	grokHome := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GROK_HOME", grokHome)

	skillDir := filepath.Join(t.TempDir(), "portable")
	if err := os.MkdirAll(filepath.Join(skillDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: portable\ndescription: Portable test skill.\n---\n\nUse this skill.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "agents", "worker.md"),
		[]byte("---\nname: worker\ndescription: Worker.\nmodel: sonnet\ntools: Read, Grep\n---\n\nWork carefully.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, err := ValidateSkill(skillDir, "")
	if err != nil {
		t.Fatal(err)
	}
	target, err := ResolveTarget("grok", ScopeGlobal, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := InstallSkill(InstallOptions{
		Scope: ScopeGlobal, Target: target, CWD: t.TempDir(),
		Source: Source{Raw: skillDir, Type: SourceLocal}, Skill: skill,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Agents) != 1 || meta.Agents[0] != "worker.md" {
		t.Fatalf("grok metadata should track the exported agent: %v", meta.Agents)
	}
	// GROK_HOME moves the agents directory exactly as it moves the skills one,
	// so a relocated home does not get its skills and subagents split apart.
	exported := filepath.Join(grokHome, "agents", "worker.md")
	written, err := os.ReadFile(exported)
	if err != nil {
		t.Fatalf("agent was not exported to the grok agents dir: %v", err)
	}
	if strings.Contains(string(written), "sonnet") || strings.Contains(string(written), "Read, Grep") {
		t.Fatalf("export carried Claude-only keys into grok:\n%s", written)
	}
	if !strings.Contains(string(written), "Work carefully.") {
		t.Fatalf("export lost the prompt body:\n%s", written)
	}
	// The skill's own agents/ folder is still copied as a resource, so the
	// untranslated original stays available beside the skill.
	if _, err := os.Stat(filepath.Join(meta.InstalledPath, "agents", "worker.md")); err != nil {
		t.Fatalf("bundled agent should remain a skill resource: %v", err)
	}
}

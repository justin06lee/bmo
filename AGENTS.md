# Repository guidance

## Purpose

`bmo` is a Go CLI for installing portable `SKILL.md` Agent Skills from local directories, GitHub repositories, or zip URLs. Claude Code remains the default harness for backward compatibility, but new behavior must work through the harness abstraction in `internal/bmo/harness.go`.

## Build and test

- Run `gofmt` on changed Go files.
- Run `go vet ./...` and `go test ./...` before handing off changes.
- Use `make build` to produce the local `./bmo` binary.
- Do not edit the checked-in `bmo` binary as source; it is a build artifact.

## Architecture

- `internal/bmo/` owns source resolution, validation, installation, metadata, paths, harness presets, project discovery (`scout.go`, `registry.go`), and diagnostics.
- `internal/cli/` owns Cobra commands and terminal presentation.
- `skills/bmo/SKILL.md` is embedded into the binary by `main.go`; keep it synchronized with CLI behavior.
- A zero-value harness in internal APIs must preserve historical Claude behavior for backward compatibility.

## Compatibility invariants

- Keep the `SKILL.md` format harness-neutral and compatible with the Agent Skills open standard.
- Add harness-specific paths in one place: `internal/bmo/harness.go`.
- A harness only gets an agent destination once its Markdown subagent directory is verified against that harness's own documentation or source; never guess one.
- Claude receives bundled `agents/*.md` byte-for-byte. Every other harness receives frontmatter rebuilt from the keys it understands, with the prompt body preserved; a key that does not port is reported to the user rather than dropped silently, because losing a `tools:` allowlist widens what the subagent may do.
- Keep metadata aligned with its resolved destination; presets sharing one `.agents/skills` directory intentionally share that project install.
- Preserve `CLAUDE_CONFIG_DIR`, `~/.bmo/skills.json`, and `.claude/bmo-lock.json` behavior for existing Claude users.
- For an unrecognized harness, maintain the `--skills-dir` escape hatch rather than guessing its filesystem convention.
- A new harness preset must appear in `HarnessPreferenceOrder`, `HarnessesByProjectConfigDir`, and the detection order in `detectedHarnesses`, or fan-out commands, `bmo scout`, and `everyone` will silently skip it.
- A harness that relocates its configuration home through an environment variable resolves its global destination through that variable, not a fixed `~` path, so bmo does not install where the harness will not look.
- A fan-out command resolves its destinations through `sweepLocations` and `everywhereHarnesses`, so a location keyword cannot come to mean different things on different commands. `universe` is the machine-wide sweep; `everywhere` is the global destination, except on `update` and `share`, which predate the word and translate it with `sweepEverything`.
- Only `remove`, `update`, and `share` accept `universe`. A command that resolves a single destination must reject it rather than quietly treat it as global.
- A behavior that applies to only one harness is either a documented backward-compatibility carve-out or a bug. Claude's carve-outs are its default when no harness is named, its relaxed skill validation, `CLAUDE_CONFIG_DIR`, `~/.bmo/skills.json`, and `.claude/bmo-lock.json`; each is surfaced to the user rather than left silent. A helper implementing one is named for the harness it serves, so a generic name never hides harness-specific behavior.
- A removal never deletes a path outside the target's resolved skills directory. When metadata records a path that has moved outside it, the copy named inside the resolved directory is removed instead, and an entry with no copy left is untracked rather than refused.
- `bmo share` stays purely additive: it never replaces, renames, or deletes an installed skill, and it never promotes a project skill into a global destination.
- A skill copied between harnesses keeps its donor's recorded source, so `bmo update` in the new destination still resolves the real upstream.

## Code review rules

- Flag path handling that could delete outside the resolved skills directory.
- Flag installs that execute downloaded content; bmo must remain copy-only.
- Flag new source extraction paths that bypass size caps, zip-slip checks, symlink refusal, or `.bmoignore`.
- Require tests for every new harness path, metadata location, and scope behavior.
- Flag a filesystem sweep that follows symlinks, descends into dependency or build trees, or aborts on one unreadable directory.

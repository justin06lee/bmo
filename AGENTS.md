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
- Never export Claude-format `agents/*.md` files into another harness's live agent directory without a validated format adapter.
- Keep metadata aligned with its resolved destination; presets sharing one `.agents/skills` directory intentionally share that project install.
- Preserve `CLAUDE_CONFIG_DIR`, `~/.bmo/skills.json`, and `.claude/bmo-lock.json` behavior for existing Claude users.
- For an unrecognized harness, maintain the `--skills-dir` escape hatch rather than guessing its filesystem convention.
- A new harness preset must appear in `HarnessPreferenceOrder` and `HarnessesByProjectConfigDir`, or fan-out commands and `bmo scout` will silently skip it.
- A fan-out command resolves its destinations through `sweepLocations` and `everywhereHarnesses`, so a location keyword cannot come to mean different things on different commands. `universe` is the machine-wide sweep; `everywhere` is the global destination, except on `update` and `share`, which predate the word and translate it with `sweepEverything`.
- Only `remove`, `update`, and `share` accept `universe`. A command that resolves a single destination must reject it rather than quietly treat it as global.
- A removal never deletes a path outside the target's resolved skills directory. When metadata records a path that has moved outside it, the copy named inside the resolved directory is removed instead, and an entry with no copy left is untracked rather than refused.
- `bmo share` stays purely additive: it never replaces, renames, or deletes an installed skill, and it never promotes a project skill into a global destination.
- A skill copied between harnesses keeps its donor's recorded source, so `bmo update` in the new destination still resolves the real upstream.

## Code review rules

- Flag path handling that could delete outside the resolved skills directory.
- Flag installs that execute downloaded content; bmo must remain copy-only.
- Flag new source extraction paths that bypass size caps, zip-slip checks, symlink refusal, or `.bmoignore`.
- Require tests for every new harness path, metadata location, and scope behavior.
- Flag a filesystem sweep that follows symlinks, descends into dependency or build trees, or aborts on one unreadable directory.

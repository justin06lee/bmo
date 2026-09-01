<div align="center">

<img src="assets/bmo.svg" alt="bmo" width="460" />

# bmo

**A tiny installer for portable coding-agent skills.**<br>
*No marketplaces. No plugin wrappers. No manual cloning.*

</div>

---

```bash
bmo add owner/repo
bmo inspect owner/repo
bmo list
bmo update
bmo remove skill-name
bmo doctor
bmo upgrade    # upgrade bmo itself to the latest release

bmo add owner/repo here         # install into this project
bmo add owner/repo everywhere   # install globally (the default)

bmo add owner/repo codex          # Codex + ChatGPT via portable .agents/skills
bmo add owner/repo chatgpt        # alias for the same shared destination
bmo add owner/repo gemini         # Gemini CLI's native directory
bmo add owner/repo everyone       # every detected coding harness
bmo add owner/repo --skills-dir PATH  # any other coding harness

bmo init       # install the skill bmo ships with
bmo add bmo    # ...or restore it if you deleted it
```

## What It Does

`bmo` installs standalone [Agent Skills](https://agentskills.io). A skill is a folder containing a `SKILL.md` file. Built-in presets support ChatGPT/Codex, Claude Code, Cursor, Gemini CLI, GitHub Copilot, Windsurf, OpenCode, Amp, and Cline; `--skills-dir` supports any other harness that reads the same open format.

It resolves a source (GitHub repo, local path, or zip URL), finds installable skill folders, validates the `SKILL.md` frontmatter, copies the selected folder into the target harness's skills directory, and records metadata so the skill can be listed, updated, or removed later.

A skill can also bundle **Claude Code subagents** in an `agents/` folder. With the `claude` preset, bmo installs and tracks them separately — see [Subagents](#subagents). Other harnesses use different agent schemas, so bmo leaves that folder inside the installed skill as a resource instead of writing incompatible live configuration.

When the skill folder doubles as a working repository, a [`.bmoignore`](#bmoignore) file keeps tests, CI config, and demo assets out of the install.

## The bundled `bmo` skill

`bmo` ships with its own portable Agent Skill baked into the binary (the
`skills/bmo/` folder, embedded at build time). It teaches the active harness both
how to drive `bmo` — installing, inspecting, listing, updating, and removing
skills in plain language — and how to author skills to `bmo`'s contract (the
folder layout, `SKILL.md` frontmatter rules, naming constraints, and repo
structure), so anything the agent creates is immediately installable with
`bmo add`.

You don't have to install it by hand. **The first time you run any `bmo`
command, bmo installs the `bmo` skill globally for the selected harness** (once) and prints a one-line
note. Per-harness sentinels under `~/.bmo/` record that this happened, so it
won't fight you if you later remove it.

You can also manage it explicitly:

```bash
bmo init       # install (or refresh) the bundled bmo skill
bmo init --harness codex
bmo add bmo    # the same thing — restore it if you deleted the folder
bmo add self   # alias for `bmo add bmo`
```

Because the skill is embedded in the binary, `bmo init` / `bmo add bmo` work
**fully offline** — no GitHub clone, no network. `bmo add bmo` is the answer to
"I accidentally deleted the skill, how do I get it back?"

## What It Is Not

`bmo` is **not** a marketplace, package registry, dependency manager, plugin installer, publisher, signing system, or background service.

It does **not** execute downloaded code, run install hooks, or install dependencies. A skill that ships `requirements.txt` or `package.json` still needs you to install those yourself — bmo only flags their presence.

---

## Install

**From source:**

```bash
go install github.com/justin06lee/bmo@latest
```

**Or build locally:**

```bash
git clone https://github.com/justin06lee/bmo
cd bmo
make        # build and install onto your PATH
```

`make build` produces the `./bmo` binary only; `make update` refreshes an
already-installed copy; `make test` runs vet and the test suite.

---

## Quick Start

```bash
# Inspect a skill before installing
bmo inspect owner/repo

# Install a skill globally
bmo add owner/repo

# Install every skill a repo ships
bmo add owner/repo --all

# Install to the current project instead
bmo add --project owner/repo

# Install for Codex and ChatGPT. Their .agents/skills path is also read by several harnesses.
bmo add owner/repo codex

# The ChatGPT spelling selects the same destination and metadata.
bmo add owner/repo chatgpt

# Install into every coding harness detected on this machine
bmo add owner/repo everyone

# See every native preset, or target an arbitrary skills directory
bmo harnesses
bmo add --skills-dir ./path/used/by/my-agent owner/repo

# List installed skills
bmo list

# Update every installed skill that changed at its source
bmo update

# Remove a skill
bmo remove skill-name

# Run diagnostics
bmo doctor

# (Re)install the skill bmo ships with — also runs automatically on first use
bmo init
```

---

## Commands

### `add`

Install a skill from a source.

```bash
bmo add [here|everywhere] SOURCE [HARNESS|everyone] [--all] [--project | --global] [--name NAME] [--force] [--yes] [--dry-run]
```

| Flag | Description |
|------|-------------|
| `--all` | Install every skill the source contains, instead of picking one |
| `--project` | Install to the current harness's project directory instead of globally |
| `--global` | Install to the harness's global directory (the default) |
| `--harness` | Backward-compatible alias for the positional harness name |
| `--skills-dir` | Target an explicit directory for any other Agent Skills-compatible harness |
| `--name` | Override the skill name for legacy Claude installs (portable harnesses require it to match the declared frontmatter name); cannot be combined with `--all` |
| `--force` | Overwrite an existing installation with the same name, or a subagent bmo doesn't own |
| `--yes` | Skip all confirmation prompts |
| `--dry-run` | Show what would be installed without writing anything; first-run bootstrapping is suppressed |

**Installing a whole suite.** When a repository ships a set of skills meant to be used together, `--all` installs them in one command:

```bash
bmo add owner/repo --all              # every skill in the repo
bmo add owner/repo/skills --all       # every skill under skills/
```

Each skill is tracked individually, so `update` and `remove` still work per skill. The batch is validated before anything is written — if any skill fails validation, any name is claimed twice, any skill is already installed (without `--force`), or two skills ship the same subagent file, nothing is installed and the reason is reported. That check is what keeps a failed run from leaving half a suite behind.

Duplicate names are refused rather than resolved, since installing whichever folder came last would silently pick for you. Narrow the source to a subpath (`owner/repo/skills`) or install the odd one out separately with `--name`.

**Source formats:**

| Format | Example |
|--------|---------|
| GitHub repo | `owner/repo` or `github:owner/repo` |
| GitHub with subpath | `owner/repo/path/to/skill` |
| GitHub with ref | `owner/repo@v1.0.0` |
| GitHub with subpath + ref | `owner/repo/path@branch` |
| Local directory | `./path/to/skill` |
| Zip URL | `https://example.com/skill.zip` |
| The bundled bmo skill | `bmo` (or `self`) — installs the embedded skill, offline |

The `github:` prefix is optional — a bare `owner/repo` is treated as GitHub. Local relative paths must use a `./` (or `../`) prefix so they aren't mistaken for a repo; absolute paths and `~/` paths (expanded by bmo even when quoted) work too.

When no ref is specified, bmo tries `main` first, then falls back to `master`.

The source, harness, and location keywords may appear in any order. Regular
flags can be mixed in as usual:

```bash
bmo add owner/repo codex here
bmo add owner/repo here gemini
bmo add everywhere owner/repo everyone --all --yes
bmo add here owner/repo everyone
```

`everyone` detects harnesses whose CLI is on `PATH`, whose user configuration directory already exists, or whose supported macOS desktop app is installed. ChatGPT.app and Codex.app both select the canonical `codex` destination. It preflights every destination, asks once, and installs only to detected harnesses. Destinations shared by multiple harnesses—such as the project-level `.agents/skills` used by Codex and Amp—are written once. If a destination fails partway through, the copies already written are rolled back so a plain retry works without `--force`.

### `init`

Install the `bmo` skill that ships bundled inside the binary.

```bash
bmo init [--project | --global] [--harness NAME | --skills-dir PATH]
```

| Flag | Description |
|------|-------------|
| `--project` | Install into the target harness's project directory instead of globally |
| `--global` | Install into the target harness's global directory (the default) |
| `--harness` | Target a built-in harness preset (default: `claude`) |
| `--skills-dir` | Target an explicit skills directory |

This is the explicit form of the first-run auto-install. It works offline and
refreshes the skill if it's already installed. `bmo add bmo` does the same thing.

### `inspect`

Preview a skill source without installing.

```bash
bmo inspect SOURCE
```

Shows the skill name, description, file count (after `.bmoignore` is applied), how many ignore rules ran, the subagents it would install, notable files, and any executable file warnings. Useful for vetting a skill before running `add`.

### `list`

List installed skills.

```bash
bmo list [here|everywhere] [HARNESS] [--project | --global] [--harness NAME | --skills-dir PATH] [--json]
```

| Flag | Description |
|------|-------------|
| `--project` | Show only project-installed skills |
| `--global` | Show only globally-installed skills |
| `--harness` | Read metadata for a built-in harness preset (default: `claude`) |
| `--skills-dir` | Read metadata beside an explicit skills directory |
| `--json` | Output as JSON for programmatic use |

By default (no flags), lists both global and project skills.

### `remove`

Uninstall a skill.

```bash
bmo remove SKILL_NAME [here|everywhere] [HARNESS] [--project | --global] [--harness NAME | --skills-dir PATH] [--yes]
```

| Flag | Description |
|------|-------------|
| `--project` | Remove from the current project |
| `--global` | Remove from the global install directory |
| `--harness` | Select the harness that owns the install |
| `--skills-dir` | Select an explicit skills directory |
| `--yes` | Skip confirmation |

Removes the skill directory from disk and its entry from the metadata file. Any subagents the skill installed are deleted too — only the exact files bmo recorded, so hand-written subagents sharing the directory are left alone.

### `update`

Refresh installed skills whose source content changed.

```bash
bmo update [SKILL_NAME] [here|everywhere] [HARNESS] [--project | --global] [--harness NAME | --skills-dir PATH] [--yes] [--dry-run]
bmo update SKILL_NAME [--project] [--global] [--harness NAME | --skills-dir PATH] [--yes] [--dry-run]
```

| Flag | Description |
|------|-------------|
| `--all` | Update every tracked skill (the default when no name is given) |
| `--project` | Update only project-installed skills |
| `--global` | Update only globally-installed skills |
| `--harness` | Update installs tracked for a built-in harness |
| `--skills-dir` | Update an install in an explicit skills directory |
| `--yes` | Skip confirmation |
| `--dry-run` | Show what would be updated without writing anything |

Re-resolves each skill's original source and compares its content hash against
the installed copy: unchanged skills are reported as `up to date` and left
untouched; changed ones are reinstalled (existing files overwritten, the
original `InstalledAt` timestamp preserved). Skills tracked from the same
source share a single download per run.

The hash covers exactly the files an install would copy, so `.bmoignore`d paths
never trigger a spurious update. Subagents are reconciled too: ones the new
version no longer ships are removed.

#### `bmo update everywhere`: every repo at once

On `update`, the `everywhere` keyword means more than "global scope": it walks
**every project bmo has ever installed into**, without you cd-ing anywhere.

```bash
bmo update everywhere              # global skills + every registered repo
bmo update cool-skill everywhere   # one skill, wherever it is tracked
```

Every project-scope install records its repo in a registry at
`~/.bmo/projects.json`, and `update everywhere` visits each recorded repo in
turn (repos whose directory has vanished are skipped with a note; `bmo doctor`
lists them). Repos installed into before the registry existed are backfilled
the first time any `bmo update` runs inside them. Sources shared across repos
are still downloaded only once per run — and a source that fails to resolve is
tried once, not once per skill.

Unless a harness is named, the sweep covers **every built-in harness**: a
project installed into with `bmo add src codex here` is updated by a plain
`bmo update everywhere`, under a `(codex)` heading. Name a harness
(`bmo update everywhere codex`) to narrow the sweep to it. One destination's
failure never stops the sweep; everything that went wrong is reported at the
end, after every reachable install has been updated.

### `doctor`

Run system diagnostics.

```bash
bmo doctor [here|everywhere] [HARNESS] [--harness NAME | --skills-dir PATH]
```

Checks:
- Global and project skills directories exist and are writable
- Global and project metadata files are valid JSON
- Every tracked skill path exists and contains `SKILL.md`
- No duplicate skill names across scopes
- Harness-specific skill and metadata destinations
- Metadata that tracks subagents a harness cannot host (the state `remove` refuses)
- `CLAUDE_CONFIG_DIR` status when checking the Claude preset

The `here` / `everywhere` keyword (or `--project` / `--global`) narrows the
report to one scope. An unresolvable destination — say, no home directory — is
reported as an `ERROR` line while every other diagnostic still runs.

---

## Coding harnesses

Claude remains the default for backward compatibility. On `add`, put the harness name directly after the source (`bmo add owner/repo codex`). `--harness` remains available on every managed command and is the form used by `init`, `list`, `remove`, `update`, and `doctor`:

| Harness | Project skills | Global skills |
|---------|----------------|---------------|
| `claude` | `.claude/skills/` | `$CLAUDE_CONFIG_DIR/skills/` or `~/.claude/skills/` |
| `codex` (`chatgpt` alias) | `.agents/skills/` | `~/.agents/skills/` |
| `cursor` | `.cursor/skills/` | `~/.cursor/skills/` |
| `gemini` | `.gemini/skills/` | `~/.gemini/skills/` |
| `copilot` | `.github/skills/` | `~/.copilot/skills/` |
| `windsurf` | `.windsurf/skills/` | `~/.codeium/windsurf/skills/` |
| `opencode` | `.opencode/skills/` | `~/.config/opencode/skills/` |
| `amp` | `.agents/skills/` | `~/.config/agents/skills/` |
| `cline` | `.cline/skills/` | `~/.cline/skills/` |

Run `bmo harnesses` to print this table from the installed binary. `chatgpt` is an alias for `codex`: both use the same files and canonical `codex` metadata, so installs cannot drift or be removed out from under one another. Invoke an installed skill as `@name` in ChatGPT and `$name` in Codex. The shared project and global locations use the cross-harness `.agents/skills` convention, which is also discovered by Cursor, Gemini CLI, GitHub Copilot, Windsurf, OpenCode, and Amp. For a new or private harness, `--skills-dir PATH` uses the exact directory you provide and stores `bmo-lock.json` beside it.

Detection is path-based and does not read credentials. In addition to CLI and configuration-directory detection, macOS recognizes Claude.app, Cursor.app, Windsurf.app, and either ChatGPT.app or Codex.app. A broken CLI does not prevent BMO from installing files for a working desktop app.

Each distinct destination has its own metadata. Global non-Claude metadata lives at `~/.bmo/<harness>-skills.json`; project metadata lives beside that harness's `skills/` directory. Presets that intentionally share `.agents/skills` also share the physical project install—one copy serves every harness that discovers that directory.

Portable/non-Claude installs enforce the common denominator of the Agent Skills format: `name` must be explicit, use lowercase letters/digits separated by single hyphens, and match the installed folder; `description` must be 1–1024 characters. The default Claude target retains its historical support for omitted names, `--name` overrides, and legacy hyphen placement (a pre-existing `my--skill` keeps installing and updating there; `inspect` warns that such a name is not portable).

---

## Scopes

Skills can be installed in two scopes:

### Global

Installed to the chosen harness's global directory in the table above.

For the default Claude preset, global metadata remains at `~/.bmo/skills.json` for backward compatibility.

### Project

Installed to the chosen harness's project directory.

For the default Claude preset, project metadata remains at `<project-root>/.claude/bmo-lock.json` for backward compatibility.

Both scopes can coexist. A skill name collision across scopes triggers a warning during `bmo doctor`.

### Subagents

A skill may ship Claude Code subagents in an `agents/` folder. A subagent is a separate worker with its own context window, model, and tool allowlist, spawned by name and able to run in parallel — as opposed to a skill, which loads instructions into the current context.

Claude Code discovers subagents from its agents directory, beside the skills directory, so bmo installs them to a second destination that follows the same scope:

| Scope | Skill | Subagents |
|-------|-------|-----------|
| Global | `~/.claude/skills/<name>/` (or `$CLAUDE_CONFIG_DIR/skills/`) | `~/.claude/agents/` (or `$CLAUDE_CONFIG_DIR/agents/`) |
| Project | `<project-root>/.claude/skills/<name>/` | `<project-root>/.claude/agents/` |

Behavior:

- Only top-level `agents/*.md` files are installed; nested folders are ignored because Claude Code does not scan them.
- Each file must parse as frontmatter with a non-empty `description`. `name` defaults to the filename stem and must use lowercase letters/digits separated by single hyphens. A malformed subagent fails the install rather than being skipped.
- Installed filenames are recorded in metadata, so `bmo remove` deletes exactly those files, and `bmo update` removes subagents a new version no longer ships.
- Installing over a subagent bmo doesn't own requires `--force`. Existing files are moved aside first and restored if any later step fails.
- The `agents/` folder is also copied inside the skill, so the install stays a faithful copy of the published tree.
- `bmo doctor` reports subagents that went missing and files claimed by more than one skill.

---

## `.bmoignore`

By default an install copies everything except `.git`, `node_modules`, `.venv`, and `__pycache__`. A `.bmoignore` file at the skill root excludes more — useful when the skill folder is also a working repository and you don't want its tests, CI config, or demo assets in the user's skills directory.

```gitignore
# Development-only trees
tests/
.github/
screenshots/
*.gif

# Anchored to the skill root, so a nested build/ survives
/build

# ** spans path segments
docs/**/*.png

# A later ! line re-includes
*.svg
!logo.svg
```

| Form | Meaning |
|------|---------|
| `# text` | comment; blank lines are skipped |
| `name` | matches that basename at any depth |
| `name/` | directories only |
| `/name` or `a/b` | anchored to the skill root |
| `*`, `?` | wildcards within one path segment |
| `**` | spans zero or more segments |
| `!name` | re-includes something an earlier line excluded |

Two deliberate deviations from a naive reading:

- **`SKILL.md` at the skill root is never ignorable**, so a broad `*.md` cannot produce an install that fails its own validation.
- **A negation cannot rescue a file inside an excluded directory.** `vendor/` followed by `!vendor/keep.txt` still excludes `keep.txt` — the same rule git applies. Use `vendor/*` when you need exceptions.

The rules apply to every walk: which files are copied, which subagents are installed, which folders are discovered as skills, and the content hash `bmo update` compares. Because the copier and the hasher share one walk, excluded files never register as phantom changes that make `update` reinstall on every run.

`.bmoignore` is read from the skill root only — nested ignore files have no effect — and the file itself is installed, so the skill on disk documents what was left out.

### Picking a scope: `here` / `everywhere`

`add`, `init`, `list`, `remove`, `update`, and `doctor` take an optional
location keyword as a plain positional word — a friendlier alias for the
`--project` / `--global` flags:

| Keyword | Meaning | Equivalent flag |
|---------|---------|-----------------|
| `here` | the current harness's project skills directory | `--project` |
| `everywhere` | globally (the default) | `--global` |

```bash
bmo add owner/repo here          # install into this project
bmo add owner/repo everywhere    # install globally (same as the default)
bmo list here                    # list only this project's skills
bmo remove cool-skill here       # remove from this project
bmo update here                  # update this project's skills
```

The keyword may appear before or after the other argument, so
`bmo add here owner/repo` works too. Commands default to **global** when no
keyword or flag is given (`bmo list` with neither still lists both scopes). The
`--project` / `--global` flags continue to work and can be used
interchangeably; a contradictory pairing (`bmo list everywhere --project`) is
rejected rather than silently resolved.

One exception: on `update`, `everywhere` reaches further than the global scope —
it also updates every registered project repo. See
[`bmo update everywhere`](#bmo-update-everywhere-every-repo-at-once).

### Naming a harness positionally

The same commands accept a harness name as a plain positional word, anywhere in
the arguments — a friendlier alias for `--harness`:

```bash
bmo init codex                   # install the bundled skill for Codex
bmo init chatgpt                 # same files, with ChatGPT invocation guidance
bmo list here codex              # this project's Codex skills
bmo update codex                 # every skill tracked for Codex
bmo remove cool-skill codex      # remove from Codex
bmo doctor codex                 # diagnose Codex's destinations
```

`everyone` is only meaningful for `add`, which installs to every detected
harness at once; the other commands act on one harness and reject it with an
explanation. A positional harness cannot be combined with `--harness` or
`--skills-dir`. Positional names match case-insensitively, exactly like
`--harness`.

Because `remove` requires a skill name, a lone harness-shaped word there is read
as the skill: `bmo remove codex` removes a skill *named* `codex`, while
`bmo remove codex codex` removes it from the Codex harness. `add` treats its
required source the same way: `bmo add codex` installs the local folder
`./codex`, while `bmo add ./src codex` installs `./src` for Codex. This applies
even to `everyone` — `bmo remove everyone` removes a skill *named* `everyone`.
`update` has no required argument, so `bmo update codex` means "update
everything tracked for Codex"; a skill named `codex` (or `everyone`) is covered
by a plain `bmo update`.

---

## Security

**`bmo` copies files only.** It never executes downloaded code, runs install scripts, or invokes package managers.

When a skill contains files with executable extensions (`.py`, `.sh`, `.js`, `.rb`, etc.) or notable dependency files (`requirements.txt`, `package.json`, `Cargo.toml`, etc.), bmo prints a warning before installation so you can review what you're installing.

Additional hardening:

- **Zip-slip protection** — entries in downloaded archives that resolve outside the extraction directory are rejected.
- **Size caps** — downloads and extracted archives are bounded (256 MiB) to guard against decompression bombs.
- **No symlink following** — `bmo` refuses to copy symlinks when installing a skill, so a skill folder can't read files outside its tree.
- **Scoped removal** — `bmo remove` refuses to delete anything outside the managed skills directory, and deletes only the subagent files recorded in metadata.
- **No silent subagent takeover** — installing over a subagent bmo didn't write requires `--force`, so one skill can't quietly replace another's specialist.

---

## Metadata

Metadata is stored as JSON and records every installed skill's name, description, source, path, install times, scope, harness, and any Claude subagent files it installed.

Writes are **atomic** — bmo writes to a temporary file first, syncs it to disk, then renames it over the target. Partial writes are never visible.

---

## Troubleshooting

1. **Run `bmo doctor`** — it checks the most common issues.
2. Run `bmo harnesses` and confirm you selected the intended preset.
3. For Claude, ensure `CLAUDE_CONFIG_DIR` is set if you expect a custom Claude location.
4. Check the metadata path printed by `bmo doctor --harness NAME`.
5. For permission issues, verify the skills directory is writable.
6. Open an [issue](https://github.com/justin06lee/bmo/issues) if problems persist.

---

## Development

```bash
# Run tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Build
go build -o bmo .
```

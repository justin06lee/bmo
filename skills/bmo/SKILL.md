---
name: bmo
description: Use when managing portable coding-agent skills with the bmo CLI (install, inspect, list, update, remove, doctor, upgrade, scout for existing installs, share skills between harnesses, or target ChatGPT/Codex/Claude/Cursor/Gemini/Copilot/Windsurf/OpenCode/Amp/Cline/Grok) or when creating a bmo-compatible SKILL.md package.
---

# bmo

`bmo` is a tiny command-line installer for portable Agent Skills. A skill is a
folder containing a `SKILL.md` file. `bmo` resolves a source (GitHub repo,
local folder, or zip URL), validates the skill, copies it into the selected coding harness's
skills directory, and tracks it so it can be listed, updated, or removed.

A skill may also bundle **Claude Code subagents** in an `agents/` folder. The
`claude` preset exports and tracks those files. Other harnesses retain the
folder as a skill resource because their live agent schemas differ.

It **only copies files** — it never executes downloaded code, runs install
hooks, or installs dependencies.

This skill covers two jobs: **using bmo** to manage installed skills, and
**authoring skills** that bmo can install. If `bmo` is not installed, tell the
user to run `go install github.com/justin06lee/bmo@latest`.

---

## Part 1 — Using bmo

### Core commands

```bash
bmo add SOURCE        # install a skill (and any subagents it bundles)
bmo add SOURCE codex  # install into the shared Codex/ChatGPT .agents/skills location
bmo add SOURCE chatgpt # alias for codex, with ChatGPT invocation guidance
bmo add SOURCE gemini # install into Gemini CLI's native location
bmo add SOURCE everyone # install into every detected harness on the machine
bmo add SOURCE --all  # install every skill the source contains
bmo inspect SOURCE    # preview a skill without installing
bmo list              # list installed skills (both scopes)
bmo update            # re-check every tracked skill, reinstall the changed ones
bmo update NAME       # same check for one skill
bmo update everywhere everyone # every skill, every harness, every known project
bmo scout             # find projects bmo installed into, record them for later
bmo share everyone    # give every harness the union of everyone's skills
bmo remove NAME       # uninstall a skill
bmo remove NAME universe # delete every copy, every harness, every known project
bmo doctor            # run diagnostics
bmo init              # (re)install this bundled bmo skill
bmo harnesses         # list supported harness presets and paths
bmo upgrade           # upgrade the bmo binary itself to the latest release
bmo --version         # show the installed bmo version
```

Always run `bmo inspect SOURCE` before `bmo add` for third-party sources so
the user can see the file list and any executable-file warnings first.

### Coding harnesses

Claude is the backward-compatible default. For `add`, place a harness name
directly after the source: `bmo add owner/repo codex`. The `--harness NAME`
form remains available for compatibility and is used by `init`, `list`,
`remove`, `update`, `share`, and `doctor`:

| Name | Project | Global |
|------|---------|--------|
| `claude` | `.claude/skills` | `~/.claude/skills` or `$CLAUDE_CONFIG_DIR/skills` |
| `codex` (`chatgpt` alias) | `.agents/skills` | `~/.agents/skills` |
| `cursor` | `.cursor/skills` | `~/.cursor/skills` |
| `gemini` | `.gemini/skills` | `~/.gemini/skills` |
| `copilot` | `.github/skills` | `~/.copilot/skills` |
| `windsurf` | `.windsurf/skills` | `~/.codeium/windsurf/skills` |
| `opencode` | `.opencode/skills` | `~/.config/opencode/skills` |
| `amp` | `.agents/skills` | `~/.config/agents/skills` |
| `cline` | `.cline/skills` | `~/.cline/skills` |
| `grok` | `.grok/skills` | `~/.grok/skills` or `$GROK_HOME/skills` |

The shared Codex/ChatGPT `.agents/skills` location is also a cross-harness
convention read by many of the other presets. Grok Build additionally reads the
`claude` and `cursor` locations unless its `[compat]` config turns them off, so
a skill installed for those harnesses already reaches it; the `grok` preset
targets its own `.grok/skills`, which no configuration can switch off. `chatgpt` resolves to canonical
`codex` metadata so the two names cannot drift. Invoke a skill as `@name` in
ChatGPT and `$name` in Codex. For any unlisted harness use `--skills-dir PATH`;
do not combine it with `--harness`.

`everyone` detects a harness from its CLI, its user configuration directory,
or a supported macOS desktop app. ChatGPT.app and Codex.app both select the
same canonical `codex` destination. Detection never reads credentials.

Use `bmo add SOURCE everyone` to install into every detected harness. bmo
preflights every destination and writes shared directories only once.

### Source formats

| Format | Example |
|--------|---------|
| GitHub repo | `owner/repo` or `github:owner/repo` |
| GitHub subpath | `owner/repo/path/to/skill` |
| GitHub with ref | `owner/repo@v1.0.0` or `owner/repo/path@branch` |
| Local directory | `./path/to/skill` (must start with `./`, `../`, `/`, or `~`) |
| Zip URL | `https://example.com/skill.zip` |
| This bundled skill | `bmo` (or `self`) |

A bare `owner/repo` is treated as GitHub. When no ref is given, bmo tries the
`main` branch, then falls back to `master`. Sources are capped at 256 MiB.

### Scopes: `here`, `everywhere`, and `universe`

`add`, `init`, `list`, `remove`, `update`, `share`, and `doctor` accept an
optional location keyword as a plain positional word, before or after the
other argument:

- **`here`** — the selected harness's project skills directory
- **`everywhere`** — the selected harness's global directory (the default)
- **`universe`** — the global destinations **plus every project in the
  registry**; only `remove`, `update`, and `share` sweep that wide, and the
  others reject it with an explanation

`bmo remove NAME universe` is how you delete every copy of a skill in one
command. `update` and `share` predate the word and read their `everywhere` as
that same machine-wide sweep; `universe` is a synonym there.

```bash
bmo add owner/repo here       # install into this project
bmo list here                 # only this project's skills
bmo remove cool-skill here
bmo remove cool-skill everywhere  # the global install
bmo remove cool-skill universe    # every copy bmo can reach
bmo update here               # update only this project's skills
```

The `--project` / `--global` flags are equivalent, and a contradictory pairing
(`bmo list everywhere --project`) is rejected. `bmo list` and `bmo update`
with no keyword or flag cover both scopes.

Location and harness tokens can appear anywhere around the source, and flags
can follow them in any order. For example, all of these are valid:

```bash
bmo add everywhere owner/repo everyone --all --yes
bmo add here owner/repo everyone
bmo add codex owner/repo here --dry-run
```

Do not combine `--project` with `--global`, `--all` with `--name`, or a
positional harness with `--harness`/`--skills-dir`.

### Naming a harness positionally

`add`, `init`, `list`, `remove`, `update`, `share`, and `doctor` all accept a
harness name as a plain positional word — the friendlier alias for `--harness`:

```bash
bmo init codex                 # install the bundled skill for Codex
bmo list here codex            # this project's Codex skills
bmo update codex               # every skill tracked for Codex
bmo remove cool-skill codex    # remove from Codex
bmo doctor codex               # diagnose Codex's destinations
bmo share codex                # seed every other harness from Codex
```

`everyone` works with `add`, `remove`, `update`, and `share` — the commands
that fan out across harnesses. `init`, `list`, and `doctor` act on a single
harness and reject it with an explanation, as they reject `universe`. Positional harness names match
case-insensitively, like `--harness`.

Two disambiguation rules matter when a skill or source is *named* like a
harness: `bmo remove codex` removes the skill named `codex` (remove always
needs a name), while `bmo remove codex codex` removes it from the Codex
harness. `bmo add codex` likewise installs the local folder `./codex`, and
`bmo remove everyone` removes a skill named `everyone`. `bmo update codex`
means "update everything tracked for Codex", since update's name argument is
optional; a skill named `codex` is covered by a plain `bmo update`, and so is
one named `everyone` (which `update` now reads as the all-harness keyword).

### Updating

`bmo update` re-resolves each tracked skill's original source, compares a
content hash against the installed copy, reinstalls only what changed, and
reports everything else as `up to date`. Use `--dry-run` to preview.

`bmo update everywhere` goes further than the global scope: it also visits
**every repo bmo has ever installed into** (each project-scope install records
its repo in `~/.bmo/projects.json`) and updates their skills too — no need to
cd into each repo. Vanished repos are skipped with a note. Unless a harness is
named, the sweep covers every built-in harness, so codex/gemini installs are
updated by the plain command; one failure never stops the rest of the sweep.

`everyone` spells the all-harness sweep out loud and pairs with any location:

```bash
bmo update universe              # the same sweep, in the CLI's newer wording
bmo update everywhere everyone   # same as `bmo update everywhere`, made explicit
bmo update everyone              # every harness, this directory's destinations
bmo update here everyone         # every harness, this project only
```

It cannot be combined with `--harness` or `--skills-dir`, which each name one
destination. Destinations tracking nothing are skipped silently.

If `update everywhere` misses a repo, the registry has not seen it — run
`bmo scout` (below). That is the normal case after cloning a repo whose skills
were committed, or on a new machine.

### Removing

`bmo remove NAME` uninstalls from one destination: it deletes the skill folder,
the subagent files bmo recorded for it, and its metadata entry — nothing else.
`here` and `everywhere` pick that destination, as they do on every command.

`bmo remove NAME universe` is the sweep. It deletes every copy the registry can
reach: the global install plus every project bmo has installed into, across
every harness, in one run. `everyone` widens whichever location was named
across every harness without reaching any further.

```bash
bmo remove cool-skill universe         # every copy on the machine
bmo remove cool-skill universe codex   # every copy, Codex only
bmo remove cool-skill everywhere everyone  # every harness's global install
bmo remove cool-skill everyone         # every harness, this directory's destinations
bmo remove cool-skill here everyone    # every harness, this project only
```

Every copy is listed and confirmed once before anything is deleted, so a sweep
is safe to run just to see what is out there (answer `n`, or pass `--yes` to
skip the prompt). Locations that never had the skill are not an error — only a
sweep that finds no copy at all fails. One destination bmo cannot clean up
never strands the others: the rest are removed and the refusal is reported at
the end.

A project that moved after installation records a path that no longer exists.
The sweep deletes the copy that is really inside the harness's skills
directory, and untracks entries whose files are already gone. Nothing outside
the resolved skills directory is ever deleted.

If a sweep misses a repo, the registry has not seen it — run `bmo scout` first.
`universe` cannot be combined with `--project`, `--global`, or `--skills-dir`,
which each name one destination.

### Finding installs: `bmo scout`

```bash
bmo scout [PATH] [--depth N] [--hidden] [--prune] [--dry-run] [--json]
```

Walks every directory below `PATH` (the current directory by default), finds
the projects holding a bmo lock file that tracks skills, and records them in
`~/.bmo/projects.json` so `bmo remove universe`, `bmo update everywhere`, and
`bmo share everywhere` can reach them.

It never installs, moves, or deletes a skill — the only file it writes is the
project registry. `--dry-run` previews without writing even that.

The walk skips `node_modules`, `vendor`, `.venv`, `target`, `dist`, `build`,
`.git`, and similar trees, plus unrelated hidden directories unless `--hidden`
is passed; harness configuration folders (`.claude`, `.agents`, `.cursor`, …)
are always inspected. Symlinked directories are not followed. `--depth N`
bounds a large sweep (`bmo scout ~ --depth 4`). `--prune` forgets registered
projects whose directory is gone; it is opt-in, because an unmounted drive
looks the same as a deleted repo.

Only the built-in presets are discoverable: a `--skills-dir` install puts its
lock file in an arbitrary place.

### Syncing harnesses: `bmo share`

```bash
bmo share [here|everywhere|universe] [everyone|HARNESS] [--project|--global] [--yes] [--dry-run]
```

Gives every harness the **union** of the skills all of them have. Whatever
Claude has and Codex lacks is copied into Codex, and vice versa.

The sync is **purely additive**: a skill is only ever copied into a harness
that does not already have it, nothing is replaced or deleted (there is no
`--force`), an untracked folder in the way is left alone and reported, and
running it twice is a no-op.

Skills stay in the location they were installed in — a project's harnesses
exchange that project's skills, the global destinations exchange global ones.
A project skill is never promoted into global config; use
`bmo add ./that-skill everyone` for that.

```bash
bmo share                      # global + this project
bmo share everywhere everyone  # global + every registered repo, every harness
bmo share here                 # this project only
bmo share codex                # seed every other harness from Codex only
```

Copies come from the installed skill folder, so a sync is local and works
offline, but each copy keeps the donor's recorded source — `bmo update` in the
new destination still follows the real upstream.

Participants are the harnesses detected on this machine plus any harness that
already tracks skills at that location. A skill a portable harness would reject
(a legacy Claude skill with no `name:` in its frontmatter) is reported as a
skipped addition instead of failing the sync. Run `bmo scout` first so
`bmo share everywhere` knows about every project.

### Installing a whole suite

Repositories that ship several skills meant to work together install in one go
with `--all`:

```bash
bmo add owner/repo --all           # every skill in the repo
bmo add owner/repo/skills --all    # every skill under skills/
```

Each skill is still tracked separately, so `update` and `remove` work per skill.
The whole batch is checked before anything is written, and nothing is installed
if any skill fails validation, two folders claim the same name, a skill is
already installed (without `--force`), or two skills ship the same subagent
file. Duplicate names are reported rather than resolved — narrow the source to a
subpath, or install the odd one out separately with `--name`.

### Useful flags

- `--all` — install every skill in the source (not combinable with `--name`)
- `--name NAME` — override the installed folder name for legacy Claude installs;
  portable harnesses require it to match the declared frontmatter name
- `--force` — replace an existing install of the same name (on `add`)
- `--yes` — skip confirmation prompts; use for non-interactive runs
- `--dry-run` — show what would happen without writing anything; suppresses first-run bootstrap writes
- `--json` — machine-readable output (on `list`)
- positional `HARNESS` on `add` — use a built-in coding-harness preset
- positional `everyone` on `add`, `remove`, `update`, `share` — fan out across harnesses
- `--depth`, `--hidden`, `--prune` (on `scout`) — bound or tidy a sweep
- `--harness NAME` — compatibility alias and target selector on other commands
- `--skills-dir PATH` — use an exact skills directory for any other harness

### Troubleshooting

If a skill folder is missing, metadata looks corrupt, or names collide across
scopes, run `bmo doctor` — it pinpoints the issue without changing anything.
The first bmo run auto-installs this skill globally once per selected harness;
`bmo remove bmo --harness NAME` sticks after that.

---

## Part 2 — Authoring bmo-compatible skills

Whenever you create or restructure a skill, follow this contract exactly so
`bmo add` accepts it.

```
my-skill/                 <- folder name: lowercase letters, digits, hyphens only
├── SKILL.md              <- required, at the folder root, frontmatter first
├── .bmoignore            <- optional; paths to keep out of the install
├── references/           <- optional supporting files, copied verbatim
├── scripts/              <- optional; executable files are allowed but flagged
└── agents/               <- optional Claude subagents; otherwise normal resources
```

`SKILL.md` must **begin** with YAML frontmatter fenced by `---` lines — no
blank lines, comments, or prose before the opening `---`:

```markdown
---
name: my-skill
description: Use when <trigger conditions>. Triggers on phrases like <examples>.
---

# my-skill

Instructions for the coding agent go here.
```

### Frontmatter rules

- `description` is **required** and non-empty. Keep it under 1024 characters.
  Write it as trigger guidance: when should an agent reach for this skill?
- `name` is required for every portable/non-Claude target and must match the
  installed folder. It is optional only for backward-compatible Claude installs.
  Always write it as lowercase letters and digits separated by single hyphens,
  at most 64 characters — portable targets enforce that grammar. The default
  Claude target additionally tolerates legacy hyphen placement (`my--skill`)
  for compatibility with old installs; `inspect` warns that such a name is not
  portable. The name becomes the installed folder name and the invocation name
  in harnesses that support explicit skill invocation.
- If `name` is omitted, the folder name is used instead: lowercased, with
  every run of other characters collapsed to a single `-`. Prefer setting
  `name` explicitly.

### Shipping subagents with a skill

A skill may bundle Claude Code **subagents** in an `agents/` folder. A subagent
is not a skill: a skill loads instructions into the current context, while a
subagent is a separate worker with its own context window, model, and tool
allowlist, spawned by name and able to run in parallel with others.

Claude Code discovers subagents from its **agents** directory, which sits beside
the skills directory, so bmo installs them to a second destination only when
`--harness claude` is selected:

| Scope | Skill goes to | Subagents go to |
|-------|---------------|-----------------|
| `everywhere` (global) | `~/.claude/skills/<name>/` | `~/.claude/agents/` |
| `here` (project) | `./.claude/skills/<name>/` | `./.claude/agents/` |

```markdown
---
name: my-specialist
description: What this worker does and when to hand work to it.
model: sonnet
tools: Read, Grep, Glob
---

You are a specialist. When given …
```

Rules for `agents/`:

- Only top-level `*.md` files are installed. Claude Code does not scan nested
  folders, so bmo refuses to install them rather than create subagents that
  never resolve.
- Each file needs frontmatter with a non-empty `description`, exactly like a
  skill. `name` is optional; the filename stem is used when it's absent, and it
  must use lowercase letters and digits separated by single hyphens.
- The file is installed under the name you shipped it as, and recorded in bmo's
  metadata. `bmo remove` deletes exactly those files and nothing else, so
  hand-written subagents in the same directory are never touched.
- `bmo update` reconciles: subagents the new version drops are removed.
- Installing over a subagent bmo doesn't own (yours, or another skill's) is
  refused unless you pass `--force`.
- `agents/` is also copied inside the skill folder, so the installed skill stays
  a faithful copy of what you published.

### Excluding files with `.bmoignore`

When the skill folder is also a working repository, put a `.bmoignore` at the
skill root so tests, CI config, and demo assets never reach the user's skills
directory. Without one, everything except `.git`, `node_modules`, `.venv`, and
`__pycache__` is installed.

```gitignore
# Development-only trees
tests/
.github/
screenshots/
*.gif

# Anchored to the skill root, so nested build/ folders survive
/build

# Wildcards span segments with **
docs/**/*.png

# A later ! line re-includes
*.svg
!logo.svg
```

Syntax is the gitignore subset you already know:

| Form | Meaning |
|------|---------|
| `# text` | comment; blank lines are skipped |
| `name` | matches that basename at any depth |
| `name/` | matches directories only |
| `/name` or `a/b` | anchored to the skill root |
| `*`, `?` | wildcards inside one path segment |
| `**` | spans zero or more segments |
| `!name` | re-includes something an earlier line excluded |

Two rules differ from a naive reading, both deliberately:

- **`SKILL.md` at the skill root can never be ignored.** A pattern like `*.md`
  excludes everything else but leaves the skill installable.
- **A negation cannot rescue a file inside an excluded directory.** `vendor/`
  followed by `!vendor/keep.txt` still excludes `keep.txt`, exactly as git
  behaves. Exclude `vendor/*` instead if you need exceptions.

The `.bmoignore` file is itself installed, so the skill on disk documents what
was left out. It applies to every walk bmo does: which files get copied, which
subagents get installed, which folders count as skills, and the content hash
`bmo update` compares — so excluded files never show up as phantom changes.

### Hard rules (install fails if violated)

- `SKILL.md` must exist at the skill folder's root and start with frontmatter.
- The frontmatter must be valid YAML and closed with a `---` line.
- `description` must be non-empty.
- The resolved name must use lowercase letters, digits, and hyphens (≤ 64
  chars). Portable/non-Claude targets additionally reject leading, trailing,
  or repeated hyphens — write single-hyphen names so the skill installs
  everywhere.
- **No symlinks anywhere in the tree** — the copy refuses them outright.
- Every `agents/*.md` file must parse, carry a non-empty `description`, and
  resolve to a valid name. A malformed subagent fails the whole install rather
  than being skipped silently.
- A subagent filename that collides with one bmo didn't install requires
  `--force`.

### Silently ignored / limits

- `.git`, `node_modules`, `.venv`, and `__pycache__` directories are always
  skipped during discovery and copying — never put required content inside them.
- Anything matched by `.bmoignore` is skipped as well.
- Only top-level `agents/*.md` files become subagents; nested folders are
  ignored because Claude Code does not scan them.
- `.bmoignore` is read from the skill root only; nested ignore files have no
  effect.
- Executable-looking files (`.py`, `.sh`, `.js`, …) and dependency manifests
  (`package.json`, `requirements.txt`, …) are allowed but surfaced to the user
  as a security warning. bmo never runs them; scripts must be invoked from the
  SKILL.md instructions.

### Laying out a repo for distribution

- If `SKILL.md` sits at the source root, the root itself is the skill.
- Otherwise the whole tree is walked and **every folder containing a
  `SKILL.md` is a separate installable skill**.

```
# Single skill: SKILL.md at the repo root
repo/
├── SKILL.md
├── .bmoignore        <- keep tests/ and CI config out of the install
├── scripts/
└── agents/

# Multiple skills: one folder per skill
repo/
└── skills/
    ├── skill-one/SKILL.md
    └── skill-two/SKILL.md
```

Don't nest a `SKILL.md` inside another skill's folder — each one is treated
as its own skill, and multi-match sources force the user to pick with `--name`
or install the set with `--all`.

Skill names must be unique across the whole source, or `--all` refuses the
batch. Mirroring one skill in two places (say `skills/x/` and
`extensions/pack/skills/x/`) is the usual cause.

The single-skill shape is the one to reach for when a skill needs a shared
runtime: scripts, binaries, and agents all live inside the installed folder, so
one `bmo add owner/repo` delivers a working whole. The multi-skill shape suits
independent skills that share a repository but not a runtime.

### Verify before shipping

```bash
bmo inspect ./my-skill    # runs the real validator: name, description, warnings
```

`inspect` reports the resolved name, the file count after `.bmoignore` is
applied, how many ignore rules ran, the subagents that would be installed, and
any warnings. Check the file count and subagent list against what you expect —
that is where a mis-scoped ignore rule or a missed `agents/` folder shows up.

Fix anything reported, then deliver with `bmo add ./my-skill` (or
`bmo add owner/repo` once pushed).

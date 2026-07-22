---
name: bmo
description: Use when managing Claude Code skills with the bmo CLI (install, inspect, list, update, remove, doctor, upgrade) or when creating/formatting a skill so bmo can install it. Triggers on "install a skill", "add/update/remove a skill", "bmo <anything>", pointing at a GitHub repo, folder, or zip containing a SKILL.md, and on "make a skill", "write a SKILL.md", "package this as a skill", "make this bmo-compatible".
---

# bmo

`bmo` is a tiny command-line installer for Claude Code skills. A skill is a
folder containing a `SKILL.md` file. `bmo` resolves a source (GitHub repo,
local folder, or zip URL), validates the skill, copies it into Claude Code's
skills directory, and tracks it so it can be listed, updated, or removed.

A skill may also bundle **subagents** in an `agents/` folder. Those are
installed into Claude Code's agents directory and tracked with the skill, so
`bmo remove` takes them away again. See "Shipping subagents with a skill".

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
bmo add SOURCE --all  # install every skill the source contains
bmo inspect SOURCE    # preview a skill without installing
bmo list              # list installed skills (both scopes)
bmo update            # re-check every tracked skill, reinstall the changed ones
bmo update NAME       # same check for one skill
bmo remove NAME       # uninstall a skill
bmo doctor            # run diagnostics
bmo init              # (re)install this bundled bmo skill
bmo upgrade           # upgrade the bmo binary itself to the latest release
bmo --version         # show the installed bmo version
```

Always run `bmo inspect SOURCE` before `bmo add` for third-party sources so
the user can see the file list and any executable-file warnings first.

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

### Scopes: `here` and `everywhere`

`add`, `init`, `list`, `remove`, and `update` accept an optional location
keyword as a plain positional word, before or after the other argument:

- **`here`** — the current project (`./.claude/skills`, subagents in
  `./.claude/agents`, metadata in `.claude/bmo-lock.json`)
- **`everywhere`** — global, the default (`$CLAUDE_CONFIG_DIR/skills/` or
  `~/.claude/skills/`, subagents in the sibling `agents/` directory, metadata in
  `~/.bmo/skills.json`)

```bash
bmo add owner/repo here       # install into this project
bmo list here                 # only this project's skills
bmo remove cool-skill here
bmo update here               # update only this project's skills
```

The `--project` / `--global` flags are equivalent. `bmo list` and `bmo update`
with no keyword or flag cover both scopes.

### Updating

`bmo update` re-resolves each tracked skill's original source, compares a
content hash against the installed copy, reinstalls only what changed, and
reports everything else as `up to date`. Use `--dry-run` to preview.

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
- `--name NAME` — override the installed folder name (must match `^[a-z0-9-]+$`)
- `--force` — replace an existing install of the same name (on `add`)
- `--yes` — skip confirmation prompts; use for non-interactive runs
- `--dry-run` — show what would happen without writing anything
- `--json` — machine-readable output (on `list`)

### Troubleshooting

If a skill folder is missing, metadata looks corrupt, or names collide across
scopes, run `bmo doctor` — it pinpoints the issue without changing anything.
The first bmo run auto-installs this skill globally once (sentinel:
`~/.bmo/.bootstrapped`); `bmo remove bmo` sticks after that.

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
└── agents/               <- optional subagent definitions, installed separately
```

`SKILL.md` must **begin** with YAML frontmatter fenced by `---` lines — no
blank lines, comments, or prose before the opening `---`:

```markdown
---
name: my-skill
description: Use when <trigger conditions>. Triggers on phrases like <examples>.
---

# my-skill

Instructions for Claude go here.
```

### Frontmatter rules

- `description` is **required** and non-empty. Keep it under 1024 characters.
  Write it as trigger guidance: when should Claude reach for this skill?
- `name` is optional but recommended. If present it must match `^[a-z0-9-]+$`
  and be at most 64 characters. It becomes the installed folder name and the
  `/slash-command` in Claude Code.
- If `name` is omitted, the folder name is used instead: lowercased, with
  every run of other characters collapsed to a single `-`. Prefer setting
  `name` explicitly.

### Shipping subagents with a skill

A skill may bundle Claude Code **subagents** in an `agents/` folder. A subagent
is not a skill: a skill loads instructions into the current context, while a
subagent is a separate worker with its own context window, model, and tool
allowlist, spawned by name and able to run in parallel with others.

Claude Code discovers subagents from its **agents** directory, which sits beside
the skills directory, so bmo installs them to a second destination:

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
  must match `^[a-z0-9-]+$`.
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
- The resolved name must match `^[a-z0-9-]+$` (≤ 64 chars).
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

---
name: bmo
description: Use when creating, formatting, or restructuring a Claude Code skill so it can be installed with the bmo CLI. Triggers on phrases like "make a skill", "create a skill", "write a SKILL.md", "package this as a skill", "make this bmo-compatible", or preparing a folder/repo of skills for distribution.
---

# Authoring bmo-compatible skills

`bmo` installs a skill by copying a folder into Claude Code's skills directory.
A skill is **one folder containing a `SKILL.md` file**. Whenever you create or
restructure a skill, follow this contract exactly so `bmo add` accepts it.

## The contract

```
my-skill/                 <- folder name: lowercase letters, digits, hyphens only
├── SKILL.md              <- required, at the folder root, frontmatter first
├── references/           <- optional supporting files, copied verbatim
└── scripts/              <- optional; executable files are allowed but flagged
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

- `description` is **required** and non-empty. Keep it under 1024 characters
  (longer only produces a warning, but stay under it). Write it as trigger
  guidance: when should Claude reach for this skill?
- `name` is optional but recommended. If present it must match `^[a-z0-9-]+$`
  and be at most 64 characters. It becomes the installed folder name and the
  `/slash-command` in Claude Code.
- If `name` is omitted, the folder name is used instead: lowercased, with every
  run of other characters collapsed to a single `-`. Prefer setting `name`
  explicitly so the identity doesn't depend on the folder.

### Hard rules (install fails if violated)

- `SKILL.md` must exist at the skill folder's root and start with frontmatter.
- The frontmatter must be valid YAML and closed with a `---` line.
- `description` must be non-empty.
- The resolved name must match `^[a-z0-9-]+$` (≤ 64 chars).
- **No symlinks anywhere in the tree** — the copy refuses them outright.

### Silently ignored / limits

- `.git`, `node_modules`, `.venv`, and `__pycache__` directories are skipped
  during discovery and copying — never put required content inside them.
- Downloaded sources (GitHub repos, zip URLs) are capped at 256 MiB.
- Executable-looking files (`.py`, `.sh`, `.js`, `.ts`, `.rb`, `.go`, …) and
  dependency manifests (`package.json`, `requirements.txt`, …) are allowed but
  surfaced to the user as a security warning. bmo only copies files; it never
  runs anything, so scripts must be invoked from the SKILL.md instructions.

## Laying out a repo for distribution

bmo can install from a GitHub repo (`owner/repo`, optional `/sub/path` and
`@ref`), a local folder, or a `.zip` URL. Discovery works like this:

- If `SKILL.md` sits at the source root, the root itself is the skill.
- Otherwise the whole tree is walked and **every folder containing a
  `SKILL.md` is a separate installable skill**.

So structure repos one of two ways:

```
# Single skill: SKILL.md at the repo root
repo/
└── SKILL.md

# Multiple skills: one folder per skill
repo/
└── skills/
    ├── skill-one/SKILL.md
    └── skill-two/SKILL.md
```

Don't nest a `SKILL.md` inside another skill's folder — each one is treated as
its own skill and multi-match sources force the user to disambiguate.

## Verify before shipping

After authoring, validate with the real parser:

```bash
bmo inspect ./my-skill    # shows name, description, file count, warnings
```

Fix anything reported before delivering. The user installs it with
`bmo add ./my-skill` (or `bmo add owner/repo` once pushed).

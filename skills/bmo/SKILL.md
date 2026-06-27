---
name: bmo
description: Use when the user wants to install, inspect, list, update, or remove Claude Code skills with the bmo CLI. Triggers on phrases like "install a skill", "add a skill", "bmo add/list/update/remove/inspect/doctor", pointing at a GitHub repo, local folder, or zip that contains a SKILL.md.
---

# bmo

`bmo` is a tiny command-line installer for Claude Code skills. A skill is just a
folder containing a `SKILL.md` file. `bmo` resolves a source, validates the
skill, copies it into Claude Code's skills directory, and tracks it so it can be
listed, updated, or removed later.

It **only copies files** — it never executes downloaded code, runs install
hooks, or installs dependencies.

## When to use this skill

Use it whenever the user wants to manage Claude Code skills from the terminal:
installing one from a GitHub repo, a local folder, or a zip URL; checking what's
installed; updating; or removing. If `bmo` is not installed, tell the user to run
`go install github.com/justin06lee/bmo@latest`.

## Core commands

```bash
bmo add SOURCE        # install a skill
bmo inspect SOURCE    # preview a skill without installing
bmo list              # list installed skills
bmo update --all      # reinstall every tracked skill from its source
bmo remove NAME       # uninstall a skill
bmo doctor            # run diagnostics
bmo init              # (re)install the bundled bmo skill itself
```

Always run `bmo inspect SOURCE` before `bmo add` for third-party sources so the
user can see the file list and any executable-file warnings first.

## Source formats

| Format | Example |
|--------|---------|
| GitHub repo | `owner/repo` or `github:owner/repo` |
| GitHub subpath | `owner/repo/path/to/skill` |
| GitHub with ref | `owner/repo@v1.0.0` or `owner/repo/path@branch` |
| Local directory | `./path/to/skill` (must start with `./` or `../`) |
| Zip URL | `https://example.com/skill.zip` |
| The bmo skill itself | `bmo` (or `self`) — restores this bundled skill |

A bare `owner/repo` is treated as GitHub. When no ref is given, `bmo` tries
`main`, then falls back to `master`.

## Useful flags

- `--project` — install/list/remove in `./.claude/skills` instead of globally
- `--name NAME` — override the installed folder name (must match `^[a-z0-9-]+$`)
- `--force` — replace an existing install of the same name
- `--yes` — skip confirmation prompts (use for non-interactive runs)
- `--dry-run` — show what would happen without writing anything
- `--json` — (on `list`) machine-readable output

## Scopes

- **Global** — `$CLAUDE_CONFIG_DIR/skills/` (or `~/.claude/skills/`); metadata in
  `~/.bmo/skills.json`.
- **Project** — `<project>/.claude/skills/`; metadata in `.claude/bmo-lock.json`.

## Restoring this skill

This skill ships bundled inside the `bmo` binary. If it ever goes missing, run:

```bash
bmo add bmo      # or: bmo init
```

Both reinstall it offline from the binary — no network or GitHub clone needed.

## Typical flow

```bash
bmo inspect owner/cool-skill      # vet it
bmo add owner/cool-skill          # install globally
bmo list                          # confirm
bmo update --all                  # later: refresh everything
bmo remove cool-skill             # uninstall
```

If something looks wrong (a skill folder is missing, metadata is corrupt, name
collisions across scopes), run `bmo doctor` first — it pinpoints the issue.

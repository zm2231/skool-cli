# Skool CLI

Read any Skool community you belong to from the terminal or from an AI agent: posts with full text, members, the admin roster (emails, plans, renewal dates), leaderboards, courses, calendar events and notifications. It mirrors everything into a local SQLite database, so you can search it and run SQL across every community you've synced.

This is a fork of the Skool CLI from [CLI Printing Press](https://github.com/mvanhorn/printing-press-library) by Zain Haseeb ([@quoxientzero](https://github.com/quoxientzero)). The fork adds:

- `members roster`, the admin member export, with emails, price, billing period end, trial status and churn
- fixes for Skool's current routes (members tab, the `group` parameter, buildId rotation)
- `upgrade`, which updates the CLI in place from GitHub releases
- prebuilt binaries for macOS, Linux and Windows

**It only reads.** Posting, commenting, liking and moderation are not included: Skool's write endpoints changed, and they will come back once they are verified against the live site. Post from the browser for now.

## Install

macOS or Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/zm2231/skool-cli/main/install.sh | sh
```

This installs `skool-pp-cli` and `skool-pp-mcp` into `~/.local/bin` after checking them against the release checksums. Set `SKOOL_INSTALL_DIR` to put them somewhere else.

Windows: download the zip for your machine from the [latest release](https://github.com/zm2231/skool-cli/releases/latest) and put both `.exe` files somewhere on your `PATH`.

With Go 1.26.6 or newer:

```bash
go install github.com/zm2231/skool-cli/cmd/skool-pp-cli@latest
```

## Upgrade

```bash
skool-pp-cli upgrade --check   # is there a newer release?
skool-pp-cli upgrade           # download, verify, replace in place
```

`upgrade` also updates `skool-pp-mcp` when it sits in the same folder. A binary installed with `go install` is upgraded the same way, or you can run `go install ...@latest` again.

## Log in

Log in to skool.com in Chrome, then run:

```bash
skool-pp-cli auth login --chrome          # add --profile "Work" for a non-default Chrome profile
skool-pp-cli auth status
skool-pp-cli doctor
```

The CLI reads your `auth_token` cookie from Chrome and saves it to `~/.config/skool-pp-cli/config.toml`. It never stores your password. When the cookie expires, run the login command again.

Your community slug is the part of the URL after `skool.com/`. Pass it with `--community <slug>`, or set `SKOOL_COMMUNITY` once.

## What you can do

```bash
# Pull posts and members into the local database
skool-pp-cli sync --community <slug>

# The week's best posts, with full text
skool-pp-cli posts top --community <slug> --since 7d --top 10 --by engagement --json

# What's new since yesterday
skool-pp-cli digest since 24h --community <slug> --json

# Admin roster: emails, plan, renewal date, trial, churn (group admins only)
skool-pp-cli members roster --community <slug> --refresh --json
skool-pp-cli members roster --community <slug> --refresh --expiring 7 --csv > renewals.csv
skool-pp-cli members roster --community <slug> --status cancelling,churned --refresh --json

# Leaderboard with levels and bios
skool-pp-cli leaderboard --community <slug> --top 25 --json

# A whole course as markdown, one file per lesson
skool-pp-cli classroom export <course-slug> --community <slug> --out ./course

# Community calendar as .ics
skool-pp-cli calendar export --community <slug> --ics > events.ics

# Search and SQL over everything you've synced
skool-pp-cli search "onboarding" --limit 10
skool-pp-cli sql "SELECT resource_type, COUNT(*) FROM resources GROUP BY resource_type"
```

Run `skool-pp-cli --help`, or `skool-pp-cli which "<what you want>"`, to see everything.

`--json` returns full content. `--agent` and `--compact` trim posts down to ids and names, so use plain `--json` whenever you need the text.

## Commands

| Group | Commands |
|---|---|
| posts | `list`, `get`, `top` |
| members | `list`, `pending`, `roster` |
| community | `info`, `about`, `members-tab`, `leaderboard-tab` |
| classroom | `list`, `get-course`, `export` |
| calendar | `calendar`, `export` |
| notifications | `list` |
| local data | `sync`, `search`, `sql`, `digest since`, `export`, `tail`, `leaderboard` |
| setup | `auth`, `doctor`, `upgrade`, `version`, `which`, `agent-context`, `profile` |

## Use it from an agent

`SKILL.md` is an agent skill for Claude Code, Codex and similar tools. Copy it into your agent's skills folder (for Claude Code, `~/.claude/skills/skool/SKILL.md`).

`skool-pp-mcp` is an MCP server exposing the same reads. For Claude Desktop, add this to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "skool": { "command": "/Users/<you>/.local/bin/skool-pp-mcp" }
  }
}
```

## Troubleshooting

- **Exit code 4, or 401/403:** your cookie expired. Run `skool-pp-cli auth login --chrome`.
- **`404 {"notFound":true}`:** Skool rotates an internal build id on every deploy. The CLI refetches it and retries once on its own; if it still fails, check the community or course slug.
- **`sync` shows 0 records:** check `--community`, then run `skool-pp-cli doctor`.
- **`members roster` has no emails:** only group admins can see billing data.

Exit codes: `0` success, `2` usage, `3` not found, `4` auth, `5` API, `7` rate limited, `10` config.

## License

Apache-2.0. See `LICENSE` and `NOTICE`.

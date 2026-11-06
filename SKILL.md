---
name: skool
description: "Read Skool communities through the skool-pp-cli binary: posts with full text, top posts, members, the admin roster (emails, plan, renewal date, trial, churn), leaderboards, courses exported to markdown, calendar events, notifications, plus a local SQLite mirror for search and SQL. Use when the user mentions Skool, their Skool community or classroom, members, renewals or churn, a community digest, or a skool.com link. Read-only: it cannot post, comment, like or moderate."
license: "Apache-2.0"
allowed-tools: "Read Bash"
---

# Skool CLI

Drives `skool-pp-cli` (github.com/zm2231/skool-cli). It only reads. For posting, commenting or moderating, tell the user to do it in the browser.

## Before the first command

1. Check it's installed: `skool-pp-cli --version`. If not, install it (macOS/Linux):
   `curl -fsSL https://raw.githubusercontent.com/zm2231/skool-cli/main/install.sh | sh`
   On Windows, download the zip from https://github.com/zm2231/skool-cli/releases/latest.
2. Check login: `skool-pp-cli doctor`. If auth fails, the user logs in to skool.com in Chrome and you run `skool-pp-cli auth login --chrome` (add `--profile "<name>"` for a non-default Chrome profile). Never ask for their password or cookie.
3. Find the community slug: the part of the URL after `skool.com/`. `skool-pp-cli me get --json` lists the communities they've joined. Pass it as `--community <slug>`.
4. Now and then, run `skool-pp-cli upgrade --check`. If a newer release exists, offer `skool-pp-cli upgrade`.

## Use `--json`, not `--agent`

`--agent` and `--compact` drop post bodies, upvote counts and author names. Use plain `--json` whenever content matters. `--select a,b.c` keeps only the fields you name.

## Recipes

```bash
# Hydrate the local mirror (search and sql read it)
skool-pp-cli sync --community <slug>

# Best posts this week, full text
skool-pp-cli posts top --community <slug> --since 7d --top 10 --by engagement --json
#   --by: engagement (upvotes + 3x comments), upvotes, comments, newest

# One post with its comment tree (the post's id comes from posts top / posts list)
skool-pp-cli posts get <post-id> --json

# What's new since a time
skool-pp-cli digest since 24h --community <slug> --json

# Admin roster (group admins only): email, price, billing period end, trial, churn
skool-pp-cli members roster --community <slug> --refresh --json
skool-pp-cli members roster --community <slug> --refresh --expiring 7 --json
skool-pp-cli members roster --community <slug> --status cancelling,churned,banned --refresh --json
skool-pp-cli members roster --community <slug> --refresh --csv --deliver file:./roster.csv

# Leaderboard with level and bio
skool-pp-cli leaderboard --community <slug> --top 25 --json

# A course as markdown, one file per lesson
skool-pp-cli classroom list --community <slug> --json
skool-pp-cli classroom export <course-slug> --community <slug> --out ./course

# Calendar
skool-pp-cli calendar export --community <slug> --ics > events.ics

# Search and SQL over the mirror (table: resources, with resource_type and data JSON)
skool-pp-cli search "pricing" --limit 10 --json
skool-pp-cli sql "SELECT resource_type, COUNT(*) FROM resources GROUP BY resource_type" --json
```

Not sure which command does something? `skool-pp-cli which "<what you want>"`, then `skool-pp-cli <command> --help`.

## Notes

- One login covers every community the user belongs to; switch with `--community`.
- `--deliver file:<path>` or `--deliver webhook:<url>` sends output somewhere other than stdout, which is handy for scheduled jobs.
- Roster email comes from billing for paying members and from the invite for free members. `period_end` is empty when there's no subscription.
- `404 {"notFound":true}` means Skool rotated its build id. The CLI retries once on its own; if it persists, the slug is wrong.
- Exit codes: `0` ok, `2` usage, `3` not found, `4` auth (log in again), `5` API, `7` rate limited, `10` config.
- Member emails and billing data are private. Don't paste them anywhere public, and ask before sending them to another tool.

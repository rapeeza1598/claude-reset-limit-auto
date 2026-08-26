**English** | [ภาษาไทย](i18n/README.th.md)

# claude-reset-limit-auto

[![CI](https://github.com/rapeeza1598/claude-reset-limit-auto/actions/workflows/ci.yml/badge.svg)](https://github.com/rapeeza1598/claude-reset-limit-auto/actions/workflows/ci.yml)

A Docker container that fires a short prompt at Claude (via the Claude Code CLI) on a daily schedule, so your subscription's (Pro/Max) rolling 5-hour usage limit resets at predictable times — instead of depending on whenever you happen to start chatting with Claude.

**Ping times (Asia/Bangkok):** 06:00, 11:00, 16:00, 21:00 — 5 hours apart, except the 21:00→06:00 overnight gap, which is 9 hours.

Before every ping, it checks whether "this hour slot" has already been pinged (via a state file). If so, it skips — no duplicate pings.

## How it works (important — read before using)

- Uses **`CLAUDE_CODE_OAUTH_TOKEN`** (a 1-year token tied to your Pro/Max subscription) — **not** `ANTHROPIC_API_KEY`
- Why: `ANTHROPIC_API_KEY` is a pay-per-token Console account, entirely separate from the 5-hour limit you see in Claude Code/claude.ai. Using an API key would ping the wrong account and do nothing to reset your subscription's limit.
- **Never set `ANTHROPIC_API_KEY` anywhere in this project.** The Claude Code CLI always prefers `ANTHROPIC_API_KEY` over `CLAUDE_CODE_OAUTH_TOKEN` (auth precedence) — if both are present, it silently pings the wrong account with no error at all.
- The container runs as a non-root user (`appuser`) — using [`supercronic`](https://github.com/aptible/supercronic) instead of system cron, since supercronic needs no root (it runs as a plain foreground process, with none of traditional cron's privilege-drop machinery that requires writing to `/etc/cron.d`) and inherits the container's environment variables directly, no workaround needed.
- `curl`/`openssl`/`libcurl4` and the whole TLS dependency chain used to download the Claude CLI are isolated in a separate build stage (`installer`) and never installed in the image that actually runs — reducing attack surface / CVE count in the final image.

## Prerequisites

- Docker + Docker Compose
- A Claude Pro or Max subscription, and `claude login` already done on the machine you'll run `claude setup-token` on (doesn't have to be the same machine that runs the container)

## Setup

### 1. Generate a long-lived token

Run this on a Mac already logged into Claude Code (it's an interactive command — it'll open a browser to authorize):

```bash
claude setup-token
```

The token prints to the terminal (nothing is saved to a file automatically — copy it yourself). This token is valid for 1 year.

### 2. Configure the environment

```bash
cp .env.example .env
```

Open `.env` and paste your token into this line:

```
CLAUDE_CODE_OAUTH_TOKEN=<paste your copied token here>
```

**Do not add an `ANTHROPIC_API_KEY=` line to this file** (see why above).

### 3. Build and run the container

```bash
docker compose build
docker compose up -d
```

The container stays running (`restart: unless-stopped`), and the cron inside it fires `claude -p "hi"` automatically at 06:00 / 11:00 / 16:00 / 21:00 (Asia/Bangkok) every day.

## Verification

Follow this sequence after the first build:

```bash
# 1. Confirm the claude binary is actually on PATH
docker compose exec claude-reset which claude

# 2. Confirm the token reaches the container's environment
docker compose exec claude-reset printenv CLAUDE_CODE_OAUTH_TOKEN

# 3. Run reset manually to confirm it actually works
docker compose exec claude-reset /app/reset
# should see a JSON log line, e.g. {"level":"INFO","msg":"ping ok","slot":"...","duration_ms":...}

# 4. Run it again immediately — it must "skip", not ping again
docker compose exec claude-reset /app/reset
# should see {"level":"INFO","msg":"skip","slot":"...","reason":"already pinged"}

# 5. Check the persisted log (survives restarts since it's on a volume)
cat data/reset.log

# 6. Confirm it's actually running as non-root
docker compose exec claude-reset whoami
# should be appuser, not root
```

If step 3 errors on auth (e.g. an expired or invalid token), the state file won't be written, so the next scheduled slot will retry — it never gets stuck on a false "success" state.

## Changing the schedule

Ping times are set via `RESET_HOURS` in `.env` (not hardcoded into the image) — the container regenerates the cron schedule from this value every time it starts.

```
RESET_HOURS=6,11,16,21
```

Edit this (comma-separated hours, 0-23, Thailand time) and restart the container — **no rebuild needed**:

```bash
docker compose restart
```

Confirm the new value actually took effect:

```bash
docker compose exec claude-reset cat /app/crontab
```

## Log and state file

Both files live on the `./data` volume (mounted from the host) so they survive container restart/recreation:

| File              | Purpose                                                                                     |
| ----------------- | --------------------------------------------------------------------------------------------- |
| `data/last_slot`  | Remembers the last hour slot (format `YYYY-MM-DD_HH`) that was pinged successfully — prevents duplicate pings |
| `data/reset.log`  | Line-delimited JSON (JSON Lines) log of every run, whether it pinged or skipped              |

You can also tail logs live from the container:

```bash
docker compose logs -f
```

## Common commands

```bash
docker compose ps            # check whether the container is running / not crashed
docker compose logs -f        # follow logs live
docker compose down           # stop and remove the container (data/ is untouched)
docker compose up -d --build  # rebuild and rerun (e.g. after code changes)
```

## Troubleshooting

- **`which claude` can't find the binary** — the Claude Code install script may have changed its install location in a newer version. Update the `ENV PATH="/home/appuser/.local/bin:${PATH}"` line in `Dockerfile` to match the real location, then rebuild.
- **cron (supercronic) fires but the log is missing `CLAUDE_CODE_OAUTH_TOKEN`** — shouldn't happen, since supercronic runs as a plain process (no privilege-drop like traditional cron) and its jobs inherit the container's environment directly. If you hit this, confirm `.env` is actually loaded with `docker compose config`.
- **Ping times look shifted from what you set (7-hour offset)** — wrong timezone. Check with `docker compose exec claude-reset date`; it should match the current time in Thailand.
- **Want to test that cron actually fires without waiting for the scheduled hour** — temporarily set `RESET_HOURS` in `.env` to the next upcoming hour, `docker compose restart` (no rebuild needed), watch `docker compose logs -f`, then don't forget to change it back to `6,11,16,21` afterward.

## Deliberately out of scope

- No automatic token renewal/rotation (the token lasts 1 year — you need to run `claude setup-token` again yourself before it expires)
- No log rotation (at ~4 small JSON lines a day, it'd take years to become a large file)
- No alerting when a ping fails — you need to check `docker compose logs` or `data/reset.log` yourself
- The pre-ping check only asks "have we already pinged this hour slot" (from its own state file) — it doesn't check Claude's actual rate-limit state. If you're already using Claude normally during that window, the scheduled ping may have no additional effect (since the window is already open from real usage) — but it doesn't cost anything beyond a small amount of quota either.

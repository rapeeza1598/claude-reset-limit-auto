# Knowledge base

What was built, why each decision was made, what broke along the way, and what is still open. For how to use the service, see [README.md](../README.md). For how the code is laid out, see [CLAUDE.md](../CLAUDE.md). This file covers what neither of those explains: the reasons and the lessons.

Last updated: 2026-11-18.

## Timeline

| Date | Change | Commit / PR |
|---|---|---|
| 2026-08-25 | First version: Go ping binary, supercronic, multi-stage Dockerfile, `entrypoint.sh` | `0db6631` |
| 2026-08-25 | Drop unused `STATE_FILE`/`LOG_FILE` env overrides and a dead `MkdirAll` | `6cd9f82` |
| 2026-08-25 | CI: `go vet`, `go test`, `docker build` on push/PR. Module cache disabled. checkout/setup-go moved to v7 | `20c02ad`, `68abcce`, `870ce5f` |
| 2026-08-25 | Force LF line endings (`.gitattributes`) | `d377061` |
| 2026-08-25 | MIT license, Dependabot (actions + docker), CI badge | `aea5b63` |
| 2026-08-25 | Least-privilege `GITHUB_TOKEN` in CI (Copilot code-scanning fix) | PR #1 |
| 2026-08-25 | Weekly workflow that bumps `SUPERCRONIC_VERSION` | `b3ff56c`, `eae3775` |
| 2026-08-26 | README made English-primary; Thai moved to `i18n/README.th.md` | `25c35fc` |
| 2026-10-30 | Runtime moves to distroless; `entrypoint.sh` replaced by `/app/reset -init`; supercronic v0.2.51 | PR #2 (`d3314bf`) |
| 2026-11-18 | This knowledge base added; `go.mod` directive moved from 1.23 to 1.27; GitHub Actions workflows (CI, supercronic bump) and the CI badge removed | PR #3 |
| 2026-11-18 | 5-minute ping timeout; slimmer ping (`--tools ""`, `--no-session-persistence`); supercronic `-json -passthrough-logs`; CLI nonessential traffic off; `cap_drop: ALL` + `no-new-privileges`; `.dockerignore` | PR #4 |
| 2026-11-18 | Late cron fires (host was asleep) are skipped via `/app/reset -scheduled`; `RESET_HOURS` validated; README section on keeping the (Windows) host awake | `skip-late-fires` branch |

## Design decisions

### Auth: `CLAUDE_CODE_OAUTH_TOKEN`, never `ANTHROPIC_API_KEY`
- **Decision:** authenticate with the 1-year token from `claude setup-token`.
- **Why:** the 5-hour window belongs to the Pro/Max subscription. An API key bills a separate Console account, so the ping would never touch the window. The CLI also prefers `ANTHROPIC_API_KEY` whenever both are set, so a stray key silently redirects every ping with no error.

### State file holds the slot, written only on success
- **Decision:** `/data/last_slot` stores `YYYY-MM-DD_HH`. The file is written only after `claude -p "hi"` exits 0.
- **Why:** a failed ping (expired token, network error) stays retryable at the next slot instead of being recorded as done. Verified on 2026-08-25: `claude -p` exits non-zero on a 401.
- **Limit:** the file only knows whether this tool already pinged in that hour. It can't see real rate-limit state. That's deliberate (see the README's out-of-scope list).

### supercronic instead of system cron
- **Decision:** run [supercronic](https://github.com/aptible/supercronic) as the non-root `appuser`.
- **Why:** system cron needs root to write `/etc/cron.d` and drop privileges per job. A Docker scanner flagged "runs as root". The first response was a comment accepting the risk; the user wanted a real fix instead. supercronic fixed the warning, and because jobs inherit its environment directly, the `/etc/environment` passthrough hack went away too.

### Multi-stage build with an isolated installer stage
- **Decision:** `builder` (golang alpine) → `installer` (debian trixie-slim + curl) → runtime.
- **Why:** curl brings a large TLS dependency chain (openssl, libcurl4, krb5, ldap, …). None of it ships in the runtime image; only the downloaded binaries are copied out.
- **Gotcha:** the Claude installer symlinks `~/.local/bin/claude` to an *absolute* path under `~/.local/share/claude/versions/<v>`. The installer stage therefore runs as the same `appuser` with the same home path the runtime uses. Otherwise the copied symlink dangles.
- **Builder on alpine:** fewer CVEs, and it doesn't matter for the shipped image because `CGO_ENABLED=0` produces a static binary.

### Distroless runtime (PR #2)
- **Decision:** the final stage is `gcr.io/distroless/base-debian13`: glibc, libssl, CA certs, tzdata and DNS config, with no shell, package manager or coreutils.
- **The problem it created:** supercronic runs every job as `exec.Command(shell, "-c", command)`, and `shell` defaults to `/bin/sh`, which doesn't exist in this image.
- **The fix:** supercronic reads a `SHELL=` line from the crontab file. `/app/reset -init` writes `SHELL=/app/reset`, so each job runs as `/app/reset -c /app/reset`. `main.go` handles `-c` by splitting the command on whitespace and exec'ing it.
- **`entrypoint.sh` replaced:** `-init` regenerates the crontab from `RESET_HOURS` on every start, then execs supercronic. If extra args are passed (`docker compose run … <cmd>`), it execs those instead.
- **No `mkdir`/`chown` in the final stage:** `/app` and `/data` are created and chowned in the installer stage, then copied as whole directories. `COPY --chown` applies only to the copied files, not to parent directories it creates along the way.

### Ping hardening (2026-11-18)
- **Timeout:** `claude -p` gets 5 minutes. supercronic never starts a job while the previous run is still going, so a hung ping would silently block every later slot. `cmd.WaitDelay` stops leftover child processes from holding `Wait` open past the timeout.
- **Smaller ping:** `--tools ""` drops the tool definitions from the prompt, and `--no-session-persistence` skips writing a session file. The model is unchanged (see open items).
- **Logs:** supercronic runs with `-json -passthrough-logs`. Before this, its log lines were plain text and the job's JSON was escaped inside its `msg` field.
- **Container:** `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` (no autoupdater or telemetry), `cap_drop: [ALL]` and `no-new-privileges` in compose. `.dockerignore` keeps `.env` (the token), `data/` and `.git` out of the build context.

### Schedule comes from `RESET_HOURS` at runtime
- **Decision:** the crontab is generated at container start, not baked into the image.
- **Why:** changing the schedule needs only `docker compose restart`, with no rebuild.
- **Validation:** `parseHours()` rejects anything that isn't a comma-separated list of hours 0–23, and the container exits with `invalid RESET_HOURS`. Before 2026-11-18 a bad value silently produced a broken crontab. Spaces are trimmed, and the crontab is written in normalized form.

### Late fires are skipped (2026-11-18)
- **Decision:** the crontab runs `/app/reset -scheduled`. With that flag, `onTime()` pings only when the run is less than `lateGrace` (10 min) past an hour in `RESET_HOURS`. Otherwise it logs `"reason":"late fire, host was probably asleep"`.
- **Early fires count:** `slotStart()` assigns a run up to `earlyGrace` (1 min) before an hour to that hour, for both `onTime()` and the slot key. Without this, the first version would have skipped most pings on the Windows host (see the lessons below).
- **Why:** see the sleeping-host lesson below. A missed slot is better than a window that starts at a random hour.
- **Manual runs** (`/app/reset` with no flag) skip the check, so README verification still works at any hour.
- **Cost:** a host that sleeps through a slot now misses that slot entirely. The real fix is keeping the host awake (README, "Keeping the host awake").

### No CI (workflows removed 2026-11-18)
- **Decision:** delete `.github/workflows/ci.yml` and `update-supercronic.yml`, the README badge, and Dependabot's `github-actions` entry.
- **Why:** from 2026-11-09 no Actions job started on this account ("The job was not started because your account is locked due to a billing issue"). Other repos on the account were hit too, so this wasn't a problem with this repo's config. The bump workflow also couldn't open PRs, because the repo setting allowing Actions to create PRs was off.
- **Instead:** run `go vet ./...`, `go test ./...` and `docker build .` locally before pushing, and bump `ARG SUPERCRONIC_VERSION` by hand from the [supercronic releases](https://github.com/aptible/supercronic/releases). Dependabot still watches the Docker `FROM` lines.
- **If CI comes back:** restore the files from git history (`git show 50d366f:.github/workflows/ci.yml`). Two details worth keeping: `setup-go` needs `cache: false` because the project has no `go.sum`, and the bump workflow runs `docker build` itself because PRs opened with the default `GITHUB_TOKEN` don't trigger other workflows.
- `.gitattributes` forces LF. It was added when `entrypoint.sh` existed: a CRLF checkout on Windows broke its shebang and crash-looped the container. There are no shell scripts now, but the rule is harmless to keep.

## Lessons and gotchas

- **Never test with a real ping.** Running `/app/reset` with the real token starts the 5-hour window at an unscheduled hour, which is exactly what this tool exists to prevent. Use a bogus token (see the recipes below).
- **`syscall.Exec` doesn't search `PATH`.** `execOrExit` calls `exec.LookPath` first. Without it, `docker compose run --rm claude-reset claude --version` fails with "no such file or directory".
- **The distroless image has no `which`, `printenv`, `whoami`, `cat` or `date`.** Use these instead:

  | Old command | Works now |
  |---|---|
  | `exec … which claude` | `docker compose exec claude-reset claude --version` |
  | `exec … printenv CLAUDE_CODE_OAUTH_TOKEN` | `docker compose config \| grep CLAUDE_CODE_OAUTH_TOKEN` (`findstr` on Windows) |
  | `exec … whoami` | `docker compose top` (UID should be 1000) |
  | `exec … cat /app/crontab` | `docker compose logs claude-reset \| grep "crontab written"`. This works on Windows too (with `findstr`), unlike `docker compose cp … - \| tar -xO`, whose binary pipe Windows PowerShell 5.1 can mangle |
  | `exec … date` | the `time` field in `data/reset.log` should end in `+07:00` |

- **Never pass `--bare` to `claude`.** Its help text says it reads only `ANTHROPIC_API_KEY` (or an `apiKeyHelper`), never OAuth, so every ping would fail.
- **A sleeping host makes pings fire late.** When Docker's VM is paused (Mac or Windows asleep), supercronic runs the overdue job as soon as it wakes. Logs from 2026-09-09 to 09-27 show 9 cron fires: 5 in the wrong hour (23:10, 14:18, 12:01, 10:51, 17:08) and 1 that was 43 minutes late, with supercronic logging `job took too long to run: it should have started 65h10m45s ago`. A late ping starts the window at the wrong time and can turn the next scheduled ping into a no-op. These fires are now skipped (see "Late fires are skipped").
- **On the Windows host, supercronic fires a few seconds early.** Logs from 2026-10-30 to 11-18 (Docker Desktop on WSL2) show most jobs starting at 05:59:58, 10:59:57, 15:59:59 or 20:59:59, at most about 3s before the hour. One ran 6s late. Two consequences in the version deployed then: the slot key came from the fire time, so an 11:00 ping was logged as slot `..._10`; and a naive "hour must be in `RESET_HOURS`" check would have skipped those pings. Both are handled by `slotStart()`. Likely cause: the WSL2 VM's clock is periodically corrected to the host's, while supercronic sleeps on the monotonic clock.
- **The Windows host is reliable.** From the 2026-10-30 deploy to 11-18, every 06/11/16/21 slot was pinged. The container restarted five times (e.g. Docker Desktop restarts), always between slots, so no slot was lost.
- **Local Docker builds can be slow or flaky.** This machine's network is unreliable for large layer downloads. Retry before assuming a real bug; the layer cache makes retries cheap. Docker Desktop may also need starting first (`open -a Docker`).

## Safe test recipes

None of these touch the real account or the `data/` folder. `docker run` without `-v` gets a throwaway anonymous volume.

```bash
IMG=claude-reset-limit-auto-claude-reset
docker compose build

# claude resolves through PATH (tests the LookPath fix and the -init debug path)
docker run --rm $IMG claude --version

# the exact command supercronic runs on schedule; expect "ping failed" with a 401
docker run --rm -e CLAUDE_CODE_OAUTH_TOKEN=bogus $IMG /app/reset -c /app/reset

# full service: supercronic should log "processes will be spawned using shell: /app/reset"
docker run -d --name crl-test -e CLAUDE_CODE_OAUTH_TOKEN=bogus -e RESET_HOURS=3 $IMG
docker logs crl-test
docker cp crl-test:/app/crontab - | tar -xO
docker exec crl-test /usr/local/bin/supercronic -test /app/crontab
docker top crl-test
docker rm -f crl-test
```

Not yet run end to end: a real scheduled fire at minute 0. The command it runs is covered by the second recipe.

## Open items (as of 2026-11-18)

- **CodeQL default setup was turned off on 2026-11-18.** It's a repo setting, not a workflow file, and it was failing on every PR while the account is locked. Once billing is fixed, turn it back on in Settings → Code security.
- **The production host is a Windows PC (Docker Desktop on WSL2).** It must stay awake at the scheduled hours. Late fires are skipped, so sleep means missed slots, not wrong ones. See README, "Keeping the host awake".
- **Smaller model for the ping (not done yet).** `--model haiku` is accepted by CLI 2.1.327. Confirm with one real ping at a scheduled hour that it starts the 5-hour window before switching.
- **The OAuth token expires one year after `claude setup-token`.** Nothing renews or alerts on it (out of scope). Run `claude setup-token` again before it expires and update `.env`.
- **`RESET_HOURS` isn't validated** (see above).

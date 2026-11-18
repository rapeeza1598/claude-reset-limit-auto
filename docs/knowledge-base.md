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
| 2026-11-18 | This knowledge base added; `go.mod` directive moved from 1.23 to 1.27. Actions (major tags) and supercronic were already current | PR #3 |

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

### Schedule comes from `RESET_HOURS` at runtime
- **Decision:** the crontab is generated at container start, not baked into the image.
- **Why:** changing the schedule needs only `docker compose restart`, with no rebuild.
- **Limit:** the value isn't validated. A malformed value produces a broken crontab, not an error.

### CI and dependency updates
- CI only builds; nothing is published. Nothing pulls this image from a registry, so no registry secrets are needed.
- `setup-go` has `cache: false`. The project is stdlib-only with no `go.sum`, so the cache step only produced a noisy annotation.
- Dependabot handles GitHub Actions and Docker `FROM` lines. It can't track the `ARG SUPERCRONIC_VERSION`, so `.github/workflows/update-supercronic.yml` checks it weekly. That workflow runs `docker build` itself because PRs opened with the default `GITHUB_TOKEN` don't trigger `ci.yml` (GitHub's loop prevention).
- `.gitattributes` forces LF. It was added when `entrypoint.sh` existed: a CRLF checkout on Windows broke its shebang and crash-looped the container. There are no shell scripts now, but the rule is harmless to keep.

## Lessons and gotchas

- **Never test with a real ping.** Running `/app/reset` with the real token starts the 5-hour window at an unscheduled hour, which is exactly what this tool exists to prevent. Use a bogus token (see the recipes below).
- **`syscall.Exec` doesn't search `PATH`.** `execOrExit` calls `exec.LookPath` first. Without it, `docker compose run --rm claude-reset claude --version` fails with "no such file or directory".
- **The distroless image has no `which`, `printenv`, `whoami`, `cat` or `date`.** Use these instead:

  | Old command | Works now |
  |---|---|
  | `exec … which claude` | `docker compose exec claude-reset claude --version` |
  | `exec … printenv CLAUDE_CODE_OAUTH_TOKEN` | `docker compose config \| grep CLAUDE_CODE_OAUTH_TOKEN` |
  | `exec … whoami` | `docker compose top` (UID should be 1000) |
  | `exec … cat /app/crontab` | `docker compose cp claude-reset:/app/crontab - \| tar -xO` |
  | `exec … date` | the `time` field in `data/reset.log` should end in `+07:00` |

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

- **GitHub Actions jobs aren't starting.** The scheduled runs on 2026-11-09 and 2026-11-16 failed with "The job was not started because your account is locked due to a billing issue." Fix the billing in the GitHub account settings. Until then, neither CI nor the supercronic bump workflow runs.
- **The bump workflow can't open PRs.** The repo setting is off (`can_approve_pull_request_reviews: false`). Turn on Settings → Actions → General → "Allow GitHub Actions to create and approve pull requests".
- **The OAuth token expires one year after `claude setup-token`.** Nothing renews or alerts on it (out of scope). Run `claude setup-token` again before it expires and update `.env`.
- **`RESET_HOURS` isn't validated** (see above).

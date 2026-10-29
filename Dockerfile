# alpine builder: far fewer CVEs than the trixie variant, and irrelevant to
# the shipped image either way since CGO_ENABLED=0 produces a static binary
# and only the compiled binary is copied into the runtime stage below.
FROM golang:1-alpine AS builder
WORKDIR /src
COPY main.go go.mod ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /reset-bin .

# installer: fetches the Claude CLI + supercronic, and prepares everything
# the shell-less distroless final stage can't create for itself (no
# useradd/mkdir/chown there). Needs curl and its heavy TLS dependency chain
# (openssl/libcurl4/krb5/ldap/...), but none of that ships in the final
# image below — only the resulting binaries and metadata get copied out.
#
# The Claude installer links ~/.local/bin/claude to an *absolute* path under
# ~/.local/share/claude/versions/<v>, so it's installed here as the same
# appuser/home layout the final stage uses — copying the tree verbatim would
# otherwise leave a symlink pointing at a path that doesn't exist there.
FROM debian:trixie-slim AS installer
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
      curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

ARG TARGETARCH
# not covered by Dependabot (its docker ecosystem only tracks FROM lines) —
# auto-checked weekly and bumped via PR instead by
# .github/workflows/update-supercronic.yml.
ARG SUPERCRONIC_VERSION=v0.2.51
RUN curl -fsSL -o /usr/local/bin/supercronic \
      "https://github.com/aptible/supercronic/releases/download/${SUPERCRONIC_VERSION}/supercronic-linux-${TARGETARCH}" \
    && chmod +x /usr/local/bin/supercronic

# /appdata/{app,data} become /app and /data in the final stage — created and
# chowned here since the distroless final image has no mkdir/chown to do it
# itself. Copying the whole directory below (not letting COPY auto-create a
# parent for a deeper path) is deliberate: --chown is documented to apply to
# the copied files, not to a parent directory it has to create along the
# way, so this is the only way to guarantee /app and /data themselves end up
# appuser-owned.
RUN useradd --create-home --uid 1000 appuser \
    && mkdir -p /appdata/app /appdata/data \
    && chown -R appuser:appuser /appdata /home/appuser
USER appuser
RUN curl -fsSL https://claude.ai/install.sh | bash

# distroless: no shell, no package manager, no coreutils — just glibc,
# libssl, ca-certificates, tzdata and DNS resolution (nsswitch.conf +
# netbase), all bundled by the image itself.
FROM gcr.io/distroless/base-debian13

COPY --from=installer /etc/passwd /etc/passwd
COPY --from=installer /etc/group /etc/group
COPY --from=installer --chown=appuser:appuser /appdata/app /app
COPY --from=installer --chown=appuser:appuser /appdata/data /data
COPY --from=installer --chown=appuser:appuser /home/appuser /home/appuser
COPY --from=installer /usr/local/bin/supercronic /usr/local/bin/supercronic
COPY --chmod=755 --from=builder /reset-bin /app/reset

ENV TZ=Asia/Bangkok
ENV PATH="/home/appuser/.local/bin:${PATH}"

USER appuser
VOLUME /data
# supercronic (see cron/cron.go + crontab/crontab.go in aptible/supercronic)
# runs every scheduled command as exec.Command(shell, "-c", command), where
# shell comes from a SHELL= line in the crontab file itself, not the process
# environment. /app/reset -init writes that crontab with SHELL=/app/reset,
# so supercronic ends up invoking "/app/reset -c /app/reset" instead of
# "/bin/sh -c /app/reset" — the whole service runs with no shell in the
# image at all.
ENTRYPOINT ["/app/reset", "-init"]

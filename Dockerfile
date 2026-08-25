# alpine builder: far fewer CVEs than the bookworm variant, and irrelevant to
# the shipped image either way since CGO_ENABLED=0 produces a static binary
# and only the compiled binary is copied into the runtime stage below.
FROM golang:1-alpine AS builder
WORKDIR /src
COPY main.go go.mod ./
RUN CGO_ENABLED=0 go build -o /reset-bin .

# installer: fetches the Claude CLI + supercronic. Needs curl and its heavy
# TLS dependency chain (openssl/libcurl4/krb5/ldap/...), but none of that
# ships in the final image below — only the resulting binaries get copied out.
#
# The Claude installer links ~/.local/bin/claude to an *absolute* path under
# ~/.local/share/claude/versions/<v>, so it's installed here as the same
# appuser/home layout the final stage uses — copying the tree verbatim would
# otherwise leave a symlink pointing at a path that doesn't exist there.
FROM debian:bookworm-slim AS installer
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
      curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

ARG TARGETARCH
# manual pin, not covered by Dependabot (its docker ecosystem only tracks
# FROM lines) — check https://github.com/aptible/supercronic/releases
# occasionally and bump by hand.
ARG SUPERCRONIC_VERSION=v0.2.49
RUN curl -fsSL -o /usr/local/bin/supercronic \
      "https://github.com/aptible/supercronic/releases/download/${SUPERCRONIC_VERSION}/supercronic-linux-${TARGETARCH}" \
    && chmod +x /usr/local/bin/supercronic

RUN useradd --create-home --uid 1000 appuser
USER appuser
RUN curl -fsSL https://claude.ai/install.sh | bash

FROM debian:bookworm-slim

RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
      ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

ENV TZ=Asia/Bangkok
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

# supercronic (unlike system cron) runs as a plain foreground process with no
# privilege-dropping model, so it needs no root access — jobs inherit its own
# environment directly, no /etc/environment passthrough hack required either.
RUN useradd --create-home --uid 1000 appuser

COPY --from=installer --chown=appuser:appuser /home/appuser/.local /home/appuser/.local
COPY --from=installer /usr/local/bin/supercronic /usr/local/bin/supercronic
COPY --from=builder /reset-bin /app/reset
COPY entrypoint.sh /app/entrypoint.sh

ENV PATH="/home/appuser/.local/bin:${PATH}"
RUN chmod +x /app/reset /app/entrypoint.sh \
    && mkdir -p /data && chown appuser:appuser /data /app

USER appuser
VOLUME /data
ENTRYPOINT ["/app/entrypoint.sh"]

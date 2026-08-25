#!/bin/bash
set -euo pipefail

RESET_HOURS="${RESET_HOURS:-6,11,16,21}"
echo "0 ${RESET_HOURS} * * * /app/reset" > /app/crontab

# respect an explicit command (e.g. `docker compose run claude-reset sh`) for
# ad-hoc debugging instead of always starting the real cron loop
if [ "$#" -gt 0 ]; then
  exec "$@"
fi

exec supercronic /app/crontab

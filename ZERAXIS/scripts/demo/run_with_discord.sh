#!/usr/bin/env bash
# run_with_discord.sh
# --------------------
# Thin wrapper: runs the existing ZERAXIS demo through the Discord relay.
#
# Usage:
#   ./scripts/demo/run_with_discord.sh [--log PATH]
#
# Environment:
#   DISCORD_WEBHOOK_URL   Discord webhook URL (or set in integrations/discord/.env)
#
# The original run_demo.sh is executed unchanged. Its terminal output is
# preserved and also archived to demo.log. Discord receives milestone embeds.
# The original demo exit code is preserved.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
RELAY="$REPO_ROOT/integrations/discord/relay.py"
VENV="$REPO_ROOT/integrations/discord/.venv"
LOG_ARG="${1:-}"  # optional --log path

# ---- Set up venv on first run -----------------------------------------------
if [ ! -d "$VENV" ]; then
    echo "[discord-relay] Creating Python venv …"
    python3 -m venv "$VENV"
    "$VENV/bin/pip" install -q --upgrade pip
    "$VENV/bin/pip" install -q -r "$REPO_ROOT/integrations/discord/requirements.txt"
fi

# ---- Load .env if present ---------------------------------------------------
ENV_FILE="$REPO_ROOT/integrations/discord/.env"
if [ -f "$ENV_FILE" ]; then
    # python-dotenv handles this; but also export so child processes see it
    set -a
    # shellcheck disable=SC1090
    source "$ENV_FILE"
    set +a
fi

# ---- Forward to relay -------------------------------------------------------
export VIRTUAL_ENV="$VENV"
export PATH="$VENV/bin:$PATH"
exec "$VENV/bin/python3" "$RELAY" ${LOG_ARG:+--log "$LOG_ARG"} \
     -- bash "$REPO_ROOT/scripts/demo/run_demo.sh"

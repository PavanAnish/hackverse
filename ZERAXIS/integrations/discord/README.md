# ZERAXIS → Discord Integration

Sends live milestone notifications and a final summary to a Discord channel
while `run_demo.sh` runs. The original demo is completely unchanged.

## Architecture

```
./scripts/demo/run_demo.sh
          │
          └─► relay.py  (wraps the demo process)
                │  tee
                ├─► Terminal  (unchanged output)
                ├─► demo.log  (full local archive)
                └─► Discord Webhook API
                          │
                          └─► Discord Channel
```

## Quick Start

### 1. Create your Discord webhook

1. Open the Discord channel where you want notifications.
2. **Edit Channel → Integrations → Webhooks → New Webhook**
3. Copy the **Webhook URL**.

### 2. Configure credentials

```bash
cp integrations/discord/.env.example integrations/discord/.env
# Edit .env and paste your webhook URL
```

`.env` is already listed in `.gitignore` — it will never be committed.

### 3. Install Python dependencies

```bash
cd integrations/discord
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

Or let the wrapper handle it automatically on first run (see below).

### 4. Run the demo with Discord notifications

```bash
./scripts/demo/run_with_discord.sh
```

That's it. The wrapper auto-creates the venv on the first run.  
A custom log file path can be specified:

```bash
./scripts/demo/run_with_discord.sh --log /tmp/my-run.log
```

The original demo can still be run without any Discord involvement:

```bash
./scripts/demo/run_demo.sh
```

## Discord Messages Sent

| # | Trigger | Embed colour |
|---|---------|-------------|
| 1 | Demo starts | 🔵 Blue |
| 2 | Suspicious behaviour detected | 🟡 Yellow |
| 3 | Behaviour correlated | 🟡 Yellow |
| 4 | Incident created | 🔴 Red |
| 5 | Response recommended | 🔵 Blue |
| 6 | Human authorization approved | 🟢 Green |
| 7 | Response executed | 🟢 Green |
| 8 | Post-execution verification confirmed | 🟢 Green |
| 9 | Incident resolved | 🟢 Green |
| 10 | Final summary (always sent) | 🟢/🔴 |

Only real milestones from the demo output are sent. If an optional stage
does not occur, its message is not sent.

## Accuracy Policy

- DB counts in the final summary are queried directly from PostgreSQL
  when `ZERAXIS_PG_DSN` is set and are scoped to the current run's
  incident/execution IDs.
- If `ZERAXIS_PG_DSN` is not set, counts show **UNAVAILABLE** — never 0.
- Verification is only reported **VERIFIED** when genuine DB evidence
  supports that status.

## Optional: PostgreSQL summary counts

Set `ZERAXIS_PG_DSN` in `.env` to a **read-only** DSN:

```
ZERAXIS_PG_DSN=postgresql://zeraxis_ro:password@localhost:5432/zeraxis
```

Then install the optional driver:

```bash
pip install psycopg2-binary
```

To create a read-only role in PostgreSQL:

```sql
CREATE ROLE zeraxis_ro WITH LOGIN PASSWORD 'strong-password';
GRANT CONNECT ON DATABASE zeraxis TO zeraxis_ro;
GRANT USAGE ON SCHEMA public TO zeraxis_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO zeraxis_ro;
```

## Testing (offline — no real Discord or DB needed)

```bash
cd integrations/discord
source .venv/bin/activate
pip install pytest
pytest test_relay.py -v
```

## Smoke test (sends one real message to Discord)

```bash
DISCORD_WEBHOOK_URL=<your-url> ZERAXIS_RUN_SMOKE_TEST=1 \
  pytest test_relay.py::TestWebhookSmoke -v
```

## Reliability

| Scenario | Behaviour |
|---------|-----------|
| Discord down | Demo continues; messages queued in `outbox.db` |
| HTTP 429 rate-limit | Waits `Retry-After`, then retries |
| HTTP 5xx transient | Exponential backoff, up to 5 retries |
| HTTP 401 / 404 | Logged; not retried (invalid webhook) |
| Network timeout | Message retained in outbox |
| Duplicate milestone | Suppressed (dedup by title+description) |

## Files

```
integrations/discord/
  relay.py            — Python relay (line parser + webhook sender + outbox)
  requirements.txt    — Python dependencies
  .env.example        — Credential template (safe to commit)
  .env                — Your real credentials (gitignored, never commit)
  test_relay.py       — Offline test suite
  outbox.db           — Auto-created; retains unsent messages (gitignored)
  .venv/              — Auto-created Python virtual environment (gitignored)

scripts/demo/
  run_with_discord.sh — Drop-in demo wrapper with Discord integration
  run_demo.sh         — Original demo (unchanged)
  cleanup_demo.sh     — Original cleanup (unchanged)
```

## Security notes

- The webhook URL is read from the environment only — never hardcoded or logged.
- No Docker socket access.
- No PostgreSQL writes — read-only queries only when `ZERAXIS_PG_DSN` is set.
- No changes to ZERAXIS detection, decision, or remediation logic.
- Discord outages cannot prevent the demo from running.

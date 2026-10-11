#!/usr/bin/env python3
"""
ZERAXIS → Discord Live Run Card
================================
Wraps ./scripts/demo/run_demo.sh, tees all output to demo.log, and
maintains ONE persistent Discord embed that is edited in-place as each
lifecycle stage is recorded.

Usage (via run_with_discord.sh):
    python3 relay.py [--log demo.log] -- ./scripts/demo/run_demo.sh

Environment:
    DISCORD_WEBHOOK_URL   Discord Incoming Webhook URL (required for delivery)
    ZERAXIS_PG_DSN        Optional read-only Postgres DSN for DB-backed counts
"""

from __future__ import annotations

import argparse
import datetime
import json
import logging
import os
import re
import sqlite3
import subprocess
import sys
import threading
import time
import uuid
from pathlib import Path
from typing import Optional

import requests
from dotenv import load_dotenv

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------
load_dotenv(dotenv_path=Path(__file__).parent / ".env")
load_dotenv()  # also try cwd

WEBHOOK_URL: str = os.environ.get("DISCORD_WEBHOOK_URL", "")
LOG_FILE: str = "demo.log"
OUTBOX_FILE: str = str(Path(__file__).parent / "outbox.db")

TIMEOUT_S: float = 10.0
MAX_RETRIES: int = 5
BASE_BACKOFF_S: float = 1.0
MAX_BACKOFF_S: float = 60.0

# Embed colours (decimal)
COLOR_RUNNING   = 16098851   # amber
COLOR_COMPLETE  = 3066993    # green
COLOR_FAILED    = 15158332   # red
COLOR_INCOMPLETE= 16098851   # amber (same as running)
COLOR_ERROR     = 15158332   # red

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [relay] %(levelname)s %(message)s",
    datefmt="%H:%M:%S",
)
log = logging.getLogger("relay")

# ---------------------------------------------------------------------------
# Outbox — unchanged from original, extended with message_id tracking
# ---------------------------------------------------------------------------

class Outbox:
    """Persist unsent edit payloads and the Discord message ID for the run."""

    def __init__(self, path: str):
        self.conn = sqlite3.connect(path, check_same_thread=False)
        self.lock = threading.Lock()
        with self.lock:
            self.conn.executescript("""
                CREATE TABLE IF NOT EXISTS outbox (
                    id          INTEGER PRIMARY KEY AUTOINCREMENT,
                    run_id      TEXT    NOT NULL,
                    payload     TEXT    NOT NULL,
                    created_at  TEXT    NOT NULL,
                    attempts    INTEGER NOT NULL DEFAULT 0,
                    sent        INTEGER NOT NULL DEFAULT 0
                );
                CREATE TABLE IF NOT EXISTS run_messages (
                    run_id      TEXT PRIMARY KEY,
                    message_id  TEXT NOT NULL,
                    created_at  TEXT NOT NULL
                );
            """)
            self.conn.commit()

    def enqueue(self, run_id: str, payload: dict) -> int:
        with self.lock:
            cur = self.conn.execute(
                "INSERT INTO outbox (run_id, payload, created_at) VALUES (?,?,?)",
                (run_id, json.dumps(payload), _utcnow()),
            )
            self.conn.commit()
            return cur.lastrowid

    def mark_sent(self, row_id: int):
        with self.lock:
            self.conn.execute("UPDATE outbox SET sent=1 WHERE id=?", (row_id,))
            self.conn.commit()

    def increment_attempts(self, row_id: int):
        with self.lock:
            self.conn.execute("UPDATE outbox SET attempts=attempts+1 WHERE id=?", (row_id,))
            self.conn.commit()

    def pending(self, run_id: str):
        with self.lock:
            return self.conn.execute(
                "SELECT id, payload, attempts FROM outbox WHERE run_id=? AND sent=0 ORDER BY id",
                (run_id,),
            ).fetchall()

    def save_message_id(self, run_id: str, message_id: str):
        with self.lock:
            self.conn.execute(
                "INSERT OR REPLACE INTO run_messages (run_id, message_id, created_at) VALUES (?,?,?)",
                (run_id, message_id, _utcnow()),
            )
            self.conn.commit()

    def get_message_id(self, run_id: str) -> Optional[str]:
        with self.lock:
            row = self.conn.execute(
                "SELECT message_id FROM run_messages WHERE run_id=?", (run_id,)
            ).fetchone()
        return row[0] if row else None


# ---------------------------------------------------------------------------
# Discord HTTP — POST (create) + PATCH (edit)
# ---------------------------------------------------------------------------

class DiscordCard:
    """
    Manages one persistent Discord message per run.

    POST  webhook?wait=true  → create, capture message_id
    PATCH webhook/messages/{id} → edit in-place
    """

    def __init__(self, url: str, outbox: Outbox, run_id: str):
        self.url = url
        self.outbox = outbox
        self.run_id = run_id
        self._message_id: Optional[str] = outbox.get_message_id(run_id)

    @property
    def message_id(self) -> Optional[str]:
        return self._message_id

    # ---- internal HTTP with retry / rate-limit ----------------------------

    def _http(self, method: str, url: str, payload: dict) -> Optional[requests.Response]:
        if not self.url:
            return None
        backoff = BASE_BACKOFF_S
        for attempt in range(1, MAX_RETRIES + 1):
            try:
                resp = requests.request(
                    method, url, json=payload,
                    timeout=TIMEOUT_S,
                    headers={"Content-Type": "application/json"},
                )
            except requests.exceptions.Timeout:
                log.warning("Discord timeout (attempt %d/%d)", attempt, MAX_RETRIES)
            except requests.exceptions.RequestException as exc:
                log.warning("Discord network error (attempt %d/%d): %s", attempt, MAX_RETRIES, exc)
            else:
                if resp.status_code == 429:
                    retry_after = float(resp.headers.get("Retry-After", backoff))
                    log.warning("Rate limited — waiting %.1fs", retry_after)
                    time.sleep(retry_after)
                    continue
                if resp.status_code in (401, 403, 404):
                    log.error("Discord rejected request HTTP %d — check webhook URL.", resp.status_code)
                    return None
                if resp.status_code >= 500:
                    log.warning("Discord server error %d (attempt %d/%d)", resp.status_code, attempt, MAX_RETRIES)
                elif resp.status_code not in (200, 204):
                    log.warning("Unexpected HTTP %d — not retrying.", resp.status_code)
                    return None
                else:
                    return resp

            if attempt < MAX_RETRIES:
                sleep_s = min(backoff, MAX_BACKOFF_S)
                log.info("Retrying in %.1fs …", sleep_s)
                time.sleep(sleep_s)
                backoff *= 2

        log.error("All %d delivery attempts failed.", MAX_RETRIES)
        return None

    # ---- public API -------------------------------------------------------

    def create(self, embed: dict) -> bool:
        """POST with ?wait=true, capture message_id. Returns True on success."""
        if not self.url:
            log.warning("DISCORD_WEBHOOK_URL not set — card not created.")
            return False
        if self._message_id:
            log.warning("Message already exists for run %s — editing instead.", self.run_id[:8])
            return self.edit(embed)

        post_url = self.url + "?wait=true"
        components = embed.pop("_components", None) if isinstance(embed, dict) else None
        payload = _wrap_embed(embed, components=components)
        row_id = self.outbox.enqueue(self.run_id, payload)
        resp = self._http("POST", post_url, payload)
        if resp is None:
            return False
        try:
            data = resp.json()
            self._message_id = str(data["id"])
            self.outbox.save_message_id(self.run_id, self._message_id)
            self.outbox.mark_sent(row_id)
            log.info("Run card created — message_id=%s", self._message_id)
            return True
        except (KeyError, ValueError) as exc:
            log.error("Failed to parse message_id from Discord response: %s — %s", exc, resp.text[:200])
            return False

    def edit(self, embed: dict) -> bool:
        """PATCH the existing message. Returns True on confirmed success."""
        if not self._message_id:
            log.warning("No message_id — cannot edit; attempting create instead.")
            return self.create(embed)
        edit_url = self.url + f"/messages/{self._message_id}"
        components = embed.pop("_components", None) if isinstance(embed, dict) else None
        payload = _wrap_embed(embed, components=components)
        row_id = self.outbox.enqueue(self.run_id, payload)
        resp = self._http("PATCH", edit_url, payload)
        if resp is not None:
            self.outbox.mark_sent(row_id)
            return True
        log.error("Edit failed — message_id=%s. Card not updated.", self._message_id)
        return False

    def flush_pending(self):
        """Retry any unsent payloads from this run (best-effort)."""
        for row_id, raw, attempts in self.outbox.pending(self.run_id):
            payload = json.loads(raw)
            log.info("Retrying outbox entry id=%d (previous attempts: %d)", row_id, attempts)
            if self._message_id:
                edit_url = self.url + f"/messages/{self._message_id}"
                resp = self._http("PATCH", edit_url, payload)
            else:
                resp = self._http("POST", self.url + "?wait=true", payload)
            if resp is not None:
                self.outbox.mark_sent(row_id)


# ---------------------------------------------------------------------------
# Run state — single source of truth for the live card
# ---------------------------------------------------------------------------

STAGES = ["detection", "incident", "recommendation", "authorization", "execution", "verification"]

class RunState:
    """Accumulates lifecycle evidence as the demo runs."""

    def __init__(self, run_id: str, start_utc: datetime.datetime, target: str = "payment-demo-v17"):
        self.run_id = run_id
        self.start_utc = start_utc
        self.target = target
        self.end_utc: Optional[datetime.datetime] = None
        self.exit_code: Optional[int] = None

        # Stage records: each key maps to a dict of evidence, or None = pending
        self.stages: dict[str, Optional[dict]] = {s: None for s in STAGES}

        # Key identifiers (filled as milestones are parsed)
        self.rule_id: str = ""
        self.finding_id: str = ""   # not in demo output — populated from DB if available
        self.incident_id: str = ""
        self.risk: str = ""
        self.confidence: str = ""
        self.action: str = ""
        self.recommendation_id: str = ""  # from DB
        self.approver: str = ""
        self.authorization_id: str = ""   # execution_authorizations.execution_id (PK)
        self.execution_id: str = ""
        self.verification_status: str = ""
        self.findings_records: list[dict] = []

        # DB counts (filled post-run)
        self.db_findings: str = "UNAVAILABLE"
        self.db_incidents: str = "UNAVAILABLE"
        self.db_recommendations: str = "UNAVAILABLE"
        self.db_executions: str = "UNAVAILABLE"
        self.db_verifications: str = "UNAVAILABLE"
        self.db_failed_verifications: str = "UNAVAILABLE"

        # Recommendation selection (filled from DB + ranking)
        self.best_rec: Optional[dict] = None        # top-ranked recommendation dict
        self.rec_reason: str = ""                   # human-readable reason for selection
        self.rec_alternatives: list[dict] = []      # other recs for this incident
        # Authorization decision: None = AWAITING APPROVAL, dict = developer decision
        self.auth_decision: Optional[dict] = None
        self.selected_action: str = "NONE"
        self.selection_status: str = "AWAITING DEVELOPER SELECTION"

    def record_detection(self, rule: str, asset: str):
        self.rule_id = rule
        self.target = asset
        self.stages["detection"] = {"rule": rule, "asset": asset}

    def record_incident(self, inc_id: str, risk: str, confidence: str):
        self.incident_id = inc_id
        self.risk = risk
        self.confidence = confidence
        self.stages["incident"] = {"incident_id": inc_id, "risk": risk, "confidence": confidence}

    def record_recommendation(self, action: str):
        self.action = action
        self.stages["recommendation"] = {"action": action}

    def record_authorization(self, approver: str = ""):
        self.approver = approver
        self.stages["authorization"] = {"status": "APPROVED"}

    def record_execution(self, exec_id: str):
        self.execution_id = exec_id
        self.stages["execution"] = {"execution_id": exec_id, "status": "EXECUTED"}

    def record_verification(self, status: str = "VERIFIED"):
        self.verification_status = status
        self.stages["verification"] = {"status": status}

    def stages_recorded(self) -> int:
        return sum(1 for v in self.stages.values() if v is not None)

    def final_status(self) -> str:
        """
        Determine reporting status from evidence, NOT from exit code.
        COMPLETE   — all 6 stages recorded and verification is VERIFIED
        VERIFICATION FAILED — verification stage recorded with non-VERIFIED status
        INCOMPLETE — run ended with stages missing
        """
        ver = self.stages.get("verification")
        if ver is not None:
            if ver.get("status") == "VERIFIED":
                if self.stages_recorded() == 6:
                    return "COMPLETE"
                return "INCOMPLETE"
            return "VERIFICATION FAILED"
        if self.exit_code is not None:
            return "INCOMPLETE"
        return "RUNNING"


# ---------------------------------------------------------------------------
# Embed builders
# ---------------------------------------------------------------------------

def _utcnow() -> str:
    return datetime.datetime.utcnow().isoformat() + "Z"


def _ist_and_utc(dt: datetime.datetime) -> str:
    ist = dt + datetime.timedelta(hours=5, minutes=30)
    return f"{ist.strftime('%H:%M:%S')} IST · {dt.strftime('%H:%M:%S')} UTC"


def _short(uid: str, n: int = 8) -> str:
    return uid[:n] if uid else "—"


def _stage_line(state: RunState, stage: str) -> str:
    """Return a single compact line for a lifecycle stage."""
    record = state.stages.get(stage)
    icon = "○" if record is None else "✓"

    if stage == "detection":
        if record is None:
            return "○ **Detection** — pending"
        return (
            f"✓ **Detection**\n"
            f"  Rule: `{record['rule']}`"
        )
    if stage == "incident":
        if record is None:
            return "○ **Incident** — pending"
        return (
            f"✓ **Incident**\n"
            f"  Risk: {record['risk']}\n"
            f"  ID: `{_short(record['incident_id'])}`"
        )
    if stage == "recommendation":
        if record is None:
            return "○ **Recommendation** — pending"
        rec_id = _short(state.recommendation_id) if state.recommendation_id else "—"
        return (
            f"✓ **Recommendation**\n"
            f"  Action: `{record['action']}`\n"
            f"  ID: `{rec_id}`"
        )
    if stage == "authorization":
        if record is None:
            return "○ **Authorization** — pending"
        auth_id = _short(state.authorization_id) if state.authorization_id else "—"
        approver = f" · `{state.approver}`" if state.approver else ""
        return f"✓ **Authorization** — {record['status']}{approver}\n  Execution ID: `{auth_id}`"
    if stage == "execution":
        if record is None:
            return "○ **Execution** — pending"
        return (
            f"✓ **Execution** — {record['status']}\n"
            f"  ID: `{_short(record['execution_id'])}`"
        )
    if stage == "verification":
        if record is None:
            return "○ **Verification** — pending"
        st = record["status"]
        icon = "✓" if st == "VERIFIED" else "✕"
        return f"{icon} **Verification** — {st}"
    return f"○ **{stage}** — pending"


def _lifecycle_block(state: RunState) -> str:
    return "\n".join(_stage_line(state, s) for s in STAGES)


def build_running_card(state: RunState) -> dict:
    """Embed shown while the demo is in progress."""
    recorded = state.stages_recorded()
    desc = f"Monitoring `{state.target}` for runtime findings."
    embed = {
        "title": "AEGIS · ZERAXIS Runtime Defense",
        "description": desc,
        "color": COLOR_RUNNING,
        "timestamp": _utcnow(),
        "footer": {"text": f"Source: ZERAXIS evidence · Raw log archived locally · Run {state.run_id[:8]}"},
        "fields": [
            {
                "name": "Run ID",
                "value": f"`{state.run_id}`",
                "inline": False,
            },
            {
                "name": "Started",
                "value": _ist_and_utc(state.start_utc),
                "inline": True,
            },
            {
                "name": "Status",
                "value": "🟡 RUNNING",
                "inline": True,
            },
            {
                "name": f"Lifecycle  —  {recorded}/6 stages recorded",
                "value": _lifecycle_block(state),
                "inline": False,
            },
        ],
    }
    return embed


def build_final_card(state: RunState) -> dict:
    """Embed shown after the demo exits."""
    status = state.final_status()
    recorded = state.stages_recorded()

    color = {
        "COMPLETE": COLOR_COMPLETE,
        "VERIFICATION FAILED": COLOR_FAILED,
        "INCOMPLETE": COLOR_INCOMPLETE,
    }.get(status, COLOR_ERROR)

    duration = ""
    if state.end_utc and state.start_utc:
        secs = int((state.end_utc - state.start_utc).total_seconds())
        duration = f"{secs // 60}m {secs % 60}s"

    # db_recommendations = total recs generated for the incident (all statuses)
    # db_executions      = recs that were approved and executed (authorized count)
    results_val = (
        f"Findings: **{state.db_findings}**\n"
        f"Incidents: **{state.db_incidents}**\n"
        f"Recs generated: **{state.db_recommendations}**\n"
        f"Recs executed: **{state.db_executions}**\n"
        f"Verified: **{state.db_verifications}**\n"
        f"Failed verifications: **{state.db_failed_verifications}**"
    )

    findings_lines = []
    if state.findings_records:
        for f in state.findings_records:
            fid_s = _short(f.get("finding_id", ""))
            rule = f.get("rule_id", "—")
            sev = f.get("severity", "—")
            ev_detail = ""
            raw_ev = f.get("evidence")
            if raw_ev:
                try:
                    ev_data = json.loads(raw_ev) if isinstance(raw_ev, str) else raw_ev
                    events = ev_data.get("events", []) if isinstance(ev_data, dict) else []
                    if events:
                        first_ev = json.loads(events[0]) if isinstance(events[0], str) else events[0]
                        if isinstance(first_ev, dict):
                            if first_ev.get("path"):
                                ev_detail = f" · `{first_ev.get('operation', 'WRITE')}` `{first_ev.get('path')}`"
                            elif first_ev.get("command"):
                                cmd = str(first_ev.get("command"))[:35]
                                ev_detail = f" · `{cmd}`"
                except Exception:
                    pass
            findings_lines.append(f"• `{fid_s}` **{rule}** [{sev}]{ev_detail}")
    findings_val = "\n".join(findings_lines)

    evidence_parts = []
    if state.incident_id:
        evidence_parts.append(f"Incident: `{state.incident_id}`")
    for f in state.findings_records:
        evidence_parts.append(f"Finding: `{f['finding_id']}`")
    if state.recommendation_id:
        evidence_parts.append(f"Recommendation: `{state.recommendation_id}`")
    if state.authorization_id and state.execution_id and state.authorization_id == state.execution_id:
        evidence_parts.append(f"Auth / Exec: `{state.authorization_id}`")
    else:
        if state.authorization_id:
            evidence_parts.append(f"Authorization (Execution ID): `{state.authorization_id}`")
        if state.execution_id:
            evidence_parts.append(f"Execution ID: `{state.execution_id}`")
    evidence_val = "\n".join(evidence_parts) if evidence_parts else "—"

    embed = {
        "title": f"AEGIS · ZERAXIS — Run {status}",
        "color": color,
        "timestamp": _utcnow(),
        "footer": {"text": f"ZERAXIS · Run {state.run_id[:8]}"},
        "fields": [
            # ── Run info ──────────────────────────────────────────────────
            {
                "name": "Run ID",
                "value": f"`{state.run_id}`",
                "inline": False,
            },
            {
                "name": "Target",
                "value": f"`{state.target}`",
                "inline": True,
            },
            {
                "name": "Duration",
                "value": duration or "—",
                "inline": True,
            },
            {
                "name": "Start",
                "value": _ist_and_utc(state.start_utc),
                "inline": True,
            },
            {
                "name": "End",
                "value": _ist_and_utc(state.end_utc) if state.end_utc else "—",
                "inline": True,
            },
            # ── Lifecycle ────────────────────────────────────────────────
            {
                "name": f"Lifecycle  —  {recorded}/6 stages recorded",
                "value": _lifecycle_block(state),
                "inline": False,
            },
            # ── Results ──────────────────────────────────────────────────
            {
                "name": "Results",
                "value": results_val,
                "inline": False,
            },
            # ── Security Findings ─────────────────────────────────────────
            *(
                [{
                    "name": f"Security Findings ({len(state.findings_records)})",
                    "value": findings_val,
                    "inline": False,
                }] if findings_val else []
            ),
            # ── Script execution ─────────────────────────────────────────
            {
                "name": "Script Exit Code",
                "value": str(state.exit_code) if state.exit_code is not None else "—",
                "inline": True,
            },
            # ── Evidence ─────────────────────────────────────────────────
            {
                "name": "Evidence",
                "value": evidence_val,
                "inline": False,
            },
        ],
    }

    # ── Recommendation & Authorization (from recommend_select module) ─────
    try:
        from recommend_select import build_discord_recommendation_fields, rank_recommendations
        # Derive inc-like dict for the ranker from state fields
        inc_proxy = {
            "incident_id": state.incident_id,
            "risk_level": state.risk,
            "confidence": state.confidence,
            "asset_id": state.target,
            "container_id": "",
        }
        recs_proxy: list[dict] = []
        if state.best_rec is not None:
            recs_proxy = [state.best_rec] + state.rec_alternatives
        elif state.action:
            # Build a minimal proxy rec from parsed demo output
            recs_proxy = [{
                "recommendation_id": state.recommendation_id or "—",
                "action": state.action,
                "priority": "CRITICAL",
                "status": "APPROVED" if state.authorization_id else "PROPOSED",
                "reason": "Action selected by ZERAXIS recommendation engine.",
            }]

        best, reason = (
            (state.best_rec, state.rec_reason)
            if state.best_rec is not None
            else rank_recommendations(inc_proxy, recs_proxy)
        )
        sel_state = None
        try:
            from recommend_select import get_run_selection
            sel_state = get_run_selection(state.run_id)
        except Exception:
            pass

        rec_fields = build_discord_recommendation_fields(
            best, reason, state.auth_decision, recs_proxy, selection_state=sel_state
        )
        # Insert before Evidence field (last entry) to keep evidence at bottom
        embed["fields"] = embed["fields"][:-1] + rec_fields + [embed["fields"][-1]]

        try:
            from recommend_select import build_discord_components
            is_awaiting = sel_state.get("status") == "AWAITING CONFIRMATION" if sel_state else False
            is_done = state.final_status() in ("COMPLETE", "VERIFICATION FAILED", "FAILED")
            embed["_components"] = build_discord_components(
                selected_action=sel_state.get("selected_action") if sel_state else None,
                awaiting_confirmation=is_awaiting,
                disabled=is_done,
            )
        except Exception:
            pass
    except Exception as _rec_exc:
        log.debug("Recommendation fields skipped: %s", _rec_exc)

    return embed


def _wrap_embed(embed: dict, components: Optional[list] = None) -> dict:
    d = {
        "username": "AEGIS | ZERAXIS",
        "avatar_url": "https://cdn.discordapp.com/embed/avatars/0.png",
        "embeds": [embed],
    }
    if components:
        d["components"] = components
    return d


# ---------------------------------------------------------------------------
# Line parser — updates RunState, triggers card edits
# ---------------------------------------------------------------------------

_PATTERNS = [
    ("detection",   re.compile(r"\[02\].*SUSPICIOUS|⚠.*SUSPICIOUS")),
    ("rule",        re.compile(r"Rule:\s*(.+)")),
    ("asset_det",   re.compile(r"Asset:\s*(.+)")),
    ("incident",    re.compile(r"\[04\].*INCIDENT CREATED|🚨.*INCIDENT CREATED")),
    ("inc_id",      re.compile(r"Incident:\s*([0-9a-f\-]{36})")),
    ("risk",        re.compile(r"Risk:\s*(\w+)")),
    ("confidence",  re.compile(r"Confidence:\s*(\d+)")),
    ("recommended", re.compile(r"\[05\].*RESPONSE RECOMMENDED|🛡.*RESPONSE RECOMMENDED")),
    ("action",      re.compile(r"Action:\s*(\w+)")),
    ("authorized",  re.compile(r"\[06\].*HUMAN AUTHORIZATION|👤.*HUMAN AUTHORIZATION")),
    ("executed",    re.compile(r"\[07\].*RESPONSE EXECUTED|⚙.*RESPONSE EXECUTED")),
    ("exec_id",     re.compile(r"Execution:\s*([0-9a-f\-]{36})")),
    ("verified",    re.compile(r"\[08\].*POST.EXECUTION VERIFICATION|🔍.*POST.EXECUTION")),
    ("resolved",    re.compile(r"\[09\].*INCIDENT RESOLVED|✅.*INCIDENT RESOLVED")),
]


class MilestoneParser:
    def __init__(self, state: RunState, card: DiscordCard):
        self.state = state
        self.card = card
        self._phase = "idle"

    def feed(self, line: str):
        l = re.sub(r"\x1b\[[0-9;]*m", "", line.strip())

        for name, pattern in _PATTERNS:
            m = pattern.search(l)
            if not m:
                continue

            if name == "detection":
                self._phase = "detection"

            elif name == "rule" and self._phase == "detection":
                self.state.rule_id = m.group(1).strip()

            elif name == "asset_det" and self._phase == "detection":
                asset = m.group(1).strip()
                if self.state.rule_id:
                    self.state.record_detection(self.state.rule_id, asset)
                    self.card.edit(build_running_card(self.state))
                    self._phase = "post_detection"

            elif name == "incident":
                self._phase = "incident"

            elif name == "inc_id" and self._phase in ("incident", "incident_partial"):
                self.state.incident_id = m.group(1).strip()
                self._phase = "incident_partial"
                self._try_commit_incident()

            elif name == "risk" and self._phase in ("incident", "incident_partial"):
                self.state.risk = m.group(1).strip()
                self._try_commit_incident()

            elif name == "confidence" and self._phase in ("incident", "incident_partial"):
                self.state.confidence = m.group(1).strip()
                self._try_commit_incident()

            elif name == "recommended":
                self._phase = "recommended"

            elif name == "action" and self._phase == "recommended":
                self.state.record_recommendation(m.group(1).strip())
                self.card.edit(build_running_card(self.state))
                self._phase = "post_recommended"

            elif name == "authorized":
                self.state.record_authorization()
                self.card.edit(build_running_card(self.state))
                self._phase = "post_authorized"

            elif name == "executed":
                self._phase = "executed"

            elif name == "exec_id" and self._phase in ("executed", "exec_partial"):
                self.state.record_execution(m.group(1).strip())
                self.card.edit(build_running_card(self.state))
                self._phase = "post_executed"

            elif name == "verified":
                self.state.record_verification("VERIFIED")
                self.card.edit(build_running_card(self.state))
                self._phase = "post_verified"

            elif name == "resolved":
                self._phase = "post_resolved"

            break

    def _try_commit_incident(self):
        if self.state.incident_id and self.state.risk and self.state.confidence:
            self.state.record_incident(
                self.state.incident_id, self.state.risk, self.state.confidence
            )
            self.card.edit(build_running_card(self.state))
            self._phase = "post_incident"


# ---------------------------------------------------------------------------
# DB-backed counts (read-only, run-scoped)
# ---------------------------------------------------------------------------

def _query_db(state: RunState):
    """
    Populate state.db_* counts and supplemental IDs from PostgreSQL.

    Isolation strategy (no run_id column exists):
      incident_id (parsed from demo output)
        └─ incident_findings.incident_id        → findings count
        └─ incident_recommendations.incident_id → recommendation_id, rec count
             └─ execution_authorizations.recommendation_id
                  → authorization execution_id, approver_id, auth count
                  └─ execution_results.execution_id
                       → exec count, verification counts

    All queries are read-only and scoped via FK relationships to
    the current run's incident_id and execution_id only.
    """
    dsn = os.environ.get("ZERAXIS_PG_DSN", "")
    if not dsn:
        log.info("ZERAXIS_PG_DSN not set — DB counts will show UNAVAILABLE.")
        return
    try:
        import psycopg2  # type: ignore
        conn = psycopg2.connect(dsn, connect_timeout=5)
        conn.set_session(readonly=True)
        cur = conn.cursor()

        # ── findings linked to this incident ─────────────────────────────
        if state.incident_id:
            cur.execute(
                "SELECT COUNT(*) FROM incident_findings WHERE incident_id = %s",
                (state.incident_id,),
            )
            state.db_findings = str(cur.fetchone()[0])

            cur.execute(
                "SELECT f.finding_id, f.rule_id, f.severity, f.first_seen, f.asset_id, f.evidence "
                "FROM incident_findings inf "
                "JOIN findings f ON f.finding_id = inf.finding_id "
                "WHERE inf.incident_id = %s "
                "ORDER BY f.first_seen",
                (state.incident_id,),
            )
            try:
                rows = cur.fetchall()
                if rows and isinstance(rows, (list, tuple)):
                    for r in rows:
                        if isinstance(r, (list, tuple)) and len(r) >= 6:
                            state.findings_records.append({
                                "finding_id": str(r[0]),
                                "rule_id": str(r[1]),
                                "severity": str(r[2]),
                                "first_seen": str(r[3]),
                                "asset_id": str(r[4]) if r[4] else "",
                                "evidence": r[5],
                            })
            except Exception:
                pass

            # ── incident count (always 1 for a known incident_id) ────────
            cur.execute(
                "SELECT COUNT(*) FROM incidents WHERE incident_id = %s",
                (state.incident_id,),
            )
            state.db_incidents = str(cur.fetchone()[0])

            # ── recommendation for this incident ─────────────────────────
            cur.execute(
                "SELECT recommendation_id FROM incident_recommendations "
                "WHERE incident_id = %s AND action = 'QUARANTINE_ASSET' "
                "ORDER BY created_at DESC LIMIT 1",
                (state.incident_id,),
            )
            row = cur.fetchone()
            if row:
                state.recommendation_id = str(row[0])

            cur.execute(
                "SELECT COUNT(*) FROM incident_recommendations "
                "WHERE incident_id = %s",
                (state.incident_id,),
            )
            state.db_recommendations = str(cur.fetchone()[0])

        # ── authorization record linked via recommendation_id ─────────────
        if state.recommendation_id:
            cur.execute(
                "SELECT execution_id, approver_id FROM execution_authorizations "
                "WHERE recommendation_id = %s::uuid "
                "ORDER BY created_at DESC LIMIT 1",
                (state.recommendation_id,),
            )
            row = cur.fetchone()
            if row:
                # execution_authorizations.execution_id is the authorization's PK
                state.authorization_id = str(row[0])
                state.approver = row[1] or ""
                # If we didn't get execution_id from demo output, use DB value
                if not state.execution_id:
                    state.execution_id = str(row[0])

            cur.execute(
                "SELECT COUNT(*) FROM execution_authorizations "
                "WHERE recommendation_id = %s::uuid",
                (state.recommendation_id,),
            )
            state.db_executions = str(cur.fetchone()[0])

        # ── verification results for this execution ───────────────────────
        if state.execution_id:
            cur.execute(
                "SELECT COUNT(*) FROM execution_results "
                "WHERE execution_id = %s::uuid "
                "AND verification_status = 'VERIFIED'",
                (state.execution_id,),
            )
            state.db_verifications = str(cur.fetchone()[0])

            cur.execute(
                "SELECT COUNT(*) FROM execution_results "
                "WHERE execution_id = %s::uuid "
                "AND verification_status = 'FAILED'",
                (state.execution_id,),
            )
            state.db_failed_verifications = str(cur.fetchone()[0])

        # ── fetch all recommendations for ranking ─────────────────────────
        if state.incident_id:
            cur.execute(
                "SELECT recommendation_id, action, priority, status, reason "
                "FROM incident_recommendations WHERE incident_id = %s ORDER BY created_at",
                (state.incident_id,),
            )
            all_recs = []
            for row in cur.fetchall():
                all_recs.append({
                    "recommendation_id": str(row[0]),
                    "action": row[1],
                    "priority": row[2],
                    "status": row[3],
                    "reason": row[4],
                })
            if all_recs:
                try:
                    from recommend_select import rank_recommendations
                    inc_proxy = {
                        "incident_id": state.incident_id,
                        "risk_level": state.risk,
                        "confidence": state.confidence,
                        "asset_id": state.target,
                    }
                    best, reason = rank_recommendations(inc_proxy, all_recs)
                    state.best_rec = best
                    state.rec_reason = reason
                    state.rec_alternatives = [r for r in all_recs if r is not best]
                except Exception as rank_exc:
                    log.debug("Recommendation ranking skipped: %s", rank_exc)

        cur.close()
        conn.close()
        log.info(
            "DB: findings=%s incidents=%s recs=%s auths=%s verified=%s failed=%s | "
            "rec_id=%s auth_id=%s approver=%s",
            state.db_findings, state.db_incidents, state.db_recommendations,
            state.db_executions, state.db_verifications, state.db_failed_verifications,
            state.recommendation_id[:8] if state.recommendation_id else "—",
            state.authorization_id[:8] if state.authorization_id else "—",
            state.approver,
        )
    except Exception as exc:
        log.warning("DB query failed (non-fatal): %s", exc)



# ---------------------------------------------------------------------------
# Main runner
# ---------------------------------------------------------------------------

def run(cmd: list[str], log_path: str) -> int:
    run_id = str(uuid.uuid4())
    os.environ["ZERAXIS_RUN_ID"] = run_id
    start_utc = datetime.datetime.utcnow()

    outbox = Outbox(OUTBOX_FILE)
    state = RunState(run_id, start_utc)
    card = DiscordCard(WEBHOOK_URL, outbox, run_id)
    parser = MilestoneParser(state, card)

    log.info("Run ID: %s", run_id)
    log.info("Logging demo output to: %s", log_path)

    if not WEBHOOK_URL:
        log.warning("DISCORD_WEBHOOK_URL not set — Discord delivery disabled.")

    # Create initial run card (POST ?wait=true)
    card.create(build_running_card(state))

    exit_code = 1
    with open(log_path, "w", encoding="utf-8") as log_fh:
        proc = subprocess.Popen(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            bufsize=1,
        )
        try:
            for line in proc.stdout:
                sys.stdout.write(line)
                sys.stdout.flush()
                log_fh.write(line)
                log_fh.flush()
                parser.feed(line)
        finally:
            proc.wait()
            exit_code = proc.returncode

    state.end_utc = datetime.datetime.utcnow()
    state.exit_code = exit_code

    # Enrich with DB evidence
    _query_db(state)

    # Final card edit
    ok = card.edit(build_final_card(state))
    if not ok:
        log.error("Final card edit failed — attempting flush.")

    card.flush_pending()
    log.info("Demo exited with code %d · lifecycle status: %s", exit_code, state.final_status())
    return exit_code


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def _parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="ZERAXIS → Discord Live Run Card relay")
    p.add_argument("--log", default=LOG_FILE, help="Path to local log archive (default: demo.log)")
    p.add_argument("cmd", nargs=argparse.REMAINDER, help="Demo command (after --)")
    return p.parse_args()


def main():
    args = _parse_args()
    cmd = args.cmd
    if cmd and cmd[0] == "--":
        cmd = cmd[1:]
    if not cmd:
        print("Usage: relay.py [--log PATH] -- <demo-command>", file=sys.stderr)
        sys.exit(1)
    sys.exit(run(cmd, args.log))


if __name__ == "__main__":
    main()

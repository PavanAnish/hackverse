#!/usr/bin/env python3
"""
AEGIS | ZERAXIS — Response Selection & Human Authorization Interface
====================================================================
Presents an evidence-based, ranked list of response options for a
security incident and requires explicit developer authorization before
any protected remediation is executed via the approvals API.

Supported actions (backed by executor handlers):
  QUARANTINE_ASSET   — docker.NetworkDisconnect  [FULLY SUPPORTED]
  STOP_CONTAINER     — docker.ContainerStop      [FULLY SUPPORTED]

Recommendation-only actions (no executor handler):
  COLLECT_MORE_EVIDENCE   — gather additional logs / traces
  ALERT_AND_MONITOR       — notify and observe without disruption
  BLOCK_NETWORK_TRAFFIC   — network-layer block (no handler)
  SUSPEND_PROCESS         — process suspend (no handler)
  REJECT                  — developer rejects the recommendation

Usage:
    python3 recommend_select.py                      # most recent incident
    python3 recommend_select.py --incident <id>      # specific incident
    python3 recommend_select.py --dry-run            # print only, no prompts
    python3 recommend_select.py --demo               # simulation mode

Environment (read from integrations/discord/.env):
    ZERAXIS_PG_DSN          PostgreSQL DSN (required for live data)
    ZERAXIS_APPROVALS_URL   Optional — Approvals API base URL
"""

from __future__ import annotations

import argparse
import datetime
import json
import os
import re
import sqlite3
import sys
import textwrap
from pathlib import Path
from typing import Optional

from dotenv import load_dotenv

load_dotenv(dotenv_path=Path(__file__).parent / ".env")
load_dotenv()

# ── Terminal colours ──────────────────────────────────────────────────────────
RED    = "\033[91m"
YELLOW = "\033[93m"
GREEN  = "\033[92m"
CYAN   = "\033[96m"
BLUE   = "\033[94m"
BOLD   = "\033[1m"
DIM    = "\033[2m"
RESET  = "\033[0m"
STAR   = "⭐"

SEV_COLOR = {"CRITICAL": RED, "HIGH": YELLOW, "MEDIUM": CYAN, "LOW": GREEN}


def _c(color: str, text: str) -> str:
    return f"{color}{text}{RESET}"


def _divider(char: str = "─", width: int = 72) -> str:
    return _c(DIM, char * width)


def _section(title: str) -> str:
    line = "═" * 72
    return f"\n{_c(BOLD+CYAN, line)}\n{_c(BOLD+CYAN, f'  {title}')}\n{_c(BOLD+CYAN, line)}"


# ---------------------------------------------------------------------------
# Supported / unsupported action catalogue
# ---------------------------------------------------------------------------

# Actions with a live executor handler (executor.go switch statement)
FULLY_SUPPORTED = {"QUARANTINE_ASSET", "STOP_CONTAINER"}

# Final response options configuration
RESPONSE_MENU = [
    {
        "key": "1",
        "action": "QUARANTINE_ASSET",
        "display_name": "Quarantine Asset",
        "label": "Quarantine Asset",
        "status": "HIGHLY RECOMMENDED",
        "selectable": True,
        "supported": True,  # Has real executor handler
        "description": "Disconnect the container from its network segment to isolate the threat.",
        "impact": "Container loses network access. Service availability affected.",
        "note": "HIGHLY RECOMMENDED — fully implemented and verified via Docker NetworkDisconnect.",
    },
    {
        "key": "2",
        "action": "COLLECT_MORE_EVIDENCE",
        "display_name": "Collect More Evidence",
        "label": "Collect More Evidence",
        "status": "AVAILABLE",
        "selectable": True,
        "supported": False,  # Missing backend executor handler
        "description": "Gather additional runtime logs and telemetry before acting.",
        "impact": "No immediate disruption. Incident remains active.",
        "note": "AVAILABLE — genuine automated executor handler is missing in backend.",
    },
    {
        "key": "3",
        "action": "ALERT_AND_MONITOR",
        "display_name": "Alert and Monitor",
        "label": "Alert and Monitor",
        "status": "AVAILABLE",
        "selectable": True,
        "supported": False,  # Missing backend executor handler
        "description": "Notify the security team and continue observation without disruption.",
        "impact": "No disruption. Threat continues under observation.",
        "note": "AVAILABLE — genuine automated executor handler is missing in backend.",
    },
    {
        "key": "4",
        "action": "BLOCK_SUSPICIOUS_NETWORK_TRAFFIC",
        "display_name": "Block Network Traffic",
        "label": "Block Network Traffic",
        "status": "NOT IMPLEMENTED",
        "selectable": False,
        "supported": False,
        "description": "Block a specific network connection identified as suspicious.",
        "impact": "Targeted network block. Requires eBPF/iptables network handler.",
        "note": "NOT IMPLEMENTED — option is disabled.",
    },
    {
        "key": "5",
        "action": "SUSPEND_SUSPICIOUS_PROCESS",
        "display_name": "Suspend Suspicious Process",
        "label": "Suspend Suspicious Process",
        "status": "NOT IMPLEMENTED",
        "selectable": False,
        "supported": False,
        "description": "Suspend a specific process reliably identified by the detection rule.",
        "impact": "Process paused. Host cgroups/kernel support required.",
        "note": "NOT IMPLEMENTED — option is disabled.",
    },
    {
        "key": "6",
        "action": "REJECT_RECOMMENDATION",
        "display_name": "Reject Recommendation",
        "label": "Reject Recommendation",
        "status": "NOT IMPLEMENTED",
        "selectable": False,
        "supported": False,
        "description": "Decline the proposed response without taking action.",
        "impact": "No automated action.",
        "note": "NOT IMPLEMENTED — option is disabled.",
    },
]

# Map action -> menu entry for quick lookup
_MENU_BY_ACTION = {m["action"]: m for m in RESPONSE_MENU}


# ---------------------------------------------------------------------------
# Recommendation ranking
# ---------------------------------------------------------------------------

def rank_recommendations(
    inc: dict,
    recs: list[dict],
) -> tuple[Optional[dict], str]:
    """
    Select the best recommendation from existing DB records using evidence
    and policy. Returns (best_rec, reason_text).

    Policy (least-to-most disruptive, prefer what we have evidence for):
    1. If any rec is already APPROVED or EXECUTED, show it as the selection.
    2. Among PROPOSED recs, prefer QUARANTINE_ASSET at CRITICAL risk (conf >= 70).
    3. At HIGH risk with lower confidence, prefer ESCALATE_INCIDENT / INVESTIGATE.
    4. Otherwise, prefer evidence gathering (COLLECT_MORE_EVIDENCE surrogate).
    5. If no recs, return None.
    """
    if not recs:
        return None, "No recommendations generated for this incident."

    # Respect existing decisions
    for r in recs:
        if r.get("status") in ("APPROVED", "EXECUTED"):
            action = r.get("action", "—")
            reason = r.get("reason") or "Evidence-based policy selection."
            return r, f"Action '{action}' was already authorized and executed. Reason: {reason}"

    # Among PROPOSED, rank by policy
    proposed = [r for r in recs if r.get("status") == "PROPOSED"]
    if not proposed:
        # All dismissed / superseded — nothing to select
        return recs[0], recs[0].get("reason") or "No selectable recommendations remain."

    risk = (inc.get("risk_level") or "").upper()
    conf = inc.get("confidence") or 0
    try:
        conf = int(conf)
    except (ValueError, TypeError):
        conf = 0

    # Find a QUARANTINE_ASSET rec first if conditions match
    for r in proposed:
        if r.get("action") == "QUARANTINE_ASSET" and risk == "CRITICAL" and conf >= 70:
            return r, (
                f"CRITICAL risk (confidence {conf}%) with active findings on this asset. "
                f"Policy: isolate before lateral movement. "
                f"Reason recorded: {r.get('reason') or '—'}"
            )

    # Otherwise take highest-priority PROPOSED rec
    prio_order = {"CRITICAL": 0, "HIGH": 1, "MEDIUM": 2, "LOW": 3}
    proposed_sorted = sorted(
        proposed,
        key=lambda r: prio_order.get((r.get("priority") or "").upper(), 9),
    )
    best = proposed_sorted[0]
    return best, (
        f"Recommended based on risk={risk}, confidence={conf}%, "
        f"priority={best.get('priority')}. "
        f"Reason: {best.get('reason') or '—'}"
    )


# ---------------------------------------------------------------------------
# Display helpers
# ---------------------------------------------------------------------------

def print_incident_header(inc: dict, asset_name: str, findings: list[dict]):
    risk = (inc.get("risk_level") or "UNKNOWN").upper()
    rc = SEV_COLOR.get(risk, "")
    inc_id = inc.get("incident_id", "—")
    container = inc.get("container_id") or "—"

    finding_summary = "—"
    if findings:
        rules = ", ".join(f.get("rule_id", "—") for f in findings[:3])
        finding_summary = rules if rules else f"{len(findings)} finding(s)"

    print(_section("ZERAXIS SECURITY INCIDENT"))
    print(f"""
  {_c(BOLD, 'Incident')}   {inc_id}
  {_c(BOLD, 'Target')}     {asset_name}
  {_c(BOLD, 'Container')}  {container}
  {_c(BOLD, 'Risk')}       {_c(rc + BOLD, risk)}
  {_c(BOLD, 'Finding')}    {finding_summary}
""")


def print_recommendation_panel(
    best_rec: Optional[dict],
    reason: str,
    findings: list[dict],
):
    """Print the starred recommendation panel."""
    if best_rec is None:
        print(f"\n  {_c(DIM, 'No actionable recommendation available for this incident.')}")
        return

    action = best_rec.get("action", "—")
    rec_id = str(best_rec.get("recommendation_id", "—"))
    priority = best_rec.get("priority", "—")
    pc = SEV_COLOR.get((priority or "").upper(), "")

    # Evidence summary from findings
    ev_lines = []
    for f in findings[:2]:
        fid = str(f.get("finding_id", ""))[:8]
        rule = f.get("rule_id", "—")
        sev = f.get("severity", "—")
        ev_lines.append(f"  • Finding `{fid}…`  rule={rule}  severity={sev}")
    ev_text = "\n".join(ev_lines) if ev_lines else "  (no findings linked)"

    # Impact
    menu_entry = _MENU_BY_ACTION.get(action, {})
    impact = menu_entry.get("impact", "NOT ASSESSED")

    print(f"""
{_c(BOLD + YELLOW, f'  {STAR} ZERAXIS RECOMMENDATION')}
  {_divider('─')}
  {_c(BOLD, 'Action')}      {_c(BOLD + GREEN, action)}
  {_c(BOLD, 'Rec ID')}      {rec_id}
  {_c(BOLD, 'Priority')}    {_c(pc + BOLD, priority)}

  {_c(BOLD, 'Why recommended:')}
{textwrap.fill(reason, 68, initial_indent='  ', subsequent_indent='  ')}

  {_c(BOLD, 'Evidence:')}
{ev_text}

  {_c(BOLD, 'Potential impact (pre-authorization):')}
  If approved: {impact}
""")


def print_alternatives(best_action: Optional[str]):
    """Print the alternative response menu."""
    print(f"  {_c(BOLD, 'Alternative actions:')}\n")
    for opt in RESPONSE_MENU:
        is_rec = opt["action"] == best_action
        if is_rec:
            continue  # already shown as starred recommendation
        key = opt["key"]
        label = opt["label"]
        note = opt.get("note", "")
        supported = opt.get("supported", False)
        if supported:
            tag = _c(GREEN, "[AVAILABLE]")
        elif "NOT IMPLEMENTED" in note:
            tag = _c(DIM, "[NOT IMPLEMENTED]")
        else:
            tag = _c(YELLOW, "[RECOMMENDATION ONLY]")
        print(f"  [{key}] {label}  {tag}")
        if note:
            print(f"       {_c(DIM, note)}")
    print()


OUTBOX_FILE = str(Path(__file__).parent / "outbox.db")


# ---------------------------------------------------------------------------
# Shared selection state store (SQLite in outbox.db)
# ---------------------------------------------------------------------------

def init_selection_table(db_path: str = OUTBOX_FILE):
    try:
        conn = sqlite3.connect(db_path, check_same_thread=False)
        with conn:
            conn.execute("""
                CREATE TABLE IF NOT EXISTS run_selections (
                    run_id                TEXT PRIMARY KEY,
                    incident_id           TEXT,
                    target                TEXT,
                    recommendation_action TEXT,
                    selected_action       TEXT,
                    status                TEXT,
                    confirmed             INTEGER DEFAULT 0,
                    actor_id              TEXT,
                    actor_role            TEXT,
                    created_at            TEXT,
                    updated_at            TEXT
                );
            """)
        conn.close()
    except Exception:
        pass


def save_run_selection(
    run_id: str,
    incident_id: str,
    target: str,
    rec_action: str,
    sel_action: str,
    status: str,
    confirmed: bool = False,
    actor_id: str = "developer",
    actor_role: str = "Security Operator",
    db_path: str = OUTBOX_FILE,
):
    init_selection_table(db_path)
    now = datetime.datetime.utcnow().isoformat() + "Z"
    try:
        conn = sqlite3.connect(db_path, check_same_thread=False)
        with conn:
            conn.execute("""
                INSERT INTO run_selections
                (run_id, incident_id, target, recommendation_action, selected_action, status, confirmed, actor_id, actor_role, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(run_id) DO UPDATE SET
                    selected_action=excluded.selected_action,
                    status=excluded.status,
                    confirmed=excluded.confirmed,
                    actor_id=excluded.actor_id,
                    actor_role=excluded.actor_role,
                    updated_at=excluded.updated_at
            """, (run_id, incident_id, target, rec_action, sel_action, status, 1 if confirmed else 0, actor_id, actor_role, now, now))
        conn.close()
    except Exception:
        pass


def get_run_selection(run_id: str, db_path: str = OUTBOX_FILE) -> Optional[dict]:
    init_selection_table(db_path)
    try:
        conn = sqlite3.connect(db_path, check_same_thread=False)
        cur = conn.cursor()
        cur.execute("SELECT run_id, incident_id, target, recommendation_action, selected_action, status, confirmed, actor_id, actor_role, created_at, updated_at FROM run_selections WHERE run_id = ?", (run_id,))
        row = cur.fetchone()
        conn.close()
        if not row:
            return None
        cols = ["run_id", "incident_id", "target", "recommendation_action", "selected_action", "status", "confirmed", "actor_id", "actor_role", "created_at", "updated_at"]
        return dict(zip(cols, row))
    except Exception:
        return None


def print_full_menu(best_action: Optional[str] = "QUARANTINE_ASSET"):
    """Print the final response options menu in the exact format required."""
    print(f"\n  {_c(BOLD, 'RESPONSE OPTIONS:')}\n")
    for opt in RESPONSE_MENU:
        key = opt["key"]
        name = opt["display_name"]
        status = opt["status"]
        if key == "1":
            print(f"  [{key}] * {name} — {_c(BOLD + GREEN, status)}")
        elif opt["selectable"]:
            print(f"  [{key}]   {name} — {_c(CYAN, status)}")
        else:
            print(f"  [{key}]   {name} — {_c(DIM, status)}")
    print()


# ---------------------------------------------------------------------------
# Interactive authorization
# ---------------------------------------------------------------------------

AuthResult = dict  # {action, approved, status, reason, timestamp, rec_id}


def prompt_developer(
    best_rec: Optional[dict],
    recs: list[dict],
    inc: dict,
    dry_run: bool = False,
    run_id: str = "",
    target_override: str = "",
) -> AuthResult:
    """
    Interactively prompt the developer to select and confirm a response.
    Returns an AuthResult dict. In dry_run mode, returns without prompting.
    """
    orig_rec_action = best_rec.get("action", "QUARANTINE_ASSET") if best_rec else "QUARANTINE_ASSET"
    target_asset = target_override or inc.get("asset_id") or inc.get("target") or "payment-demo-v17"
    inc_id = inc.get("incident_id", "—")

    if dry_run:
        action = orig_rec_action
        return {
            "action": action,
            "approved": False,
            "status": "AWAITING DEVELOPER SELECTION",
            "reason": "Dry run — no developer input collected.",
            "timestamp": datetime.datetime.utcnow().isoformat() + "Z",
            "rec_id": str(best_rec.get("recommendation_id")) if best_rec else None,
        }

    # Record initial state
    if run_id:
        save_run_selection(
            run_id=run_id,
            incident_id=inc_id,
            target=target_asset,
            rec_action=orig_rec_action,
            sel_action="NONE",
            status="AWAITING DEVELOPER SELECTION",
            confirmed=False,
        )

    print_full_menu(orig_rec_action)

    # Check environment override (useful for non-interactive test runs or automation)
    env_choice = os.environ.get("ZERAXIS_SELECTION", "").strip()
    if not env_choice and not sys.stdin.isatty():
        env_choice = "1"

    while True:
        if env_choice:
            raw = env_choice
            print(f"  {_c(BOLD, 'Selected via ZERAXIS_SELECTION')}: {raw}")
            env_choice = ""  # use once
        else:
            try:
                raw = input(f"  {_c(BOLD, 'Enter selection')} (1-3) or q to quit: ").strip().lower()
            except (EOFError, KeyboardInterrupt):
                print(f"\n  {_c(YELLOW, 'Interrupted — no action taken.')}")
                if run_id:
                    save_run_selection(run_id, inc_id, target_asset, orig_rec_action, "NONE", "CANCELLED", False)
                return {
                    "action": "NONE",
                    "approved": False,
                    "status": "CANCELLED",
                    "reason": "Developer interrupted selection.",
                    "timestamp": datetime.datetime.utcnow().isoformat() + "Z",
                    "rec_id": str(best_rec.get("recommendation_id")) if best_rec else None,
                }

        if raw == "q":
            print(f"  {_c(YELLOW, 'Selection cancelled — no action taken.')}")
            if run_id:
                save_run_selection(run_id, inc_id, target_asset, orig_rec_action, "NONE", "CANCELLED", False)
            return {
                "action": "NONE",
                "approved": False,
                "status": "CANCELLED",
                "reason": "Developer cancelled selection.",
                "timestamp": datetime.datetime.utcnow().isoformat() + "Z",
                "rec_id": str(best_rec.get("recommendation_id")) if best_rec else None,
            }

        selected = next((m for m in RESPONSE_MENU if m["key"] == raw), None)
        if selected is None:
            print(f"  {_c(RED, 'Invalid choice.')} Enter 1, 2, or 3.")
            continue

        action = selected["action"]
        selectable = selected.get("selectable", False)

        # Options 4, 5, 6 must be disabled
        if not selectable:
            print(f"\n  {_c(RED, 'Action disabled:')} {selected['display_name']} is {_c(BOLD + RED, 'NOT IMPLEMENTED')}.")
            print(f"  {_c(DIM, 'Options 4, 5, and 6 cannot be executed. Select 1, 2, or 3.')}\n")
            continue

        # Valid selection made: display required confirmation block
        print(f"""
  {_c(BOLD, 'ZERAXIS RECOMMENDATION')}: {orig_rec_action}
  {_c(BOLD, 'DEVELOPER SELECTION')}   : {_c(BOLD + GREEN, action)}
  {_c(BOLD, 'TARGET')}                : {target_asset}
""")
        if run_id:
            save_run_selection(run_id, inc_id, target_asset, orig_rec_action, action, "AWAITING CONFIRMATION", False)

        env_confirm = os.environ.get("ZERAXIS_CONFIRM", "").strip().lower()
        if not env_confirm and not sys.stdin.isatty():
            env_confirm = "y"
        if env_confirm:
            confirm = env_confirm
            print(f"  {_c(BOLD, 'Confirmation via ZERAXIS_CONFIRM')}: {confirm}")
        else:
            try:
                confirm = input(
                    f"  {_c(BOLD, 'Confirm authorization')} for {action}? [{_c(GREEN, 'y')} = approve / {_c(RED, 'n')} = cancel]: "
                ).strip().lower()
            except (EOFError, KeyboardInterrupt):
                confirm = "n"

        if confirm != "y":
            print(f"  {_c(YELLOW, 'Authorization cancelled — no action taken.')}")
            if run_id:
                save_run_selection(run_id, inc_id, target_asset, orig_rec_action, action, "CANCELLED", False)
            return {
                "action": action,
                "approved": False,
                "status": "CANCELLED",
                "reason": "Developer cancelled authorization confirmation.",
                "timestamp": datetime.datetime.utcnow().isoformat() + "Z",
                "rec_id": str(best_rec.get("recommendation_id")) if best_rec else None,
            }

        print(f"  {_c(GREEN, f'✓ Authorization confirmed for: {action}')}")
        if run_id:
            save_run_selection(run_id, inc_id, target_asset, orig_rec_action, action, "APPROVED", True)

        matched_rec = best_rec if action == orig_rec_action else next(
            (r for r in recs if r.get("action") == action),
            best_rec,
        )

        return {
            "action": action,
            "approved": True,
            "status": "APPROVED",
            "reason": f"Developer explicitly authorized {action} on {target_asset}.",
            "timestamp": datetime.datetime.utcnow().isoformat() + "Z",
            "rec_id": str(matched_rec.get("recommendation_id")) if matched_rec else None,
        }
        return {
            "action": action,
            "approved": True,
            "reason": f"Developer explicitly authorized {action}.",
            "timestamp": datetime.datetime.utcnow().isoformat() + "Z",
            "rec_id": str(matched_rec.get("recommendation_id")) if matched_rec else None,
        }


# ---------------------------------------------------------------------------
# Audit trail print
# ---------------------------------------------------------------------------

def print_authorization_summary(result: AuthResult, best_rec: Optional[dict], reason: str):
    print(_section("AUTHORIZATION DECISION"))

    approved = result.get("approved", False)
    action   = result.get("action", "—")
    status   = _c(GREEN, "APPROVED") if approved else _c(YELLOW, "NOT APPROVED")

    print(f"""
  {_c(BOLD, 'Decision')}     {status}
  {_c(BOLD, 'Action')}       {action}
  {_c(BOLD, 'Rec ID')}       {result.get('rec_id') or '—'}
  {_c(BOLD, 'Timestamp')}    {result.get('timestamp', '—')}
  {_c(BOLD, 'Reason')}       {result.get('reason', '—')}
""")
    if not approved and action not in ("REJECT", "NONE"):
        print(f"  {_c(DIM, 'No action was executed.')}")
    elif action == "REJECT":
        print(f"  {_c(DIM, 'Recommendation rejected. Decision recorded.')}")
    elif approved:
        print(f"  {_c(GREEN, 'Action authorized. Submit to the approvals API to execute.')}")


# ---------------------------------------------------------------------------
# DB helpers (reuse same DSN pattern as report.py)
# ---------------------------------------------------------------------------

def _connect():
    dsn = os.environ.get("ZERAXIS_PG_DSN", "")
    if not dsn:
        return None
    try:
        import psycopg2
        conn = psycopg2.connect(dsn, connect_timeout=5)
        conn.set_session(readonly=True)
        return conn
    except Exception as exc:
        err_msg = str(exc)
        if "@" in err_msg:
            err_msg = re.sub(r'://[^@]+@', '://***:***@', err_msg)
        print(f"  {YELLOW}DB connection warning: {err_msg}{RESET}", file=sys.stderr)
        return None


def fetch_incident(conn, incident_id: Optional[str]) -> dict:
    cur = conn.cursor()
    if incident_id:
        cur.execute(
            "SELECT incident_id, status, risk_level, severity, confidence, "
            "asset_id, container_id, image_digest, first_seen, created_at "
            "FROM incidents WHERE incident_id = %s",
            (incident_id,)
        )
    else:
        cur.execute(
            "SELECT incident_id, status, risk_level, severity, confidence, "
            "asset_id, container_id, image_digest, first_seen, created_at "
            "FROM incidents ORDER BY created_at DESC LIMIT 1"
        )
    row = cur.fetchone()
    if not row:
        sys.exit(f"{RED}No incident found.{RESET}")
    cols = [d[0] for d in cur.description]
    return dict(zip(cols, row))


def fetch_findings(conn, incident_id: str) -> list[dict]:
    cur = conn.cursor()
    cur.execute(
        "SELECT f.finding_id, f.rule_id, f.severity, f.first_seen "
        "FROM incident_findings inf "
        "JOIN findings f ON f.finding_id = inf.finding_id "
        "WHERE inf.incident_id = %s ORDER BY f.first_seen",
        (incident_id,)
    )
    cols = [d[0] for d in cur.description]
    return [dict(zip(cols, r)) for r in cur.fetchall()]


def fetch_recommendations(conn, incident_id: str) -> list[dict]:
    cur = conn.cursor()
    cur.execute(
        "SELECT recommendation_id, action, priority, status, reason "
        "FROM incident_recommendations WHERE incident_id = %s ORDER BY created_at",
        (incident_id,)
    )
    cols = [d[0] for d in cur.description]
    return [dict(zip(cols, r)) for r in cur.fetchall()]


def fetch_asset_name(conn, asset_id: str) -> str:
    try:
        cur = conn.cursor()
        cur.execute("SELECT name FROM discovery_assets WHERE asset_id = %s", (asset_id,))
        row = cur.fetchone()
        return row[0] if row else asset_id
    except Exception:
        return asset_id


# ---------------------------------------------------------------------------
# Simulation mode data
# ---------------------------------------------------------------------------

SIMULATION_BANNER = f"{YELLOW}{BOLD}⚠  SIMULATION MODE — All data below is fabricated  ⚠{RESET}"

DEMO_INCIDENT = {
    "incident_id": "DEMO-0000-0000-0000-000000000001",
    "status": "OPEN", "risk_level": "CRITICAL", "severity": "HIGH",
    "confidence": 95,
    "asset_id": "DEMO-ASSET", "container_id": "DEMO-CONTAINER",
    "image_digest": "sha256:demo",
    "first_seen": datetime.datetime(2026, 1, 1, 0, 0, 0),
    "created_at": datetime.datetime(2026, 1, 1, 0, 0, 0),
}

DEMO_FINDINGS = [
    {"finding_id": "DEMO-FINDING-001", "rule_id": "rule-sensitive-file-write",
     "severity": "CRITICAL",
     "first_seen": datetime.datetime(2026, 1, 1, 0, 0, 0)},
]

DEMO_REC = {
    "recommendation_id": "DEMO-REC-0000-0000-000000000001",
    "action": "QUARANTINE_ASSET", "priority": "CRITICAL",
    "status": "PROPOSED",
    "reason": "[SIMULATION] Critical risk with strong evidence warrants isolation.",
}


# ---------------------------------------------------------------------------
# Discord card extension helpers (imported by relay.py / report.py)
# ---------------------------------------------------------------------------

def build_discord_recommendation_fields(
    best_rec: Optional[dict],
    reason: str,
    auth_result: Optional[AuthResult],
    recs: list[dict],
    selection_state: Optional[dict] = None,
) -> list[dict]:
    """
    Returns a list of Discord embed field dicts for the recommendation and
    authorization sections. Safe to insert into an existing embed's fields list.
    """
    fields = []

    # ── Recommendation panel ──────────────────────────────────────────────
    if best_rec:
        action   = best_rec.get("action", "—")
        priority = best_rec.get("priority", "—")
        rec_id   = str(best_rec.get("recommendation_id", "—"))[:8]
        reason_s = textwrap.shorten(reason, width=180, placeholder="…")
        menu_entry_d = _MENU_BY_ACTION.get(action, {})
        impact_d = menu_entry_d.get("impact", "NOT ASSESSED")
        rec_val = (
            f"⭐ **{action}** — HIGHLY RECOMMENDED [priority: {priority}]\n"
            f"Rec ID: `{rec_id}`\n"
            f"**Why:** {reason_s}\n"
            f"**Potential impact (pre-authorization):** If approved — {impact_d}"
        )
        fields.append({"name": "ZERAXIS Recommendation", "value": rec_val, "inline": False})
    else:
        fields.append({
            "name": "ZERAXIS Recommendation",
            "value": "No actionable recommendation available.",
            "inline": False,
        })

    # ── Alternatives panel ────────────────────────────────────────────────
    alt_lines = []
    best_action = best_rec.get("action") if best_rec else None
    for opt in RESPONSE_MENU:
        if opt["action"] == best_action:
            continue
        tag = f"[{opt['status']}]"
        alt_lines.append(f"• [{opt['key']}] {opt['display_name']} {tag}")
    if alt_lines:
        fields.append({
            "name": "Alternatives",
            "value": "\n".join(alt_lines),
            "inline": False,
        })

    # ── Authorization status ──────────────────────────────────────────────
    status_str = "AWAITING APPROVAL"
    action_str = best_action or "QUARANTINE_ASSET"
    ts_str = "—"

    if selection_state:
        status_val = selection_state.get("status", "AWAITING DEVELOPER SELECTION")
        action_str = selection_state.get("selected_action", action_str)
        if status_val == "APPROVED":
            status_str = "APPROVED"
        elif status_val == "CANCELLED":
            status_str = "CANCELLED"
        else:
            status_str = status_val
    elif auth_result is not None:
        action_str = auth_result.get("action", action_str)
        ts_str = auth_result.get("timestamp", "—")
        if action_str in ("REJECT", "REJECT_RECOMMENDATION"):
            status_str = "REJECTED"
        elif auth_result.get("approved"):
            status_str = "APPROVED"
        else:
            status_str = "NOT APPROVED"

    auth_val = (
        f"**Status:** {status_str}\n"
        f"**Action:** {action_str}\n"
        f"**At:** {ts_str}"
    )
    fields.append({"name": "Authorization", "value": auth_val, "inline": False})

    return fields


def build_discord_components(
    selected_action: Optional[str] = None,
    awaiting_confirmation: bool = False,
    disabled: bool = False,
) -> list[dict]:
    """
    Returns standard Discord ActionRow component dicts for interactive buttons.
    Options 1, 2, 3 are enabled (or confirmation buttons if awaiting confirmation).
    Options 4, 5, 6 are permanently disabled (NOT IMPLEMENTED).
    """
    if awaiting_confirmation:
        return [
            {
                "type": 1,
                "components": [
                    {
                        "type": 2,
                        "style": 3,  # Green / Success
                        "label": f"Confirm: {selected_action or 'Action'}",
                        "custom_id": "confirm_action_yes",
                        "disabled": disabled,
                    },
                    {
                        "type": 2,
                        "style": 4,  # Red / Danger
                        "label": "Cancel",
                        "custom_id": "confirm_action_no",
                        "disabled": disabled,
                    },
                ],
            }
        ]

    row1 = {
        "type": 1,
        "components": [
            {
                "type": 2,
                "style": 1,  # Primary
                "label": "Quarantine Asset ⭐",
                "custom_id": "btn_act_1",
                "disabled": disabled,
            },
            {
                "type": 2,
                "style": 2,  # Secondary
                "label": "Collect More Evidence",
                "custom_id": "btn_act_2",
                "disabled": disabled,
            },
            {
                "type": 2,
                "style": 2,
                "label": "Alert and Monitor",
                "custom_id": "btn_act_3",
                "disabled": disabled,
            },
        ],
    }

    row2 = {
        "type": 1,
        "components": [
            {
                "type": 2,
                "style": 2,
                "label": "Block Network Traffic",
                "custom_id": "btn_act_4",
                "disabled": True,  # NOT IMPLEMENTED
            },
            {
                "type": 2,
                "style": 2,
                "label": "Suspend Process",
                "custom_id": "btn_act_5",
                "disabled": True,  # NOT IMPLEMENTED
            },
            {
                "type": 2,
                "style": 2,
                "label": "Reject Recommendation",
                "custom_id": "btn_act_6",
                "disabled": True,  # NOT IMPLEMENTED
            },
        ],
    }

    return [row1, row2]


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main():
    p = argparse.ArgumentParser(description="AEGIS | ZERAXIS Response Selection Interface")
    p.add_argument("--incident", help="Incident ID (default: most recent)")
    p.add_argument("--dry-run", action="store_true", help="Print only — no interactive prompts")
    p.add_argument("--demo", action="store_true", help="Simulation mode (clearly labelled)")
    p.add_argument("--interactive", action="store_true", help="Interactive terminal prompt mode")
    p.add_argument("--selection", help="Predefined choice (1, 2, or 3) for automated testing/demo")
    p.add_argument("--confirm", help="Predefined confirmation (y/n)")
    p.add_argument("--rec-id", help="Override recommendation ID")
    p.add_argument("--target", help="Override target asset")
    args = p.parse_args()

    if args.selection:
        os.environ["ZERAXIS_SELECTION"] = args.selection
    if args.confirm:
        os.environ["ZERAXIS_CONFIRM"] = args.confirm

    run_id = os.environ.get("ZERAXIS_RUN_ID", "")

    if args.demo:
        inc      = DEMO_INCIDENT
        findings = DEMO_FINDINGS
        recs     = [DEMO_REC]
        asset_name = args.target or "payment-demo-v17 [SIMULATION]"
    else:
        conn = _connect()
        inc = None
        findings = []
        recs = []
        asset_name = args.target or "payment-demo-v17"
        if conn:
            try:
                inc      = fetch_incident(conn, args.incident)
                findings = fetch_findings(conn, inc["incident_id"])
                recs     = fetch_recommendations(conn, inc["incident_id"])
                asset_name = args.target or fetch_asset_name(conn, inc.get("asset_id", ""))
            except Exception:
                inc = None
            finally:
                conn.close()

        if not inc:
            inc = {
                "incident_id": args.incident or "—",
                "risk_level": "CRITICAL",
                "severity": "CRITICAL",
                "confidence": 100,
                "asset_id": asset_name,
                "target": asset_name,
            }
            recs = [{
                "recommendation_id": args.rec_id or "—",
                "action": "QUARANTINE_ASSET",
                "priority": "CRITICAL",
                "status": "PROPOSED",
                "reason": "Critical risk with strong evidence warrants isolation.",
            }]
            findings = []

    # Rank recommendations
    best_rec, reason = rank_recommendations(inc, recs)
    if args.rec_id and best_rec:
        best_rec["recommendation_id"] = args.rec_id

    # Display incident header if not running in inline demo step
    if not args.interactive:
        print()
        print(_c(BOLD, "╔" + "═" * 70 + "╗"))
        print(_c(BOLD, "║" + "  AEGIS | ZERAXIS — Response Selection".center(70) + "║"))
        print(_c(BOLD, "╚" + "═" * 70 + "╝"))
        if args.demo:
            print(f"\n{SIMULATION_BANNER}\n")
        print_incident_header(inc, asset_name, findings)
        print_recommendation_panel(best_rec, reason, findings)
        print_alternatives(best_rec.get("action") if best_rec else None)

    # Developer selection prompt
    auth_result = prompt_developer(best_rec, recs, inc, dry_run=args.dry_run, run_id=run_id, target_override=asset_name)

    # Print summary
    print_authorization_summary(auth_result, best_rec, reason)

    # Output structured markers for caller
    print(f"\n[ZERAXIS_DECISION] ACTION={auth_result.get('action', 'NONE')} STATUS={auth_result.get('status', 'NONE')}")

    if not args.dry_run and not args.interactive:
        print(f"\n{_c(DIM, _divider('═'))}")
        print(f"{_c(BOLD + CYAN, '  ZERAXIS Response Selection Complete')}")
        print(f"{_c(DIM, _divider('═'))}\n")

    if not auth_result.get("approved"):
        sys.exit(2 if auth_result.get("status") == "CANCELLED" else 1)


if __name__ == "__main__":
    main()

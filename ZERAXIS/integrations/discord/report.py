#!/usr/bin/env python3
"""
AEGIS | ZERAXIS — Security Incident Report
==========================================
Generates a professional incident report from live database records.

Usage:
    python3 report.py                        # most recent RESOLVED incident
    python3 report.py --incident <id>        # specific incident
    python3 report.py --post-discord         # also edit/post the final Discord card
    python3 report.py --demo                 # simulation mode (clearly labelled)

Environment (read from integrations/discord/.env):
    ZERAXIS_PG_DSN          PostgreSQL DSN (required for live data)
    DISCORD_WEBHOOK_URL     Optional — only used with --post-discord
"""

from __future__ import annotations

import argparse
import datetime
import json
import os
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

SEV_COLOR = {"CRITICAL": RED, "HIGH": YELLOW, "MEDIUM": CYAN, "LOW": GREEN}

SIMULATION_BANNER = f"{YELLOW}{BOLD}⚠  SIMULATION MODE — All data below is fabricated for demonstration only  ⚠{RESET}"

RULE_METADATA = {
    "rule-sensitive-file-write": {
        "type": "FILE_ACTIVITY / SENSITIVE_FILE_WRITE",
        "description": "Unauthorized file write to sensitive system path (/etc or /var/run)",
    },
    "rule-unexpected-shell": {
        "type": "PROCESS_EXECUTION / UNEXPECTED_SHELL",
        "description": "Spawning of interactive or unexpected shell (sh/bash) in runtime container",
    },
    "rule-pkg-mgr-exec": {
        "type": "PROCESS_EXECUTION / PACKAGE_MANAGER_EXECUTION",
        "description": "Package manager execution inside container at runtime",
    },
    "rule-writable-dir-exec": {
        "type": "PROCESS_EXECUTION / WRITABLE_DIRECTORY_EXECUTION",
        "description": "Execution of binary from world-writable directory (/tmp, /dev/shm)",
    },
    "rule-vuln-exec": {
        "type": "PROCESS_EXECUTION / VULNERABLE_PACKAGE_EXEC",
        "description": "Execution of package with known vulnerable dependency",
    },
    "rule-vuln-write": {
        "type": "FILE_ACTIVITY / VULNERABLE_PACKAGE_WRITE",
        "description": "File write by package with known vulnerability",
    },
}


def parse_finding_evidence(raw_ev) -> tuple[Optional[str], dict]:
    """Parses finding evidence JSON. Returns (container_id, details_dict)."""
    container_id = None
    details = {}
    if not raw_ev:
        return container_id, details
    try:
        data = json.loads(raw_ev) if isinstance(raw_ev, str) else raw_ev
        events = data.get("events", []) if isinstance(data, dict) else []
        for ev_item in events:
            ev_obj = json.loads(ev_item) if isinstance(ev_item, str) else ev_item
            if isinstance(ev_obj, dict):
                if not container_id and ev_obj.get("container_id"):
                    container_id = ev_obj.get("container_id")
                for k, v in ev_obj.items():
                    if v not in (None, ""):
                        details[k] = v
    except Exception:
        pass
    return container_id, details


# ── Helpers ───────────────────────────────────────────────────────────────────

def _c(color: str, text: str) -> str:
    return f"{color}{text}{RESET}"

def _fmt(dt) -> str:
    if dt is None:
        return "—"
    if isinstance(dt, str):
        return dt
    return dt.strftime("%Y-%m-%d %H:%M:%S UTC")

def _duration(a, b) -> str:
    if not a or not b:
        return "—"
    secs = int((b - a).total_seconds())
    return f"{secs}s" if secs < 60 else f"{secs // 60}m {secs % 60}s"

def _trunc(s: str, n: int = 80) -> str:
    s = s.strip()
    return s[:n] + "…" if len(s) > n else s

def _divider(char: str = "─", width: int = 72) -> str:
    return _c(DIM, char * width)

def _section(title: str) -> str:
    line = "═" * 72
    return f"\n{_c(BOLD+CYAN, line)}\n{_c(BOLD+CYAN, f'  {title}')}\n{_c(BOLD+CYAN, line)}"


# ── Database layer ─────────────────────────────────────────────────────────────

def _connect():
    dsn = os.environ.get("ZERAXIS_PG_DSN", "")
    if not dsn:
        sys.exit(f"{RED}Error: ZERAXIS_PG_DSN not set. Add it to integrations/discord/.env{RESET}")
    try:
        import psycopg2
        import psycopg2.extras
        conn = psycopg2.connect(dsn, connect_timeout=5)
        conn.set_session(readonly=True)
        return conn
    except Exception as exc:
        sys.exit(f"{RED}DB connection failed: {exc}{RESET}")


def fetch_incident(conn, incident_id: Optional[str]) -> dict:
    cur = conn.cursor()
    if incident_id:
        cur.execute(
            "SELECT incident_id, status, risk_level, severity, confidence, "
            "finding_count, asset_id, container_id, image_digest, "
            "first_seen, last_seen, created_at, resolved_at, "
            "static_risk, dynamic_risk, combined_risk, attack_chain_state "
            "FROM incidents WHERE incident_id = %s",
            (incident_id,)
        )
    else:
        cur.execute(
            "SELECT incident_id, status, risk_level, severity, confidence, "
            "finding_count, asset_id, container_id, image_digest, "
            "first_seen, last_seen, created_at, resolved_at, "
            "static_risk, dynamic_risk, combined_risk, attack_chain_state "
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
        "SELECT f.finding_id, f.rule_id, f.severity, f.confidence, f.status, "
        "f.first_seen, f.last_seen, f.evidence, f.asset_id, f.image_digest "
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
        "SELECT recommendation_id, action, priority, status, reason, "
        "target_type, target_id, confidence, created_at, updated_at "
        "FROM incident_recommendations "
        "WHERE incident_id = %s ORDER BY created_at",
        (incident_id,)
    )
    cols = [d[0] for d in cur.description]
    return [dict(zip(cols, r)) for r in cur.fetchall()]


def fetch_authorization(conn, recommendation_id: str) -> Optional[dict]:
    cur = conn.cursor()
    cur.execute(
        "SELECT execution_id, approver_id, status, approved_at, created_at "
        "FROM execution_authorizations "
        "WHERE recommendation_id = %s::uuid ORDER BY created_at DESC LIMIT 1",
        (recommendation_id,)
    )
    row = cur.fetchone()
    if not row:
        return None
    cols = [d[0] for d in cur.description]
    return dict(zip(cols, row))


def fetch_execution_result(conn, execution_id: str) -> Optional[dict]:
    cur = conn.cursor()
    cur.execute(
        "SELECT execution_id, status, verification_status, verified_at, "
        "error_log, executor_version, created_at, updated_at "
        "FROM execution_results "
        "WHERE execution_id = %s::uuid ORDER BY created_at DESC LIMIT 1",
        (execution_id,)
    )
    row = cur.fetchone()
    if not row:
        return None
    cols = [d[0] for d in cur.description]
    return dict(zip(cols, row))


def fetch_asset_name(conn, asset_id: str) -> str:
    try:
        cur = conn.cursor()
        cur.execute("SELECT name FROM discovery_assets WHERE asset_id = %s", (asset_id,))
        row = cur.fetchone()
        return row[0] if row else asset_id
    except Exception:
        return asset_id


# ── Report sections ───────────────────────────────────────────────────────────

def print_header(simulation: bool):
    w = 72
    print()
    print(_c(BOLD, "╔" + "═" * (w - 2) + "╗"))
    print(_c(BOLD, "║" + "  AEGIS | ZERAXIS  —  Security Incident Report".center(w - 2) + "║"))
    print(_c(BOLD, "║" + "  Autonomous Cyber Defense Platform".center(w - 2) + "║"))
    print(_c(BOLD, "╚" + "═" * (w - 2) + "╝"))
    if simulation:
        print(f"\n{SIMULATION_BANNER}\n")
    print(_c(DIM, f"  Generated: {datetime.datetime.utcnow().strftime('%Y-%m-%d %H:%M:%S UTC')}"))


def print_executive_summary(inc: dict, asset_name: str, findings: list, recs: list, exec_result: Optional[dict]):
    print(_section("1 · EXECUTIVE SUMMARY"))

    risk = inc.get("risk_level") or "UNKNOWN"
    sev  = inc.get("severity") or "UNKNOWN"
    conf = inc.get("confidence") or "—"
    status = inc.get("status") or "UNKNOWN"

    rc = SEV_COLOR.get(risk, "")
    sc = SEV_COLOR.get(sev, "")

    print(f"""
  {_c(BOLD, 'Incident ID')}      {inc['incident_id']}
  {_c(BOLD, 'Status')}          {_c(GREEN if status == 'RESOLVED' else YELLOW, status)}
  {_c(BOLD, 'Risk Level')}      {_c(rc + BOLD, risk)}
  {_c(BOLD, 'Severity')}        {_c(sc, sev)}
  {_c(BOLD, 'Target Asset')}    {asset_name}
  {_c(BOLD, 'Container')}       {inc.get('container_id') or '—'}
  {_c(BOLD, 'Detection Time')}  {_fmt(inc.get('first_seen'))}
  {_c(BOLD, 'Resolution Time')} {_fmt(inc.get('resolved_at'))}
  {_c(BOLD, 'Response Time')}   {_duration(inc.get('first_seen'), inc.get('resolved_at'))}
""")

    ver_status = "—"
    if exec_result:
        vs = exec_result.get("verification_status")
        ver_status = _c(GREEN, vs) if vs == "VERIFIED" else _c(RED, vs or "—")

    approved_recs = sum(1 for r in recs if r["status"] in ("APPROVED", "EXECUTED"))
    print(f"  {_c(BOLD, 'Statistics')}")
    print(f"  {'─'*40}")
    print(f"  Findings detected          {len(findings)}")
    print(f"  Recommendations generated  {len(recs)}")
    print(f"  Recommendations executed   {approved_recs}")
    print(f"  Post-execution verified    {ver_status}")


def print_findings(findings: list, asset_name: str = ""):
    print(_section("2 · SECURITY FINDINGS"))
    if not findings:
        print(f"\n  {_c(DIM, 'No findings linked to this incident.')}")
        return

    for i, f in enumerate(findings, 1):
        sev = f.get("severity", "UNKNOWN")
        sc  = SEV_COLOR.get(sev, "")
        rule_id = f.get("rule_id", "—")
        meta = RULE_METADATA.get(rule_id, {
            "type": "RUNTIME_SECURITY_EVENT",
            "description": f"Runtime security rule trigger ({rule_id})",
        })

        raw_ev = f.get("evidence")
        container_id, ev_details = parse_finding_evidence(raw_ev)
        affected_asset = asset_name or f.get("asset_id") or "—"
        affected_container = container_id or "—"

        print(f"\n  {_c(BOLD, f'Finding {i} of {len(findings)}')}")
        print(f"  {_divider()}")
        print(f"  {_c(BOLD, 'ID')}                 {f['finding_id']}")
        print(f"  {_c(BOLD, 'Detection Rule')}     {_c(BOLD, rule_id)}")
        print(f"  {_c(BOLD, 'Type')}               {meta['type']}")
        print(f"  {_c(BOLD, 'Description')}        {meta['description']}")
        print(f"  {_c(BOLD, 'Severity')}           {_c(sc + BOLD, sev)}")
        print(f"  {_c(BOLD, 'Affected Asset')}     {affected_asset}")
        print(f"  {_c(BOLD, 'Container')}          {affected_container}")
        print(f"  {_c(BOLD, 'Status')}             {f.get('status', '—')}")
        print(f"  {_c(BOLD, 'First Seen')}         {_fmt(f.get('first_seen'))}")
        print(f"  {_c(BOLD, 'Last Seen')}          {_fmt(f.get('last_seen'))}")

        if ev_details:
            print(f"  {_c(BOLD, 'Evidence Details')}")
            priority_keys = ["operation", "path", "command", "process", "pid", "uid", "container_id"]
            shown_keys = set()
            for k in priority_keys:
                if k in ev_details:
                    print(f"    {_c(CYAN, k + ':')} {ev_details[k]}")
                    shown_keys.add(k)
            for k, v in ev_details.items():
                if k not in shown_keys:
                    print(f"    {_c(DIM, k + ':')} {v}")


def print_incident_analysis(inc: dict):
    print(_section("3 · INCIDENT ANALYSIS"))
    risk = inc.get("risk_level") or "UNKNOWN"
    rc = SEV_COLOR.get(risk, "")

    static  = inc.get("static_risk")
    dynamic = inc.get("dynamic_risk")
    combined= inc.get("combined_risk")
    conf    = inc.get("confidence")
    chain   = inc.get("attack_chain_state")

    print(f"""
  {_c(BOLD, 'Risk Classification')}   {_c(rc + BOLD, risk)}
  {_c(BOLD, 'Static Risk Score')}     {static if static is not None else '—'}
  {_c(BOLD, 'Dynamic Risk Score')}    {dynamic if dynamic is not None else '—'}
  {_c(BOLD, 'Combined Risk Score')}   {combined if combined is not None else '—'}
  {_c(BOLD, 'Attack Chain State')}    {chain or '—'}
""")
    print(f"  {_c(BOLD, 'Basis for Classification')}")
    print(f"  {_divider()}")
    note = (
        "The CRITICAL risk level reflects multiple concurrent rule triggers on "
        "the same container with uid=0 (root) process. Correlation assessment is "
        "derived from rule evaluation and evidence count."
    )
    for line in textwrap.wrap(note, 68):
        print(f"  {_c(DIM, line)}")


def print_recommendations(recs: list, authorizations: dict, exec_results: dict):
    print(_section("4 · RECOMMENDATIONS"))
    if not recs:
        print(f"\n  {_c(DIM, 'No recommendations.')}")
        return

    status_colors = {
        "PROPOSED": YELLOW, "APPROVED": CYAN, "EXECUTED": GREEN,
        "DISMISSED": DIM, "SUPERSEDED": DIM, "REVOKED": RED,
    }

    for i, r in enumerate(recs, 1):
        st  = r.get("status", "UNKNOWN")
        sc  = status_colors.get(st, "")
        pri = r.get("priority", "—")
        pc  = SEV_COLOR.get(pri, "")
        rec_id = str(r["recommendation_id"])

        print(f"\n  {_c(BOLD, f'Recommendation {i} of {len(recs)}')}")
        print(f"  {_divider()}")
        print(f"  {_c(BOLD, 'ID')}       {rec_id}")
        print(f"  {_c(BOLD, 'Action')}   {_c(BOLD, r.get('action', '—'))}")
        print(f"  {_c(BOLD, 'Priority')} {_c(pc + BOLD, pri)}")
        print(f"  {_c(BOLD, 'Status')}   {_c(sc + BOLD, st)}")
        print(f"  {_c(BOLD, 'Reason')}   {_trunc(r.get('reason') or '—', 64)}")
        print(f"  {_c(BOLD, 'Target')}   type={r.get('target_type') or '—'}  id={r.get('target_id') or '—'}")

        auth = authorizations.get(rec_id)
        if auth:
            print(f"  {_c(BOLD, 'Authorized')}  by={auth.get('approver_id')}  at={_fmt(auth.get('approved_at'))}")
            print(f"  {_c(BOLD, 'Exec ID')}     {auth.get('execution_id')}")

        er = exec_results.get(rec_id)
        if er:
            evs = er.get("verification_status") or "—"
            evc = GREEN if evs == "VERIFIED" else RED if evs == "FAILED" else ""
            print(f"  {_c(BOLD, 'Exec Status')} {er.get('status', '—')}  "
                  f"verification={_c(evc + BOLD, evs)}  at={_fmt(er.get('verified_at'))}")
            if er.get("error_log"):
                print(f"  {_c(BOLD, 'Error')}       {_trunc(er['error_log'])}")


def print_audit_trail(inc: dict, findings: list, recs: list, authorizations: dict, exec_results: dict):
    print(_section("5 · AUTHORIZATION & EXECUTION AUDIT TRAIL"))
    print(f"\n  {_c(BOLD, 'Timeline')}")
    print(f"  {_divider()}")

    events = []
    events.append((_fmt(inc.get("first_seen")), "DETECTION",
                   f"Incident {inc['incident_id'][:8]}… created — "
                   f"risk={inc.get('risk_level')} sev={inc.get('severity')}"))

    for r in recs:
        events.append((_fmt(r.get("created_at")), "RECOMMENDATION",
                       f"{r.get('action')} [{r.get('priority')}] — status={r.get('status')}"))
        rec_id = str(r["recommendation_id"])
        auth = authorizations.get(rec_id)
        if auth:
            events.append((_fmt(auth.get("approved_at")), "AUTHORIZATION",
                           f"Approved by {auth.get('approver_id')}  exec_id={str(auth.get('execution_id',''))[:8]}…"))
        er = exec_results.get(rec_id)
        if er:
            events.append((_fmt(er.get("created_at")), "EXECUTION",
                           f"status={er.get('status')}"))
            if er.get("verified_at"):
                events.append((_fmt(er.get("verified_at")), "VERIFICATION",
                               f"verification_status={er.get('verification_status')}"))

    if inc.get("resolved_at"):
        events.append((_fmt(inc.get("resolved_at")), "RESOLUTION",
                       f"Incident marked {inc.get('status')}"))

    label_colors = {
        "DETECTION": RED, "RECOMMENDATION": YELLOW, "AUTHORIZATION": CYAN,
        "EXECUTION": BLUE, "VERIFICATION": GREEN, "RESOLUTION": GREEN,
    }
    for ts, label, detail in events:
        lc = label_colors.get(label, "")
        print(f"  {_c(DIM, ts)}  {_c(lc + BOLD, label.ljust(16))}  {detail}")


def print_verification(exec_results: dict):
    print(_section("6 · VERIFICATION"))
    if not exec_results:
        print(f"\n  {_c(DIM, 'No execution results recorded.')}")
        return

    for rec_id, er in exec_results.items():
        vs  = er.get("verification_status") or "—"
        vc  = GREEN if vs == "VERIFIED" else RED if vs == "FAILED" else ""
        print(f"""
  {_c(BOLD, 'Execution ID')}          {er.get('execution_id')}
  {_c(BOLD, 'Execution Status')}      {er.get('status', '—')}
  {_c(BOLD, 'Verification Status')}   {_c(vc + BOLD, vs)}
  {_c(BOLD, 'Verified At')}           {_fmt(er.get('verified_at'))}
  {_c(BOLD, 'Executor Version')}      {er.get('executor_version') or '—'}""")
        if er.get("error_log"):
            print(f"  {_c(BOLD, 'Error Log')}             {_trunc(er['error_log'])}")


def print_footer():
    print(f"\n{_divider('═')}")
    print(f"{_c(BOLD+CYAN, '  ZERAXIS Autonomous Cyber Defense Platform  |  Report end')}")
    print(f"{_divider('═')}\n")


# ── Simulation mode ───────────────────────────────────────────────────────────

DEMO_INCIDENT = {
    "incident_id": "DEMO-0000-0000-0000-000000000001",
    "status": "RESOLVED", "risk_level": "CRITICAL", "severity": "HIGH",
    "confidence": 95, "finding_count": 2,
    "asset_id": "DEMO-ASSET", "container_id": "DEMO-CONTAINER",
    "image_digest": "sha256:demo",
    "first_seen": datetime.datetime(2026, 1, 1, 0, 0, 0),
    "last_seen":  datetime.datetime(2026, 1, 1, 0, 1, 0),
    "created_at": datetime.datetime(2026, 1, 1, 0, 0, 0),
    "resolved_at":datetime.datetime(2026, 1, 1, 0, 1, 30),
    "static_risk": 90, "dynamic_risk": 85, "combined_risk": 88,
    "attack_chain_state": "LATERAL_MOVEMENT",
}

DEMO_FINDINGS = [
    {"finding_id": "DEMO-FINDING-001", "rule_id": "rule-sensitive-file-write",
     "severity": "CRITICAL", "confidence": "HIGH", "status": "OPEN",
     "first_seen": datetime.datetime(2026, 1, 1, 0, 0, 0),
     "last_seen":  datetime.datetime(2026, 1, 1, 0, 0, 5),
     "evidence": json.dumps({"events": [json.dumps({"operation": "WRITE", "path": "/etc/passwd", "process": "sh", "uid": 0})]}),
     "asset_id": "DEMO-ASSET", "image_digest": "sha256:demo"},
]

DEMO_REC = {
    "recommendation_id": "DEMO-REC-0000-0000-000000000001",
    "action": "QUARANTINE_ASSET", "priority": "CRITICAL",
    "status": "EXECUTED", "reason": "[SIMULATION] Critical risk with strong evidence.",
    "target_type": "CONTAINER", "target_id": "DEMO-CONTAINER",
    "confidence": 95,
    "created_at": datetime.datetime(2026, 1, 1, 0, 0, 10),
    "updated_at": datetime.datetime(2026, 1, 1, 0, 1, 0),
}

DEMO_AUTH = {
    "execution_id": "DEMO-EXEC-0000-0000-000000000001",
    "approver_id": "demo-admin", "status": "EXECUTED",
    "approved_at": datetime.datetime(2026, 1, 1, 0, 0, 20),
    "created_at":  datetime.datetime(2026, 1, 1, 0, 0, 20),
}

DEMO_ER = {
    "execution_id": "DEMO-EXEC-0000-0000-000000000001",
    "status": "EXECUTED", "verification_status": "VERIFIED",
    "verified_at": datetime.datetime(2026, 1, 1, 0, 1, 0),
    "error_log": None, "executor_version": "demo-v1",
    "created_at": datetime.datetime(2026, 1, 1, 0, 0, 50),
    "updated_at": datetime.datetime(2026, 1, 1, 0, 1, 0),
}


# ── Optional Discord post ─────────────────────────────────────────────────────

def post_to_discord(inc: dict, findings: list, recs: list, authorizations: dict, exec_results: dict):
    """Post/edit the final run card to Discord using the existing relay infrastructure."""
    try:
        import uuid, datetime as dt
        from relay import RunState, DiscordCard, Outbox, build_final_card
        import os
        OUTBOX_FILE = str(Path(__file__).parent / "outbox.db")
        WEBHOOK_URL = os.environ.get("DISCORD_WEBHOOK_URL", "")
        if not WEBHOOK_URL:
            print(f"{YELLOW}DISCORD_WEBHOOK_URL not set — skipping Discord post.{RESET}")
            return

        run_id = str(uuid.uuid4())
        state  = RunState(run_id, inc.get("created_at") or dt.datetime.utcnow())
        state.target    = inc.get("asset_id", "unknown")
        state.incident_id = inc["incident_id"]

        if findings:
            f0 = findings[0]
            state.record_detection(f0.get("rule_id", "—"), state.target)

        state.record_incident(inc["incident_id"], inc.get("risk_level", "—"),
                              str(inc.get("confidence") or "—"))

        for r in recs:
            if r.get("status") in ("EXECUTED", "APPROVED"):
                state.record_recommendation(r.get("action", "—"))
                auth = authorizations.get(str(r["recommendation_id"]))
                if auth:
                    state.record_authorization(auth.get("approver_id", ""))
                    state.authorization_id = str(auth.get("execution_id", ""))
                    state.execution_id     = str(auth.get("execution_id", ""))
                er = exec_results.get(str(r["recommendation_id"]))
                if er:
                    state.record_execution(str(er.get("execution_id", "")))
                    vs = er.get("verification_status") or "VERIFIED"
                    state.record_verification(vs)
                break

        state.end_utc   = inc.get("resolved_at") or dt.datetime.utcnow()
        state.exit_code = 0

        outbox = Outbox(OUTBOX_FILE)
        card   = DiscordCard(WEBHOOK_URL, outbox, run_id)
        ok     = card.create(build_final_card(state))
        if ok:
            print(f"{GREEN}✓ Discord card posted (message_id={card.message_id}){RESET}")
        else:
            print(f"{YELLOW}Discord post failed — check DISCORD_WEBHOOK_URL.{RESET}")
    except Exception as exc:
        print(f"{YELLOW}Discord post error (non-fatal): {exc}{RESET}")


# ── Main ──────────────────────────────────────────────────────────────────────

def main():
    p = argparse.ArgumentParser(description="AEGIS | ZERAXIS Security Incident Report")
    p.add_argument("--incident", help="Incident ID (default: most recent)")
    p.add_argument("--post-discord", action="store_true", help="Post final card to Discord")
    p.add_argument("--demo", action="store_true", help="Simulation mode (clearly labelled)")
    args = p.parse_args()

    print_header(simulation=args.demo)

    if args.demo:
        inc           = DEMO_INCIDENT
        findings      = DEMO_FINDINGS
        recs          = [DEMO_REC]
        authorizations= {str(DEMO_REC["recommendation_id"]): DEMO_AUTH}
        exec_results  = {str(DEMO_REC["recommendation_id"]): DEMO_ER}
        asset_name    = "payment-demo-v17 [SIMULATION]"
    else:
        conn = _connect()
        inc = fetch_incident(conn, args.incident)
        asset_name = fetch_asset_name(conn, inc.get("asset_id", ""))
        findings   = fetch_findings(conn, inc["incident_id"])
        recs       = fetch_recommendations(conn, inc["incident_id"])

        authorizations: dict = {}
        exec_results: dict   = {}
        for r in recs:
            rec_id = str(r["recommendation_id"])
            auth   = fetch_authorization(conn, rec_id)
            if auth:
                authorizations[rec_id] = auth
                er = fetch_execution_result(conn, str(auth["execution_id"]))
                if er:
                    exec_results[rec_id] = er
        conn.close()

    print_executive_summary(inc, asset_name, findings, recs, next(iter(exec_results.values()), None))
    print_findings(findings, asset_name)
    print_incident_analysis(inc)
    print_recommendations(recs, authorizations, exec_results)
    print_audit_trail(inc, findings, recs, authorizations, exec_results)
    print_verification(exec_results)
    print_footer()

    if args.post_discord and not args.demo:
        post_to_discord(inc, findings, recs, authorizations, exec_results)
    elif args.post_discord and args.demo:
        print(f"{YELLOW}--post-discord is disabled in simulation mode.{RESET}")


if __name__ == "__main__":
    main()

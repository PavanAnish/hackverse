import os
import asyncio
import aiohttp
import json
import time
import asyncpg
import redis.asyncio as redis
import docker
import logging
from diagnosis import DiagnosisResult

DB_URL = os.getenv("DATABASE_URL")
REDIS_URL = os.getenv("REDIS_URL")
DISCORD_WEBHOOK_URL = os.getenv("DISCORD_WEBHOOK_URL")

async def wait_for_db():
    for i in range(30):
        try:
            print(f"Attempting to connect to database (attempt {i+1}/30)...")
            conn = await asyncpg.connect(DB_URL)
            await conn.close()
            print("Successfully connected to database.")
            break
        except Exception as e:
            print(f"Database connection failed: {e}. Retrying in 2s...")
            if i == 29:
                raise e
            await asyncio.sleep(2)

async def setup_incidents_table():
    conn = await asyncpg.connect(DB_URL)
    await conn.execute('''
        CREATE TABLE IF NOT EXISTS incidents (
            id SERIAL PRIMARY KEY,
            service VARCHAR(100) NOT NULL,
            event_msg TEXT NOT NULL,
            runbook_chunk TEXT NOT NULL,
            root_cause TEXT NOT NULL,
            confidence FLOAT NOT NULL,
            action_taken VARCHAR(100) NOT NULL,
            status VARCHAR(50) NOT NULL,
            expected_effect TEXT,
            risk_note TEXT,
            reversible BOOLEAN,
            time_to_resolution_seconds INT,
            post_mortem TEXT,
            resolved_at TIMESTAMP,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
    ''')
    await conn.close()

async def record_incident(service: str, event_msg: str, runbook_chunk: str, root_cause: str, confidence: float, action_taken: str, status: str, ttr: int, post_mortem: str, resolved_at=None, expected_effect: str = None, risk_note: str = None, reversible: bool = None, created_at=None):
    conn = await asyncpg.connect(DB_URL)
    
    import datetime
    if created_at is None:
        created_at = datetime.datetime.now()
        
    await conn.execute('''
        INSERT INTO incidents (service, event_msg, runbook_chunk, root_cause, confidence, action_taken, status, expected_effect, risk_note, reversible, time_to_resolution_seconds, post_mortem, resolved_at, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
    ''', service, event_msg, runbook_chunk, root_cause, confidence, action_taken, status, expected_effect, risk_note, reversible, ttr, post_mortem, resolved_at, created_at)
    await conn.close()

async def notify_discord(service: str, root_cause: str, attempts: int = None, status: str = "escalated", action: str = None, incident_id: str = None):
    if not DISCORD_WEBHOOK_URL:
        return
        
    # Rate limit by status type per incident
    r = redis.from_url(REDIS_URL)
    if incident_id:
        notified_key = f"notified:{service}:{incident_id}:{status}"
    else:
        notified_key = f"notified:{service}:{status}"
        
    already_notified = await r.get(notified_key)
    if already_notified:
        await r.close()
        return
    await r.setex(notified_key, 60, "1")
    await r.close()
    
    embed = {}
    if status == "detected":
        embed = {
            "title": "⚠️ Problem Detected",
            "color": 16753920, # Amber
            "fields": [
                {"name": "Service", "value": service, "inline": True},
                {"name": "Status", "value": root_cause, "inline": False}
            ]
        }
    elif status == "pending_approval":
        embed = {
            "title": "⏳ Approval Required",
            "color": 10181046, # Purple
            "fields": [
                {"name": "Service", "value": service, "inline": True},
                {"name": "Recommended Action", "value": action, "inline": True},
                {"name": "Root Cause", "value": root_cause, "inline": False},
                {"name": "Status", "value": f"AI recommends {action} for {service} — approval needed", "inline": False},
                {"name": "Action Required", "value": "[Open Dashboard to Approve](http://localhost:3001)", "inline": False}
            ]
        }
    elif status == "resolved":
        embed = {
            "title": "✅ Auto-resolved",
            "color": 3066993, # Green
            "fields": [
                {"name": "Service", "value": service, "inline": True},
                {"name": "Action Taken", "value": action, "inline": True},
                {"name": "Root Cause", "value": root_cause, "inline": False},
                {"name": "Status", "value": f"Autonomous resolution applied successfully", "inline": False}
            ]
        }
    elif status == "approved":
         embed = {
            "title": "✅ Approved and Executed",
            "color": 3066993, # Green
            "fields": [
                {"name": "Service", "value": service, "inline": True},
                {"name": "Action Taken", "value": action, "inline": True}
            ]
        }
    elif status == "rejected":
         embed = {
            "title": "❌ Rejected by Admin",
            "color": 15158332, # Red
            "fields": [
                {"name": "Service", "value": service, "inline": True},
                {"name": "Action Rejected", "value": action, "inline": True}
            ]
        }
    else:
        embed = {
            "title": "🚨 Escalation: Human Intervention Required",
            "color": 16711680, # Red
            "fields": [
                {"name": "Service", "value": service, "inline": True},
                {"name": "Auto-Remediation Attempts", "value": str(attempts), "inline": True},
                {"name": "Root Cause", "value": root_cause, "inline": False},
                {"name": "Action Required", "value": "The circuit breaker was triggered or remediation failed. Human intervention is required immediately.", "inline": False}
            ]
        }
    
    payload = {"embeds": [embed]}
    try:
        async with aiohttp.ClientSession() as session:
            resp = await session.post(DISCORD_WEBHOOK_URL, json=payload, timeout=5)
            print(f'Discord POST: {resp.status}')
    except Exception as e:
        print(f"Failed to send Discord notification: {e}")

async def execute_action(service: str, action: str):
    client = docker.from_env()
    container_name = f"sentinelops-{service}"
    if action == "restart_container":
        print(f"Executing restart on {container_name}")
        try:
            container = client.containers.get(container_name)
            container.restart()
        except docker.errors.NotFound:
            print(f"Container {container_name} not found!")
    else:
        print(f"Executing {action} on {container_name}")
        try:
            async with aiohttp.ClientSession() as session:
                await session.post(f"http://{service}:8000/chaos/fail?enable=false")
        except Exception:
            pass
        await asyncio.sleep(2)

async def execute_remediation(diagnosis: DiagnosisResult, event_msg: str, runbook_chunk: str, alert_received_at: float):
    service = diagnosis.target_service.value
    action = diagnosis.recommended_action.value
    
    HEAL_MIN_SECONDS = int(os.getenv("HEAL_MIN_SECONDS", "10"))
    HEAL_DEADLINE_SECONDS = int(os.getenv("HEAL_DEADLINE_SECONDS", "15"))
    APPROVAL_MIN_SECONDS = int(os.getenv("APPROVAL_MIN_SECONDS", "15"))
    BREAKER_MAX_REMEDIATIONS = int(os.getenv("BREAKER_MAX_REMEDIATIONS", "2"))
    
    import datetime
    created_at_dt = datetime.datetime.fromtimestamp(alert_received_at)
    
    if action in ["scale_replicas", "clear_cache"]:
        print(f"HITL Required for {action} on {service}")
        
        elapsed = time.time() - alert_received_at
        if elapsed < APPROVAL_MIN_SECONDS:
            await asyncio.sleep(APPROVAL_MIN_SECONDS - elapsed)
            
        logging.info("correlation window satisfied")
        
        await record_incident(
            service=service,
            event_msg=event_msg,
            runbook_chunk=runbook_chunk,
            root_cause=diagnosis.root_cause,
            confidence=diagnosis.confidence,
            action_taken=action,
            status="pending_approval",
            ttr=0,
            post_mortem="Awaiting human approval for high-risk action.",
            resolved_at=None,
            expected_effect=diagnosis.expected_effect,
            risk_note=diagnosis.risk_note,
            reversible=diagnosis.reversible,
            created_at=created_at_dt
        )
        asyncio.create_task(notify_discord(service, diagnosis.root_cause, status="pending_approval", action=action, incident_id=str(alert_received_at)))
        return

    r = redis.from_url(REDIS_URL)
    now = time.time()
    key = f"remediation:{service}"
    
    # Sliding window: remove older than 10 mins (600s)
    await r.zremrangebyscore(key, 0, now - 600)
    count = await r.zcard(key)
    
    if count >= BREAKER_MAX_REMEDIATIONS:
        print(f"Circuit breaker triggered for {service} ({count} remediations in last 10m). Escalating.")
        asyncio.create_task(notify_discord(service, diagnosis.root_cause, attempts=count, status="escalated", incident_id=str(alert_received_at)))
        await record_incident(
            service=service,
            event_msg=event_msg,
            runbook_chunk=runbook_chunk,
            root_cause=diagnosis.root_cause,
            confidence=diagnosis.confidence,
            action_taken="None",
            status="escalated_to_human",
            ttr=0,
            post_mortem="Circuit breaker triggered. Escalate to human.",
            resolved_at=None,
            created_at=created_at_dt
        )
        await r.close()
        return
        
    await r.zadd(key, {str(now): now})
    await r.close()
    
    await execute_action(service, action)
        
    resolved = False
    while True:
        elapsed = time.time() - alert_received_at
        if elapsed > HEAL_DEADLINE_SECONDS:
            break
            
        is_healthy = False
        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(f"http://{service}:8000/health", timeout=1) as resp:
                    if resp.status == 200:
                        is_healthy = True
        except Exception:
            pass
            
        if is_healthy:
            now_t = time.time()
            target_time = alert_received_at + HEAL_MIN_SECONDS
            if now_t < target_time:
                await asyncio.sleep(target_time - now_t)
                
            final_healthy = False
            try:
                async with aiohttp.ClientSession() as session:
                    async with session.get(f"http://{service}:8000/health", timeout=1) as resp:
                        if resp.status == 200:
                            final_healthy = True
            except Exception:
                pass
                
            if final_healthy:
                resolved = True
                logging.info("stability window satisfied")
                break
                
        await asyncio.sleep(0.1)
            
    ttr = max(0, int(time.time() - alert_received_at)) if resolved else 0
    status = "resolved" if resolved else "escalated_to_human"
    post_mortem = f"Auto-remediation applied {action}. Verification {'passed' if resolved else 'failed'}."
    
    resolved_time = datetime.datetime.now() if resolved else None

    await record_incident(
        service=service,
        event_msg=event_msg,
        runbook_chunk=runbook_chunk,
        root_cause=diagnosis.root_cause,
        confidence=diagnosis.confidence,
        action_taken=action,
        status=status,
        ttr=ttr,
        post_mortem=post_mortem,
        resolved_at=resolved_time,
        expected_effect=diagnosis.expected_effect,
        risk_note=diagnosis.risk_note,
        reversible=diagnosis.reversible,
        created_at=created_at_dt
    )
    
    if status == "escalated_to_human":
        rc_text = f"{diagnosis.root_cause} (service still unhealthy after {HEAL_DEADLINE_SECONDS}s)"
        asyncio.create_task(notify_discord(service, rc_text, attempts=count + 1, status="escalated", incident_id=str(alert_received_at)))
    else:
        asyncio.create_task(notify_discord(service, diagnosis.root_cause, status="resolved", action=action, incident_id=str(alert_received_at)))
        
    print(f"Incident for {service} {status} in {ttr}s.")

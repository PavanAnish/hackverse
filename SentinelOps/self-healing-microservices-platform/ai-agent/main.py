import asyncio
import os
import asyncpg
import uvicorn
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
import aiohttp
from diagnosis import setup_db
from remediation import setup_incidents_table
from event_consumer import start_consumer

app = FastAPI()
app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])

async def wait_for_db():
    url = os.getenv("DATABASE_URL")
    for i in range(10):
        try:
            conn = await asyncpg.connect(url)
            await conn.close()
            return
        except Exception:
            print("Waiting for DB...")
            await asyncio.sleep(2)
    raise Exception("DB not ready")

@app.on_event("startup")
async def startup_event():
    await wait_for_db()
    print("DB is ready. Initializing tables...")
    await setup_incidents_table()
    await setup_db()
    print("Setup complete. Starting event consumer...")
    asyncio.create_task(start_consumer())

@app.get("/api/incidents")
async def get_incidents():
    url = os.getenv("DATABASE_URL")
    conn = await asyncpg.connect(url)
    try:
        rows = await conn.fetch("SELECT * FROM incidents ORDER BY created_at DESC LIMIT 50")
        return [dict(r) for r in rows]
    finally:
        await conn.close()

@app.get("/api/health")
async def get_health():
    services = {
        "order-service": "http://order-service:8000/health",
        "payment-service": "http://payment-service:8000/health",
        "inventory-service": "http://inventory-service:8000/health"
    }
    status = {}
    async with aiohttp.ClientSession() as session:
        for name, url in services.items():
            try:
                async with session.get(url, timeout=2) as resp:
                    if resp.status == 200:
                        status[name] = "healthy"
                    elif resp.status == 500:
                        status[name] = "degraded"
                    else:
                        status[name] = "down"
            except Exception:
                status[name] = "down"
    return status

@app.post("/api/incidents/{incident_id}/approve")
async def approve_incident(incident_id: int):
    url = os.getenv("DATABASE_URL")
    conn = await asyncpg.connect(url)
    try:
        row = await conn.fetchrow("SELECT * FROM incidents WHERE id = $1 AND status = 'pending_approval'", incident_id)
        if not row:
            return {"error": "Incident not found or not pending approval"}
            
        service = row['service']
        action = row['action_taken']
        
        # Execute action
        from remediation import execute_action, notify_discord
        await execute_action(service, action)
        
        # In a real system, we'd poll health, but let's assume it fixes it.
        await conn.execute("UPDATE incidents SET status = 'resolved', post_mortem = 'Human approved. Action executed.' WHERE id = $1", incident_id)
        
        # Send Discord
        asyncio.create_task(notify_discord(service, row['root_cause'], status="approved", action=action))
        return {"status": "approved and executed"}
    finally:
        await conn.close()

@app.post("/api/incidents/{incident_id}/reject")
async def reject_incident(incident_id: int):
    url = os.getenv("DATABASE_URL")
    conn = await asyncpg.connect(url)
    try:
        row = await conn.fetchrow("SELECT * FROM incidents WHERE id = $1 AND status = 'pending_approval'", incident_id)
        if not row:
            return {"error": "Incident not found or not pending approval"}
            
        service = row['service']
        action = row['action_taken']
        
        await conn.execute("UPDATE incidents SET status = 'rejected', post_mortem = 'Action rejected by human admin.' WHERE id = $1", incident_id)
        
        from remediation import notify_discord
        asyncio.create_task(notify_discord(service, row['root_cause'], status="rejected", action=action))
        return {"status": "rejected"}
    finally:
        await conn.close()

if __name__ == "__main__":
    uvicorn.run("main:app", host="0.0.0.0", port=8000, reload=False)

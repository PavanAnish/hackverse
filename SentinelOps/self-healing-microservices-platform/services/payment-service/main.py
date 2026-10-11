import os
import asyncio
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
from fastapi.middleware.cors import CORSMiddleware
from prometheus_fastapi_instrumentator import Instrumentator
import asyncpg
import redis.asyncio as redis
import aio_pika
import json
from pydantic import BaseModel

class ChaosToken(BaseModel):
    enable: bool = True
    type: str = "default"

app = FastAPI(title="Payment Service")

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

Instrumentator().instrument(app).expose(app)

DB_URL = os.getenv("DATABASE_URL")
REDIS_URL = os.getenv("REDIS_URL")
RMQ_URL = os.getenv("RABBITMQ_URL")

failure_mode = False
chaos_type = "default"
rmq_exchange = None

last_alert_time = 0

@app.middleware("http")
async def chaos_middleware(request: Request, call_next):
    global last_alert_time
    if failure_mode and request.url.path not in ["/metrics", "/chaos/fail", "/chaos/token"]:
        if rmq_exchange:
            import time
            current_time = time.time()
            if current_time - last_alert_time > 30:
                msg_text = "payment-service cache memory exhausted causing slow DB queries" if chaos_type == "cache" else "payment-service error rate spikes with rising latency but CPU is normal"
                msg = json.dumps({"service": "payment-service", "msg": msg_text}).encode()
                await rmq_exchange.publish(aio_pika.Message(body=msg), routing_key="payment.failed")
                last_alert_time = current_time
        
        if request.url.path == "/pay":
            if chaos_type == "cache":
                await asyncio.sleep(2)
                return JSONResponse(status_code=503, content={"error": "cache_unavailable"})
            else:
                await asyncio.sleep(1)
                return JSONResponse(status_code=500, content={"error": "db_connection_pool_exhausted"})
        else:
            await asyncio.sleep(1)
            return JSONResponse(status_code=500, content={"error": "Chaos injected failure"})
            
    response = await call_next(request)
    return response

@app.on_event("startup")
async def startup_event():
    for i in range(5):
        try:
            conn = await asyncpg.connect(DB_URL)
            await conn.execute('''
                CREATE TABLE IF NOT EXISTS payments (
                    id SERIAL PRIMARY KEY,
                    order_id INT NOT NULL,
                    amount DECIMAL NOT NULL,
                    status VARCHAR(50) NOT NULL
                )
            ''')
            await conn.close()
            break
        except Exception as e:
            if i == 4:
                raise e
            await asyncio.sleep(2)
            
    r = redis.from_url(REDIS_URL)
    await r.ping()
    await r.close()
    
    global rmq_exchange
    for i in range(30):
        try:
            print(f"Attempting RabbitMQ connection ({i+1}/30)...")
            connection = await aio_pika.connect_robust(RMQ_URL)
            channel = await connection.channel()
            rmq_exchange = await channel.declare_exchange('sentinelops_events', aio_pika.ExchangeType.TOPIC)
            print("Successfully connected to RabbitMQ.")
            break
        except Exception as e:
            print(f"RabbitMQ connection failed: {e}. Retrying in 2s...")
            if i == 29:
                raise e
            await asyncio.sleep(2)

@app.get("/health")
async def health():
    return {"status": "ok", "service": "payment-service"}

@app.post("/chaos/fail")
async def toggle_failure(enable: bool = True, type: str = "default"):
    global failure_mode, chaos_type
    failure_mode = enable
    chaos_type = type
    return {"failure_mode": failure_mode, "type": chaos_type}

@app.post("/chaos/token")
async def toggle_token(req: ChaosToken):
    global failure_mode, chaos_type
    failure_mode = req.enable
    chaos_type = req.type
    return {"failure_mode": failure_mode, "type": chaos_type}

@app.get("/pay")
async def dummy_pay():
    return {"status": "payment processed"}

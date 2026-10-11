import os
import asyncio
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
from prometheus_fastapi_instrumentator import Instrumentator
import asyncpg
import redis.asyncio as redis
import aio_pika

from fastapi.middleware.cors import CORSMiddleware

app = FastAPI(title="Inventory Service")

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
rmq_exchange = None
stock_data = {1: 12, 2: 5, 3: 100}

@app.middleware("http")
async def chaos_middleware(request: Request, call_next):
    if failure_mode and request.url.path not in ["/metrics", "/chaos/fail"]:
        await asyncio.sleep(1)
        if rmq_exchange:
            import json
            msg = json.dumps({"service": "inventory-service", "msg": "inventory-service out of memory error"}).encode()
            await rmq_exchange.publish(aio_pika.Message(body=msg), routing_key="inventory.failed")
        return JSONResponse(status_code=500, content={"error": "Chaos injected memory leak failure"})
    response = await call_next(request)
    return response

@app.on_event("startup")
async def startup_event():
    for i in range(5):
        try:
            conn = await asyncpg.connect(DB_URL)
            await conn.execute('''
                CREATE TABLE IF NOT EXISTS inventory (
                    id SERIAL PRIMARY KEY,
                    item VARCHAR(255) NOT NULL,
                    stock INT NOT NULL
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
    return {"status": "ok", "service": "inventory-service"}

@app.post("/chaos/fail")
async def toggle_failure(enable: bool = True):
    global failure_mode
    failure_mode = enable
    return {"failure_mode": failure_mode}

@app.get("/stock")
async def get_stock():
    return stock_data

@app.post("/stock/decrement")
async def decrement_stock(data: dict):
    # data: {"items": [1, 2, 1, 3]} -> IDs of items
    items = data.get("items", [])
    for item_id in items:
        if item_id in stock_data and stock_data[item_id] > 0:
            stock_data[item_id] -= 1
    return stock_data

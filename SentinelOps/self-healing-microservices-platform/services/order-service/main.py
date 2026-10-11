import os
import asyncio
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
from prometheus_fastapi_instrumentator import Instrumentator
import asyncpg
import redis.asyncio as redis
import aio_pika
import json

from fastapi.middleware.cors import CORSMiddleware

app = FastAPI(title="Order Service")

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
orders_list = []
order_counter = 1000

@app.middleware("http")
async def chaos_middleware(request: Request, call_next):
    if failure_mode and request.url.path not in ["/metrics", "/chaos/fail"]:
        await asyncio.sleep(1)
        if rmq_exchange:
            msg = json.dumps({"service": "order-service", "msg": "order-service database write timeout causing 500 errors"}).encode()
            await rmq_exchange.publish(aio_pika.Message(body=msg), routing_key="order.failed")
        return JSONResponse(status_code=500, content={"error": "Chaos injected failure in order-service"})
    response = await call_next(request)
    return response

@app.on_event("startup")
async def startup_event():
    # Ensure retries if db is not immediately ready despite healthchecks
    for i in range(5):
        try:
            conn = await asyncpg.connect(DB_URL)
            await conn.execute('''
                CREATE TABLE IF NOT EXISTS orders (
                    id SERIAL PRIMARY KEY,
                    item VARCHAR(255) NOT NULL,
                    quantity INT NOT NULL,
                    status VARCHAR(50) NOT NULL
                )
            ''')
            await conn.close()
            break
        except Exception as e:
            if i == 4:
                raise e
            await asyncio.sleep(2)
            
    # Check Redis
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
    return {"status": "ok", "service": "order-service"}

@app.post("/chaos/fail")
async def toggle_failure(enable: bool = True):
    global failure_mode
    failure_mode = enable
    return {"failure_mode": failure_mode}

@app.get("/order")
async def dummy_order():
    return {"status": "order placed"}

@app.get("/orders")
async def get_orders():
    # Return reverse chronological
    return sorted(orders_list, key=lambda x: x["id"], reverse=True)

@app.post("/orders")
async def create_order(data: dict):
    global order_counter
    import time
    order_counter += 1
    new_order = {
        "id": order_counter,
        "items": len(data.get("items", [])),
        "total": data.get("total", 0),
        "timestamp": int(time.time() * 1000)
    }
    orders_list.append(new_order)
    return new_order

const fs = require('fs');
let content = fs.readFileSync('services/payment-service/main.py', 'utf8');

const oldMiddleware = `@app.middleware("http")
async def chaos_middleware(request: Request, call_next):
    global last_alert_time
    if failure_mode and request.url.path not in ["/metrics", "/chaos/fail", "/chaos/token"]:
        await asyncio.sleep(1) # Artificial latency
        if rmq_exchange:
            import time
            current_time = time.time()
            if current_time - last_alert_time > 30:
                msg_text = "payment-service cache memory exhausted causing slow DB queries" if chaos_type == "cache" else "payment-service error rate spikes with rising latency but CPU is normal"
                msg = json.dumps({"service": "payment-service", "msg": msg_text}).encode()
                await rmq_exchange.publish(aio_pika.Message(body=msg), routing_key="payment.failed")
                last_alert_time = current_time
        return JSONResponse(status_code=500, content={"error": "Chaos injected failure"})
    response = await call_next(request)
    return response`;

const newMiddleware = `@app.middleware("http")
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
    return response`;

content = content.replace(oldMiddleware, newMiddleware);
fs.writeFileSync('services/payment-service/main.py', content);

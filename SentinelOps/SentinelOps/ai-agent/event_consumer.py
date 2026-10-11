import os
import asyncio
import json
import aio_pika
import time
import datetime
from diagnosis import diagnose
from remediation import execute_remediation, notify_discord, record_incident

RMQ_URL = os.getenv("RABBITMQ_URL")
debouncer = {}

async def process_incident(service: str, msgs: list, alert_received_at: float):
    print(f"Processing debounced incident for {service}. Total alerts: {len(msgs)}")
    incident_msg = " | ".join(msgs)
    
    try:
        diagnosis, context = await diagnose(incident_msg)
        print(f"Diagnosis completed: {diagnosis}")
        await execute_remediation(diagnosis, incident_msg, context, alert_received_at)
    except Exception as e:
        err = f"LLM Diagnosis failed: {e}"
        print(f"Failed to handle incident for {service}: {err}")
        created_at_dt = datetime.datetime.fromtimestamp(alert_received_at)
        await record_incident(
            service=service,
            event_msg=incident_msg,
            runbook_chunk="None",
            root_cause="Unknown (Diagnosis Failed)",
            confidence=0.0,
            action_taken="None",
            status="escalated_to_human",
            ttr=0,
            post_mortem=err,
            created_at=created_at_dt
        )
        asyncio.create_task(notify_discord(service, root_cause=err, attempts=1, status="escalated", incident_id=str(alert_received_at)))
    finally:
        if service in debouncer:
            del debouncer[service]

async def start_consumer():
    while True:
        try:
            connection = None
            for i in range(30):
                try:
                    print(f"Attempting to connect to RabbitMQ (attempt {i+1}/30)...")
                    connection = await aio_pika.connect_robust(RMQ_URL)
                    print("Successfully connected to RabbitMQ.")
                    break
                except Exception as e:
                    print(f"RabbitMQ connection failed: {e}. Retrying in 2s...")
                    if i == 29:
                        raise e
                    await asyncio.sleep(2)
            
            channel = await connection.channel()
            exchange = await channel.declare_exchange('sentinelops_events', aio_pika.ExchangeType.TOPIC)
            
            queue = await channel.declare_queue('', exclusive=True)
            await queue.bind(exchange, routing_key="*.failed")
            
            print("Listening for failure events on RabbitMQ...")
            async with queue.iterator() as queue_iter:
                async for message in queue_iter:
                    async with message.process():
                        payload = json.loads(message.body.decode())
                        service = payload.get("service")
                        msg = payload.get("msg")
                        print(f"Received alert from {service}: {msg}")
                        
                        if service not in debouncer:
                            alert_time = time.time()
                            debouncer[service] = {"msgs": [msg], "task": None, "alert_received_at": alert_time}
                            try:
                                asyncio.create_task(notify_discord(service, "Investigating...", status="detected", incident_id=str(alert_time)))
                            except Exception as e:
                                print(f"Failed to send detected notification: {e}")
                            
                            task = asyncio.create_task(
                                asyncio.sleep(5)
                            )
                            
                            def on_timeout(t, s=service):
                                asyncio.create_task(process_incident(s, debouncer[s]["msgs"], debouncer[s]["alert_received_at"]))
                                
                            task.add_done_callback(on_timeout)
                            debouncer[service]["task"] = task
                        else:
                            debouncer[service]["msgs"].append(msg)
        except Exception as e:
            print(f"Consumer error or connection dropped: {e}. Reconnecting in 5 seconds...")
            await asyncio.sleep(5)

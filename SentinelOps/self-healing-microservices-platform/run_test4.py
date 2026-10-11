import requests, time, subprocess

def log(msg): print(f"[{time.strftime('%X')}] {msg}")

log("Resetting...")
import redis
r = redis.Redis(host='localhost', port=6379, db=3)
keys = r.keys('remediation:*') + r.keys('notified:*')
if keys: r.delete(*keys)
try: requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})
except: pass
subprocess.run(["docker", "exec", "sentinelops-postgres", "psql", "-U", "postgres", "-d", "sentinelops", "-c", "DELETE FROM incidents;"], check=True, capture_output=True)
subprocess.run(["docker", "restart", "sentinelops-payment-service"], check=True, capture_output=True)
time.sleep(5)

log("Test 1: DB Outage")
requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": "default"})
t0 = time.time()
res = requests.get("http://localhost:8002/pay")
t1 = time.time()
log(f"/pay (default) -> Status: {res.status_code}, Body: {res.text}, Time: {t1-t0:.2f}s")
res_health = requests.get("http://localhost:8002/health")
log(f"/health (default) -> Status: {res_health.status_code}")

log("Waiting for DB Outage to resolve...")
t2 = time.time()
while True:
    try:
        data = requests.get("http://localhost:8000/api/incidents", timeout=1).json()
        if data and data[0]['status'] == 'resolved':
            log(f"DB Outage Resolved at {time.time()-t2:.2f}s")
            break
    except: pass
    if time.time()-t2 > 20:
        log("Timeout!")
        break
    time.sleep(1)

log("Resetting for Cache Outage...")
r = redis.Redis(host='localhost', port=6379, db=3)
keys = r.keys('remediation:*') + r.keys('notified:*')
if keys: r.delete(*keys)
requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})
subprocess.run(["docker", "exec", "sentinelops-postgres", "psql", "-U", "postgres", "-d", "sentinelops", "-c", "DELETE FROM incidents;"], check=True, capture_output=True)
subprocess.run(["docker", "restart", "sentinelops-payment-service"], check=True, capture_output=True)
time.sleep(5)

log("Test 2: Cache Outage")
requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": "cache"})
t0 = time.time()
res = requests.get("http://localhost:8002/pay")
t1 = time.time()
log(f"/pay (cache) -> Status: {res.status_code}, Body: {res.text}, Time: {t1-t0:.2f}s")
res_health = requests.get("http://localhost:8002/health")
log(f"/health (cache) -> Status: {res_health.status_code}")

log("Waiting for pending_approval...")
t2 = time.time()
while True:
    try:
        data = requests.get("http://localhost:8000/api/incidents", timeout=1).json()
        if data and data[0]['status'] == 'pending_approval':
            log(f"pending_approval appeared at {time.time()-t2:.2f}s")
            log("Approving...")
            requests.post(f"http://localhost:8000/api/incidents/{data[0]['id']}/approve")
            break
    except: pass
    if time.time()-t2 > 25:
        log("Timeout waiting for pending_approval!")
        break
    time.sleep(1)

while True:
    try:
        data = requests.get("http://localhost:8000/api/incidents", timeout=1).json()
        if data and data[0]['status'] == 'resolved':
            log(f"Cache Outage Resolved!")
            break
    except: pass
    if time.time()-t2 > 35:
        log("Timeout waiting for cache resolve!")
        break
    time.sleep(1)

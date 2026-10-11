import time, requests, subprocess

print("Resetting...")
import redis
r = redis.Redis(host='localhost', port=6379, db=3)
keys = r.keys('remediation:*') + r.keys('notified:*')
if keys: r.delete(*keys)
try: requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})
except: pass
subprocess.run(["docker", "exec", "sentinelops-postgres", "psql", "-U", "postgres", "-d", "sentinelops", "-c", "DELETE FROM incidents;"], check=True, capture_output=True)
subprocess.run(["docker", "restart", "sentinelops-payment-service"], check=True, capture_output=True)
time.sleep(5)

print("Triggering Cache Outage...")
requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": "cache"})

t0 = time.time()
while True:
    try:
        res = requests.get("http://localhost:8000/api/incidents", timeout=1).json()
        if res and res[0]['status'] == 'pending_approval':
            print(f"[{time.time()-t0:.2f}s] pending_approval found in DB.")
            requests.post(f"http://localhost:8000/api/incidents/{res[0]['id']}/approve")
            print(f"[{time.time()-t0:.2f}s] Approved.")
            break
    except Exception:
        pass
    if time.time() - t0 > 25:
        print("Timeout waiting for pending_approval")
        break
    time.sleep(0.5)

while True:
    try:
        res = requests.get("http://localhost:8000/api/incidents", timeout=1).json()
        if res and res[0]['status'] == 'resolved':
            print(f"[{time.time()-t0:.2f}s] Resolved (Green).")
            break
    except Exception:
        pass
    if time.time() - t0 > 30:
        print("Timeout waiting for resolved")
        break
    time.sleep(0.5)

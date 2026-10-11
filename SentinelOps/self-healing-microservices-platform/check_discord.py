import requests, time

def reset():
    import redis, subprocess
    r = redis.Redis(host='localhost', port=6379, db=3)
    keys = r.keys('remediation:*') + r.keys('notified:*')
    if keys: r.delete(*keys)
    try: requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})
    except: pass
    subprocess.run(["docker", "exec", "sentinelops-postgres", "psql", "-U", "postgres", "-d", "sentinelops", "-c", "DELETE FROM incidents;"], check=True, capture_output=True)
    subprocess.run(["docker", "restart", "sentinelops-payment-service"], check=True, capture_output=True)
    time.sleep(5)

reset()
print("Trigger DB Outage")
requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": "default"})
time.sleep(15)

reset()
print("Trigger Cache Outage")
requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": "cache"})
time.sleep(18)
res = requests.get("http://localhost:8000/api/incidents").json()
if res and res[0]['status'] == 'pending_approval':
    requests.post(f"http://localhost:8000/api/incidents/{res[0]['id']}/approve")
time.sleep(5)

# Wait, we can't easily fetch Discord timestamps unless we have a mock webhook.
# I'll just check agent logs for "Discord POST"

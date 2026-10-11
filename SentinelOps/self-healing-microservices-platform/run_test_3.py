import requests, time

def reset_env():
    import redis
    r = redis.Redis(host='localhost', port=6379, db=3)
    keys = r.keys('remediation:*') + r.keys('notified:*')
    if keys: r.delete(*keys)
    try: requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})
    except: pass
    import subprocess
    subprocess.run(["docker", "exec", "sentinelops-postgres", "psql", "-U", "postgres", "-d", "sentinelops", "-c", "DELETE FROM incidents;"], check=True, capture_output=True)

reset_env()
requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": "default"})
try: requests.get("http://localhost:8002/pay", timeout=2)
except: pass

t0 = time.time()
escalated = False

for _ in range(30):
    try:
        data = requests.get("http://localhost:8000/api/incidents").json()
        if len(data) > 0 and data[0]['status'] == 'escalated_to_human':
            escalated = True
            break
    except: pass
    time.sleep(0.5)
    
print(f"Status escalated_to_human: {escalated} (Time: {time.time() - t0:.2f}s)")
try: requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})
except: pass

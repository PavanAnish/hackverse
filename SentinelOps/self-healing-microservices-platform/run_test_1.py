import requests, time, sys

def reset_env():
    import redis
    r = redis.Redis(host='localhost', port=6379, db=3)
    keys = r.keys('remediation:*') + r.keys('notified:*')
    if keys: r.delete(*keys)
    try: requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})
    except: pass
    
    import subprocess
    subprocess.run(["docker", "exec", "sentinelops-postgres", "psql", "-U", "postgres", "-d", "sentinelops", "-c", "DELETE FROM incidents;"], check=True, capture_output=True)
    subprocess.run(["docker", "restart", "sentinelops-payment-service"], check=True, capture_output=True)
    time.sleep(5)

def test_db_outage():
    reset_env()
    requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": "default"})
    try: requests.get("http://localhost:8002/pay", timeout=2)
    except: pass
    
    t0 = time.time()
    resolved = False
    
    for _ in range(200):  # poll fast for accuracy
        try:
            data = requests.get("http://localhost:8000/api/incidents").json()
            if len(data) > 0 and data[0]['status'] == 'resolved':
                resolved = True
                break
        except: pass
        time.sleep(0.1)
        
    ttr = time.time() - t0
    print(f"Time to resolve: {ttr:.3f}s" if resolved else "FAIL: Not resolved")
    return ttr if resolved else None

times = []
for i in range(5):
    print(f"DB Outage Run {i+1}")
    t = test_db_outage()
    if t: times.append(t)
    
if times:
    print(f"Min: {min(times):.3f}s, Avg: {sum(times)/len(times):.3f}s, Max: {max(times):.3f}s")

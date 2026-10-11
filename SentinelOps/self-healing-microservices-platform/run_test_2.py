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
    subprocess.run(["docker", "restart", "sentinelops-payment-service"], check=True, capture_output=True)
    time.sleep(5)

def test_cache_outage(action):
    reset_env()
    requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": "cache"})
    try: requests.get("http://localhost:8002/pay", timeout=2)
    except: pass
    
    t0 = time.time()
    pending_id = None
    
    for _ in range(200):
        try:
            data = requests.get("http://localhost:8000/api/incidents").json()
            if len(data) > 0 and data[0]['status'] == 'pending_approval':
                pending_id = data[0]['id']
                break
        except: pass
        time.sleep(0.1)
        
    ttr = time.time() - t0
    print(f"Time to pending: {ttr:.2f}s" if pending_id else "FAIL: Not pending_approval")
    
    if pending_id:
        requests.post(f"http://localhost:8000/api/incidents/{pending_id}/{action}")
        print(f"Did {action}.")
        
    for _ in range(10):
        try:
            data = requests.get("http://localhost:8000/api/incidents").json()
            if len(data) > 0 and data[0]['status'] in ('resolved', 'rejected'):
                print(f"Final status: {data[0]['status']}")
                break
        except: pass
        time.sleep(0.5)

print("Run 1: Approve")
test_cache_outage("approve")
print("Run 2: Reject")
test_cache_outage("reject")
print("Run 3: Approve")
test_cache_outage("approve")

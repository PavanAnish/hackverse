import requests, time, subprocess

def reset():
    import redis
    r = redis.Redis(host='localhost', port=6379, db=3)
    keys = r.keys('remediation:*') + r.keys('notified:*')
    if keys: r.delete(*keys)
    try: requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})
    except: pass
    subprocess.run(["docker", "exec", "sentinelops-postgres", "psql", "-U", "postgres", "-d", "sentinelops", "-c", "DELETE FROM incidents;"], check=True, capture_output=True)
    subprocess.run(["docker", "restart", "sentinelops-payment-service"], check=True, capture_output=True)
    time.sleep(5)

reset()
print("Trigger Cache Outage")
requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": "cache"})

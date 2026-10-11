import requests, time, subprocess
def run_cache():
    import redis
    R = redis.Redis(host='localhost', port=6379, db=3)
    if R.keys('remediation:*') + R.keys('notified:*'):
        R.delete(*(R.keys('remediation:*') + R.keys('notified:*')))
    requests.post("http://localhost:8002/chaos/fail?enable=false", headers={'Connection':'close'})
    subprocess.run(["docker", "restart", "sentinelops-payment-service"])
    time.sleep(8)
    
    t0 = time.time()
    try: requests.post("http://localhost:8002/chaos/fail?enable=true&type=cache", headers={'Connection':'close'})
    except: pass
    
    t_visible = None
    for _ in range(40):
        try:
            data = requests.get("http://localhost:8000/api/incidents").json()
            if len(data) > 0 and data[0]['status'] == 'pending_approval':
                t_visible = time.time()
                break
        except: pass
        time.sleep(0.5)
        
    print(f"Visible: {t_visible - t0 if t_visible else 'FAIL'}s")

for i in range(1, 4):
    print(f"Run {i}")
    run_cache()

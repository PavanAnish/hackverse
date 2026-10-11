import requests
import time
import redis
import subprocess
import re
from datetime import datetime
import pytz

R = redis.Redis(host='localhost', port=6379, db=3)

def clear_redis():
    keys = R.keys('remediation:*') + R.keys('notified:*')
    if keys:
        R.delete(*keys)

def reset_chaos():
    requests.post("http://localhost:8002/chaos/fail?enable=false")
    subprocess.run(["docker", "restart", "sentinelops-payment-service"], capture_output=True)
    time.sleep(10)
    for _ in range(10):
        try:
            if requests.get("http://localhost:8002/health").status_code == 200: break
        except: pass
        time.sleep(1)

def get_logs_since(since_iso):
    res = subprocess.run(["docker", "logs", "--timestamps", "sentinelops-ai-agent"], capture_output=True, text=True)
    lines = res.stdout.splitlines()
    parsed = []
    for line in lines:
        parts = line.split(" ", 1)
        if len(parts) == 2:
            ts_str, msg = parts
            try:
                ts_str_short = ts_str[:26] + "Z" if len(ts_str) > 26 else ts_str
                ts = datetime.strptime(ts_str_short, "%Y-%m-%dT%H:%M:%S.%fZ").replace(tzinfo=pytz.UTC).timestamp()
                if ts >= since_iso:
                    parsed.append((ts, msg))
            except:
                pass
    return parsed

def test_db_outage():
    clear_redis()
    reset_chaos()
    time.sleep(1)
    
    t0 = time.time()
    requests.post("http://localhost:8002/chaos/fail?enable=true&type=db_lock")
    requests.get("http://localhost:8002/health") # Trigger
    
    t_health = None
    for _ in range(30):
        try:
            r = requests.get("http://localhost:8002/health", timeout=1)
            if r.status_code == 200 and "Chaos" not in r.text:
                t_health = time.time()
                break
        except:
            pass
        time.sleep(0.5)
        
    time.sleep(2) # Give agent time to flush logs
    logs = get_logs_since(t0 - 1)
    
    events = {}
    for ts, msg in logs:
        if "Received alert" in msg and "alert" not in events: events["alert"] = ts
        if "Processing debounced" in msg and "debounce" not in events: events["debounce"] = ts
        if "Diagnosis completed" in msg and "diagnosis" not in events: events["diagnosis"] = ts
        if "Executing restart" in msg and "restart_start" not in events: events["restart_start"] = ts
        if "Incident for payment-service resolved" in msg and "resolved" not in events: events["resolved"] = ts
        if "Discord POST:" in msg and "discord" not in events:
            events["discord"] = ts
            events["discord_status"] = msg.split()[-1]

    return {
        "t0": t0,
        "t_health": t_health,
        "events": events
    }

def test_cache_outage(action=None):
    clear_redis()
    reset_chaos()
    time.sleep(1)
    
    t0 = time.time()
    requests.post("http://localhost:8002/chaos/fail?enable=true&type=cache")
    requests.get("http://localhost:8002/health") # Trigger
    
    t_visible = None
    incident_id = None
    for _ in range(40):
        r = requests.get("http://localhost:8000/api/incidents")
        if r.status_code == 200:
            data = r.json()
            if len(data) > 0 and data[0]['status'] == 'pending_approval':
                t_visible = time.time()
                incident_id = data[0]['id']
                break
        time.sleep(0.5)
        
    time.sleep(2)
    
    t_action = None
    if action and incident_id:
        t_action = time.time()
        requests.post(f"http://localhost:8000/api/incidents/{incident_id}/{action}")
        time.sleep(10)
        
    logs = get_logs_since(t0 - 1)
    events = {}
    discord_posts = []
    for ts, msg in logs:
        if "Discord POST:" in msg:
            discord_posts.append((ts, msg.split()[-1]))
            
    final_status = None
    if incident_id:
        r = requests.get("http://localhost:8000/api/incidents")
        for inc in r.json():
            if inc['id'] == incident_id:
                final_status = inc['status']
                break
                
    r = requests.get("http://localhost:8002/health")
    health_ok = (r.status_code == 200 and "Chaos" not in r.text)

    return {
        "t0": t0,
        "t_visible": t_visible,
        "t_action": t_action,
        "discord_posts": discord_posts,
        "final_status": final_status,
        "health_ok": health_ok
    }

def test_circuit_breaker():
    reset_chaos()
    time.sleep(1)
    t0 = time.time()
    
    for i in range(3):
        requests.post("http://localhost:8002/chaos/fail?enable=true&type=db_lock")
        requests.get("http://localhost:8002/health")
        time.sleep(6) # Wait for agent to process and restart
        
    time.sleep(2)
    logs = get_logs_since(t0 - 1)
    
    escalated = False
    discord_posts = []
    for ts, msg in logs:
        if "Circuit breaker triggered" in msg:
            escalated = True
        if "Discord POST:" in msg:
            discord_posts.append((ts, msg.split()[-1]))
            
    return {
        "escalated": escalated,
        "discord_posts": discord_posts
    }

if __name__ == "__main__":
    db_results = []
    for i in range(3):
        print(f"DB Outage Run {i+1}...")
        db_results.append(test_db_outage())
        
    print("Running Cache Outage 3 times (none, approve, reject)...")
    cache_1 = test_cache_outage(action=None)
    cache_2 = test_cache_outage(action="approve")
    cache_3 = test_cache_outage(action="reject")
    
    print("Running Circuit Breaker...")
    cb_result = test_circuit_breaker()
    
    print("--- RESULTS ---")
    print("DB Outages:")
    for i, r in enumerate(db_results):
        print(f"Run {i+1}:")
        for k, v in r['events'].items():
            if k != 'discord_status':
                print(f"  {k}: {v - r['t0']:.2f}s")
        print(f"  discord status: {r['events'].get('discord_status')}")
        h_rec = (r['t_health'] - r['t0']) if r['t_health'] else None
        print(f"  health recovery: {h_rec:.2f}s" if h_rec else "  health recovery: FAIL")
        
    print("\nCache Outages:")
    print(f"Run 1 (No action): visible in { (cache_1['t_visible'] - cache_1['t0']) if cache_1['t_visible'] else -1 :.2f}s, Discord posts: {len(cache_1['discord_posts'])}")
    print(f"Run 2 (Approve): visible in { (cache_2['t_visible'] - cache_2['t0']) if cache_2['t_visible'] else -1 :.2f}s, Discord posts: {len(cache_2['discord_posts'])}, final status: {cache_2['final_status']}, health ok: {cache_2['health_ok']}")
    print(f"Run 3 (Reject): visible in { (cache_3['t_visible'] - cache_3['t0']) if cache_3['t_visible'] else -1 :.2f}s, Discord posts: {len(cache_3['discord_posts'])}, final status: {cache_3['final_status']}, health ok: {cache_3['health_ok']}")
    
    print("\nCircuit Breaker:")
    print(f"Escalated: {cb_result['escalated']}, Discord posts: {len(cb_result['discord_posts'])}")

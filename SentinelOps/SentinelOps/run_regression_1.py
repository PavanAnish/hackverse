import requests, time, sys

print("1. Simulate DB Outage")
requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": "default"})
# Trigger via /pay
try: requests.get("http://localhost:8002/pay", timeout=2)
except: pass

t0 = time.time()
resolved = False
for _ in range(40):
    try:
        data = requests.get("http://localhost:8000/api/incidents").json()
        if len(data) > 0 and data[0]['status'] == 'resolved':
            resolved = True
            break
    except: pass
    time.sleep(0.5)

print(f"Time to resolve: {time.time() - t0:.2f}s" if resolved else "FAIL: Not resolved")
requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})


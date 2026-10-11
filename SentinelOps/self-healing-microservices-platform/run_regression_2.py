import requests, time, sys

print("2. Simulate Cache Outage")
requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": "cache"})
try: requests.get("http://localhost:8002/pay", timeout=2)
except: pass

t0 = time.time()
pending_id = None
for _ in range(40):
    try:
        data = requests.get("http://localhost:8000/api/incidents").json()
        if len(data) > 0 and data[0]['status'] == 'pending_approval':
            pending_id = data[0]['id']
            break
    except: pass
    time.sleep(0.5)

print(f"Time to pending_approval: {time.time() - t0:.2f}s" if pending_id else "FAIL: Not pending_approval")

if pending_id:
    requests.post(f"http://localhost:8000/api/incidents/{pending_id}/approve")
    print("Approved incident.")

for _ in range(10):
    data = requests.get("http://localhost:8000/api/incidents").json()
    if len(data) > 0 and data[0]['status'] == 'resolved':
        print("Successfully resolved.")
        break
    time.sleep(0.5)

requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})

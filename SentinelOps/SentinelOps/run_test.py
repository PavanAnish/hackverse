import requests, time, os, json

def run_test(name, chaos_type, expect_status, check_after):
    print(f"\n--- {name} ---")
    os.system("python reset.py")
    os.system("docker exec sentinelops-postgres psql -U postgres -d sentinelops -c \"TRUNCATE TABLE incidents RESTART IDENTITY;\" > NUL 2>&1")
    time.sleep(1)
    
    requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": chaos_type})
    print(f"Chaos {chaos_type} started")
    
    start = time.time()
    for _ in range(5):
        try:
            r = requests.get("http://localhost:8002/pay")
            if r.status_code in [500, 503]:
                print(f"Triggered {r.status_code} on /pay at {time.time()-start:.2f}s")
                break
        except:
            pass
        time.sleep(0.5)

    start = time.time()
    while time.time() - start < 30:
        try:
            inc = requests.get("http://localhost:8000/api/incidents").json()
            if inc:
                st = inc[0]["status"]
                print(f"Incident {inc[0]['id']} status {st} at {time.time()-start:.2f}s")
                if st == expect_status:
                    if check_after:
                        # Wait a bit then check again (e.g. approve)
                        break
                    else:
                        break
        except Exception:
            pass
        time.sleep(1)
    
    if check_after and inc and inc[0]["status"] == "pending_approval":
        time.sleep(1)
        print("Approving...")
        requests.post(f"http://localhost:8000/api/incidents/{inc[0]['id']}/approve")
        start = time.time()
        while time.time() - start < 15:
            inc = requests.get("http://localhost:8000/api/incidents").json()
            if inc and inc[0]["status"] == "resolved":
                print(f"Incident {inc[0]['id']} resolved at {time.time()-start:.2f}s after approval")
                break
            time.sleep(1)

run_test("DB Outage 1", "default", "resolved", False)
run_test("DB Outage 2", "default", "resolved", False)
run_test("DB Outage 3", "default", "resolved", False)
run_test("Cache Outage", "cache", "pending_approval", True)

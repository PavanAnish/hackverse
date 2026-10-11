import requests

def run_tests():
    print("Test B(a): Old route")
    requests.post("http://localhost:8002/chaos/fail?enable=true&type=default")
    r1 = requests.get("http://localhost:8002/pay")
    if r1.status_code != 500: raise Exception(f"Expected 500, got {r1.status_code}")
    requests.post("http://localhost:8002/chaos/fail?enable=false")
    r2 = requests.get("http://localhost:8002/pay")
    if r2.status_code != 200: raise Exception(f"Expected 200, got {r2.status_code}")
    print("B(a) PASS")

    print("Test B(b): New token route")
    requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": "default"})
    r3 = requests.get("http://localhost:8002/pay")
    if r3.status_code != 500: raise Exception(f"Expected 500, got {r3.status_code}")
    requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})
    r4 = requests.get("http://localhost:8002/pay")
    if r4.status_code != 200: raise Exception(f"Expected 200, got {r4.status_code}")
    print("B(b) PASS")

    print("Test B(c): CORS preflight")
    requests.post("http://localhost:8002/chaos/token", json={"enable": True, "type": "default"})
    r5 = requests.options("http://localhost:8002/chaos/token", headers={"Origin": "http://localhost:3002", "Access-Control-Request-Method": "POST"})
    if r5.status_code != 200: raise Exception(f"Expected 200, got {r5.status_code}")
    if "Access-Control-Allow-Origin" not in r5.headers: raise Exception("Missing CORS headers")
    
    requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})
    print("B(c) PASS")

if __name__ == "__main__":
    run_tests()

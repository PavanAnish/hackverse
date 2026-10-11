import requests, redis
print("Turning off chaos...")
requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})
r = redis.Redis(host='localhost', port=6379, db=3)
keys = r.keys('remediation:*') + r.keys('notified:*')
if keys:
    r.delete(*keys)
print("Cleared redis.")

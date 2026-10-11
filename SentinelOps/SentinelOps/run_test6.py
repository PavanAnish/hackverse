import requests

print("Current stock:", requests.get("http://localhost:8003/stock").json())
print("Decreasing 3...")
res = requests.post("http://localhost:8003/stock/decrement", json={"item_ids": [3]})
print("Response:", res.text)
print("Current stock:", requests.get("http://localhost:8003/stock").json())


import requests

def test_flow():
    print("Fetching stock...")
    s1 = requests.get("http://localhost:8003/stock").json()
    print("Initial stock:", s1)

    items = [
        {"id": 3, "name": "Cappuccino", "price": 4.25}, 
        {"id": None, "name": "Espresso", "price": 3.25}
    ]

    print("Submitting order...")
    res = requests.post("http://localhost:8001/orders", json={"items": items, "total": 7.50})
    print("Order status:", res.status_code, res.text)

    itemIds = [i["id"] for i in items if i["id"] is not None]
    if itemIds:
        print("Decreasing stock for:", itemIds)
        res = requests.post("http://localhost:8003/stock/decrement", json={"item_ids": itemIds})
        print("Stock decrement status:", res.status_code, res.text)
    
    print("Fetching stock...")
    s2 = requests.get("http://localhost:8003/stock").json()
    print("Final stock:", s2)

test_flow()

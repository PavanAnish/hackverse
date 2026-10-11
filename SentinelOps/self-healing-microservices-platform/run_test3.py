import requests, time

def test_store():
    # Add to cart is local state, but hitting /pay works if no chaos.
    try: requests.post("http://localhost:8002/chaos/token", json={"enable": False, "type": "default"})
    except: pass
    time.sleep(2)
    
    # test checkout
    res = requests.get("http://localhost:8002/pay")
    if res.status_code == 200:
        print("Checkout /pay: PASS")
    else:
        print(f"Checkout /pay: FAIL ({res.status_code})")
        
    # test orders
    try:
        res = requests.post("http://localhost:8001/orders", json={"items": [{"id": 1}], "total": 1299.99})
        if res.status_code in (200, 201):
            print("Orders: PASS")
        else:
            print(f"Orders: FAIL ({res.status_code})")
    except: print("Orders: FAIL (exception)")
    
    # test stock
    try:
        res = requests.post("http://localhost:8003/stock/decrement", json={"item_ids": [1]})
        if res.status_code == 200:
            print("Stock: PASS")
        else:
            print(f"Stock: FAIL ({res.status_code})")
    except: print("Stock: FAIL (exception)")

test_store()

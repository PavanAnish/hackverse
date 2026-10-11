import redis
r = redis.Redis(host='localhost', port=6379, db=3)
keys = r.keys('remediation:*') + r.keys('notified:*')
for k in keys:
    ttl = r.ttl(k)
    k_type = r.type(k).decode()
    if k_type == 'string':
        val = r.get(k)
    elif k_type == 'hash':
        val = r.hgetall(k)
    else:
        val = f"<{k_type}>"
    print(f"Key: {k.decode()}, TTL: {ttl}, Val: {val}")

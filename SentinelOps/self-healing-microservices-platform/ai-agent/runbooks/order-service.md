# Order Service Runbook

## Cache Saturation
**Symptoms**: order-service degraded performance and slow response times.
**Root Cause**: Redis cache is saturated with stale data.
**Resolution**: Clear the cache.

Action: `clear_cache`
Target: `order-service`

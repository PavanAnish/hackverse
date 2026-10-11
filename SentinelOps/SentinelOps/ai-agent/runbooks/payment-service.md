# Payment Service Runbook

## Stuck DB Connection Pool
**Symptoms**: payment-service error rate spikes with rising latency but CPU is normal.
**Root Cause**: DB connection pool is exhausted and stuck.
**Resolution**: Restarting the container resolves it.

Action: `restart_container`
Target: `payment-service`

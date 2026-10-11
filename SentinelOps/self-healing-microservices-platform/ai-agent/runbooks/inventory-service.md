# Inventory Service Runbook

## High Load on Stock Updates
**Symptoms**: inventory-service errors out on stock update with high CPU usage.
**Root Cause**: Service cannot handle the sudden burst of requests.
**Resolution**: Scale replicas to handle load.

Action: `scale_replicas`
Target: `inventory-service`

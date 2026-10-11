# Analyst API

All endpoints are `GET` only (Read-Only) mounted on port 8094.

* `GET /health`
* `GET /api/v1/incidents`
* `GET /api/v1/incidents/{id}`
* `GET /api/v1/incidents/{id}/timeline` - Unified chronologically sorted history of Findings, Runtime Events, and Recommendations.
* `GET /api/v1/incidents/{id}/risk` - Current static/dynamic/combined risk.
* `GET /api/v1/incidents/{id}/attack-chain` - Evidence-based attack chain state.
* `GET /api/v1/incidents/{id}/recommendations` - List of proposed analyst actions.

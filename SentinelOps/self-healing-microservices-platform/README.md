# SentinelOps

SentinelOps is a self-healing microservices platform built to detect, diagnose, and resolve production incidents autonomously. Using a combination of real-time monitoring and an AI agent, it identifies failing services and executes bounded, safe remediations without human intervention, significantly reducing mean time to recovery (MTTR).

## Architecture

```text
+--------------+       Events       +-----------+
|              | -----------------> |           |
| Microservices|                    | RabbitMQ  |
|              | <----------------- |           |
+--------------+     Remediate      +-----------+
      |                                   |
      | Metrics/Logs                      | Consume
      v                                   v
+--------------+                    +-----------+
|              |                    |           |
|  Postgres /  |                    | AI Agent  |
|    Redis     |                    |           |
|              |                    +-----------+
+--------------+                          |
      |                                   | Status/Logs
      | Data                              |
      v                                   v
+-----------------------------------------------+
|                                               |
|                  Dashboard                    |
|                                               |
+-----------------------------------------------+
```

## Safety Mechanisms

This is the most critical part of SentinelOps. AI interventions are strictly bounded to prevent catastrophic cascading failures or unpredictable behavior in production environments.

* **Bounded Action Space**: The AI agent cannot execute arbitrary code or shell commands. It is restricted to a predefined list of `enum-only` actions (e.g., `RestartService`, `ClearCache`, `KillDatabaseLock`).
* **Circuit Breaker**: To prevent remediation loops, the system implements a strict rate limit. It allows a maximum of **2 auto-remediations per 10 minutes**. If a service continues to fail after reaching this limit, the AI agent stops acting and escalates the issue to a human operator.
* **Escalation Notification**: On escalation, the system automatically notifies a human via Discord/Slack webhook, so no failure silently sits unresolved.

## Quick Start

1. **Clone the repository**
   ```bash
   git clone https://github.com/your-username/sentinelops.git
   cd sentinelops
   ```

2. **Configure Environment**
   ```bash
   cp .env.example .env
   # Edit .env and add your GEMINI_API_KEY
   ```

3. **Start the Platform**
   ```bash
   make up
   ```

4. **Trigger a Chaos Event**
   Test the self-healing in action by introducing a simulated failure:
   ```bash
   make chaos-db-lock
   ```

5. **Access the Interfaces**
   Open your browser to explore the platform:
   - **Customer Storefront:** **[http://localhost:3002](http://localhost:3002)** (Shop, checkout, and trigger simulated outages)
   - **Ops Dashboard:** **[http://localhost:3001](http://localhost:3001)** (Watch the AI detect, diagnose, and resolve the issues live)

## Tech Stack

* **Frontend**: React, Vite, Tailwind CSS
* **Backend Services**: Python, FastAPI
* **Message Broker**: RabbitMQ
* **Database / Cache**: PostgreSQL, Redis
* **AI Agent**: Gemini API
* **Infrastructure**: Docker, Docker Compose

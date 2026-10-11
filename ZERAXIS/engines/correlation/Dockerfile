# ---- Build stage ----
FROM golang:1.27-alpine AS builder

WORKDIR /app

# Copy shared events package first (for cache efficiency)
COPY pkg/events ./pkg/events

# Copy correlation engine sources
COPY engines/correlation/go.mod engines/correlation/go.sum* ./engines/correlation/
WORKDIR /app/engines/correlation
RUN go mod download

COPY engines/correlation/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /correlation-engine ./cmd/correlation

# ---- Runtime stage ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /correlation-engine /correlation-engine
COPY engines/correlation/migrations/ /migrations/

# Unprivileged: no Docker socket, no host mounts, no capabilities, no privileged mode
USER nobody

ENTRYPOINT ["/correlation-engine"]

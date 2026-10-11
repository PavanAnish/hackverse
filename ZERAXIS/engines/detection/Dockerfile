# ---- Build stage ----
FROM golang:1.27-alpine AS builder

WORKDIR /app

# Copy shared packages
COPY pkg/events ./pkg/events

# Copy detection engine sources
COPY engines/detection/go.mod engines/detection/go.sum* ./engines/detection/
WORKDIR /app/engines/detection
RUN go mod download

COPY engines/detection/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /detection-engine .

# ---- Runtime stage ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /detection-engine /detection-engine

# Unprivileged: no Docker socket, no host mounts, no capabilities, no privileged mode
USER nobody

ENTRYPOINT ["/detection-engine"]

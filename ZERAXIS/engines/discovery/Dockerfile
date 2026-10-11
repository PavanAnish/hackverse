FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY pkg/events ./pkg/events
COPY engines/discovery/go.mod engines/discovery/go.sum ./engines/discovery/
WORKDIR /app/engines/discovery
RUN go mod download
COPY engines/discovery/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /discovery-engine ./cmd/discovery

FROM alpine:3.20
# Alpine (not scratch) because the Docker client needs DNS resolution
# and ca-certificates for potential HTTPS connections
RUN apk add --no-cache ca-certificates
COPY --from=builder /discovery-engine /discovery-engine
COPY engines/discovery/migrations/ /migrations/
ENV API_PORT=8090
EXPOSE 8090
ENTRYPOINT ["/discovery-engine"]

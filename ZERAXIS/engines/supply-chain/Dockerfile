FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY pkg/events ./pkg/events
COPY engines/supply-chain/ ./engines/supply-chain/
WORKDIR /app/engines/supply-chain
RUN CGO_ENABLED=0 GOOS=linux go build -o /supply-chain-engine ./cmd/supply-chain

FROM alpine:3.20

# Install Syft & Trivy
RUN apk add --no-cache curl ca-certificates && \
    curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b /usr/local/bin v1.51.0 && \
    curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b /usr/local/bin v0.74.0

COPY --from=builder /supply-chain-engine /supply-chain-engine
COPY engines/supply-chain/migrations/ /migrations/

ENV API_PORT=8091
EXPOSE 8091
ENTRYPOINT ["/supply-chain-engine"]

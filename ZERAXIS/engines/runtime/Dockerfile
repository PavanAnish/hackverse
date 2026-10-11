FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY pkg/events ./pkg/events
COPY engines/runtime/ ./engines/runtime/
WORKDIR /app/engines/runtime
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /runtime-engine ./cmd/runtime

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /runtime-engine /runtime-engine
COPY engines/runtime/migrations/ /migrations/

ENV API_PORT=8092
EXPOSE 8092
ENTRYPOINT ["/runtime-engine"]

# Stage 1: Build
FROM golang:1.27-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/engines/shield
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /shield ./cmd/shield

# Stage 2: Runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /shield /usr/local/bin/shield
EXPOSE 8080 9090
ENTRYPOINT ["shield"]

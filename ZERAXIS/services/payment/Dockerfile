FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /payment-service

FROM scratch
COPY --from=builder /payment-service /payment-service
ENV PORT=8083
EXPOSE 8083
ENTRYPOINT ["/payment-service"]

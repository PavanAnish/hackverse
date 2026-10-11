FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /product-service

FROM scratch
COPY --from=builder /product-service /product-service
ENV PORT=8082
EXPOSE 8082
ENTRYPOINT ["/product-service"]

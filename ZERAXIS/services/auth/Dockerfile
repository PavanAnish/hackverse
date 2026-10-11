FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /auth-service

FROM scratch
COPY --from=builder /auth-service /auth-service
ENV PORT=8081
EXPOSE 8081
ENTRYPOINT ["/auth-service"]

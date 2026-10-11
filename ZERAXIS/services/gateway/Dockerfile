FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /gateway

FROM scratch
COPY --from=builder /gateway /gateway
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/gateway"]
LABEL org.zeraxis.test="identity-test-1"

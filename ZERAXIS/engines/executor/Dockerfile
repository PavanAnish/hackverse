FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY . .
RUN cd engines/executor && go build -o /app/executor cmd/executor/main.go

FROM alpine:3.18
# Needs to access docker socket, which typically requires root or docker group.
# We will use root here to ensure access to the socket.
WORKDIR /app
COPY --from=builder /app/executor .
CMD ["./executor"]

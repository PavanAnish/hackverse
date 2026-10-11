module github.com/pavanvanish/zeraxis/engines/correlation

go 1.27.0

require (
	github.com/google/uuid v1.6.0
	github.com/lib/pq v1.12.3
	github.com/nats-io/nats.go v1.53.1
	github.com/pavanvanish/zeraxis/pkg/events v0.0.0-00010101000000-000000000000
)

require (
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

replace github.com/pavanvanish/zeraxis/pkg/events => ../../pkg/events

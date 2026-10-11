module github.com/pavanvanish/zeraxis/engines/detection

go 1.27.0

replace github.com/pavanvanish/zeraxis/pkg/events => ../../pkg/events

require (
	github.com/google/cel-go v0.20.1
	github.com/google/uuid v1.6.0
	github.com/lib/pq v1.12.3
	github.com/nats-io/nats.go v1.53.1
	github.com/pavanvanish/zeraxis/pkg/events v0.0.0-00010101000000-000000000000
	google.golang.org/genproto/googleapis/api v0.0.0-20230803162519-f966b187b2e5
)

require (
	github.com/antlr4-go/antlr/v4 v4.13.0 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/stoewer/go-strcase v1.2.0 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/exp v0.0.0-20230515195305-f3d0a9c9a5cc // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230803162519-f966b187b2e5 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
)

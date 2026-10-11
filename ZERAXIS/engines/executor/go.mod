module github.com/pavanvanish/zeraxis/engines/executor

go 1.27.0

require (
	github.com/docker/docker v20.10.24+incompatible
	github.com/google/uuid v1.6.0
	github.com/lib/pq v1.12.3
	github.com/nats-io/nats.go v1.53.1
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.7.2-default-no-op // indirect
	github.com/docker/distribution v2.8.3+incompatible // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/morikuni/aec v1.1.0 // indirect
	github.com/nats-io/jwt/v2 v2.8.2 // indirect
	github.com/nats-io/nats-server/v2 v2.14.6 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pavanvanish/zeraxis/engines/correlation v0.0.0-00010101000000-000000000000 // indirect
	github.com/pavanvanish/zeraxis/pkg/events v0.0.0-00010101000000-000000000000 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	gotest.tools/v3 v3.5.2 // indirect
)

replace github.com/docker/distribution => github.com/docker/distribution v2.8.2+incompatible

replace github.com/pavanvanish/zeraxis/pkg/events => ../../pkg/events

replace github.com/pavanvanish/zeraxis/engines/correlation => ../correlation

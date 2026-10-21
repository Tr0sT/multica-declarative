.PHONY: build test vet fmt check integration integration-teambridge

build:
	go build -o ./bin/multica-declarative ./cmd/multica-declarative

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

check:
	@test -z "$$(gofmt -l .)" || (echo "Run gofmt on:"; gofmt -l .; exit 1)
	go vet ./...
	go test ./...
	@output="$$(mktemp)"; trap 'rm -f "$$output"' EXIT; go build -o "$$output" ./cmd/multica-declarative

# Explicit opt-in: MULTICA_BIN must point to the official version in integration/multica.lock.json.
integration:
	go test -race -tags=integration ./...

# Explicit opt-in: MULTICA_BIN must be the fork revision in integration/multica-teambridge.lock.json.
integration-teambridge:
	go test -race -tags='integration teambridgeintegration' ./integration -run '^TestForkCLITeamBridge'

.PHONY: build test vet fmt check integration

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

# Opt-in CLI build for emoji writes; stock 0.4.42 can export but has no setter.
.PHONY: avatar-cli integration-emoji
avatar-cli:
	bash scripts/build-avatar-cli.sh

# Both MULTICA_BIN (stock) and MULTICA_EMOJI_BIN (patched) are mandatory.
integration-emoji:
	go test -race -tags=integration,emoji ./integration -run Emoji

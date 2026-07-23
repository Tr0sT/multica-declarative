.PHONY: build test vet fmt check

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

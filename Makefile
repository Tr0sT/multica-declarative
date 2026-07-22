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
	go build ./cmd/multica-declarative

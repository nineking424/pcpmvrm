.PHONY: build test test-int lint fmt clean

build:
	go build -o bin/pcp ./cmd/pcp

test:
	go test -race ./internal/...

test-int:
	go test -race ./tests/integration/...

lint:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin/ coverage.out

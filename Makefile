.PHONY: build install test vet

build:
	go build ./cmd/lazycssh

install:
	go install ./cmd/lazycssh

test:
	go test ./...

vet:
	go vet ./...

.PHONY: help build test race fmt vet lint tidy clean

help:
	@echo "build test race fmt vet lint tidy clean"

build:
	go build ./...

test:
	go test ./...

race:
	go test -race ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

lint: vet

tidy:
	go mod tidy

clean:
	go clean ./...

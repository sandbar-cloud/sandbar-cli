.PHONY: build test lint clean

VERSION ?= dev

build:
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o sandbar .

test:
	go test ./... -v

lint:
	golangci-lint run

clean:
	rm -f sandbar
	rm -rf dist/

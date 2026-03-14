SHELL := /bin/bash

.PHONY: all proto build test lint fmt clean

all: build

proto:
	buf generate buf.build/agynio/api --path agynio/api/authorization/v1

build:
	GOFLAGS=-mod=mod go build ./...

test:
	GOFLAGS=-mod=mod go test ./...

lint:
	GOFLAGS=-mod=mod go vet ./...

fmt:
	gofmt -w $(shell find . -type f -name '*.go')

clean:
	rm -rf gen

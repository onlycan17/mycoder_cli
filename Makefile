SHELL := /bin/bash

.PHONY: all fmt fmt-check lint test build run hook-install

all: fmt lint test

fmt:
	@echo "[fmt] gofmt -s -w ."
	@gofmt -s -w .

fmt-check:
	@echo "[fmt-check] verifying formatting"
	@out=$$(gofmt -s -l .); if [ -n "$$out" ]; then echo "Files need formatting:"; echo "$$out"; exit 1; fi

lint:
	@echo "[lint] go vet ./..."
	@go vet ./...

test:
	@echo "[test] go test ./..."
	@go test ./...

build:
	@echo "[build] building mycoder"
	@go build -o bin/mycoder ./cmd/mycoder

run: build
	@./bin/mycoder serve

hook-install:
	@echo "[hooks] installing pre-commit hook"
	@mkdir -p .git/hooks
	@install -m 0755 scripts/pre-commit.sh .git/hooks/pre-commit
	@echo "[hooks] installed .git/hooks/pre-commit"

SHELL := /bin/bash

.PHONY: all fmt fmt-check lint test build install uninstall run hook-install print-path-hint smoke

# 사용자 설치 경로 (기본: $HOME/.mycoder)
PREFIX ?= $(HOME)/.mycoder
BINDIR := $(PREFIX)/bin
PORT ?= 8089

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

install: build
	@echo "[install] installing mycoder to $(BINDIR)"
	@mkdir -p $(BINDIR)
	@install -m 0755 bin/mycoder $(BINDIR)/mycoder
	@$(MAKE) print-path-hint

uninstall:
	@echo "[uninstall] removing $(BINDIR)/mycoder"
	@rm -f $(BINDIR)/mycoder

print-path-hint:
	@echo "[hint] PATH 설정(한 번만 필요):"
	@echo '  echo '\''export PATH="$(HOME)/.mycoder/bin:$$PATH"'\'' >> ~/.bashrc  # 또는 ~/.zshrc'
	@echo '  source ~/.bashrc  # 또는 source ~/.zshrc'
	@echo "[hint] 확인:"
	@echo '  which mycoder && mycoder version || echo "새 터미널을 열어주세요."'

smoke:
	@echo "[smoke] PATH에 설치된 mycoder 확인 (BINDIR=$(BINDIR), PORT=$(PORT))"
	@PATH="$(BINDIR):$$PATH"; \
	set -e; \
	which mycoder >/dev/null 2>&1 || (echo "[smoke] mycoder 가 PATH 에 없습니다"; exit 1); \
	printf "[smoke] which mycoder -> "; which mycoder; \
	printf "[smoke] mycoder version -> "; mycoder version || (echo "[smoke] version 실패"; exit 1); \
	echo "[smoke] 서버 헬스체크 시작"; \
	LOG=$$(mktemp -t mycoder-serve.XXXXXX.log); \
	(mycoder serve --addr :$(PORT) >"$$LOG" 2>&1 &) ; PID=$$!; \
	cleanup() { kill $$PID >/dev/null 2>&1 || true; }; trap cleanup EXIT; \
	for i in $$(seq 1 30); do \
		if curl -sSf http://localhost:$(PORT)/healthz >/dev/null 2>&1; then \
			echo "[smoke] /healthz OK"; \
			break; \
		fi; \
		sleep 0.2; \
		if ! kill -0 $$PID >/dev/null 2>&1; then \
			echo "[smoke] 서버 프로세스가 비정상 종료되었습니다"; \
			echo "---- server log ----"; \
			cat "$$LOG"; \
			exit 1; \
		fi; \
		if [ $$i -eq 30 ]; then \
			echo "[smoke] 서버 헬스체크 타임아웃"; \
			echo "---- server log ----"; \
			cat "$$LOG"; \
			exit 1; \
		fi; \
	done; \
	cleanup; trap - EXIT; \
	echo "[smoke] OK"

run: build
	@./bin/mycoder serve

hook-install:
	@echo "[hooks] installing pre-commit hook"
	@mkdir -p .git/hooks
	@install -m 0755 scripts/pre-commit.sh .git/hooks/pre-commit
	@echo "[hooks] installed .git/hooks/pre-commit"

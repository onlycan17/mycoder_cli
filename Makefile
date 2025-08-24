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

# Seed RAG knowledge using the CLI.
# Usage: make seed-rag PROJECT=<project-id> [WEB=path/to/web.json] [DRY=1]
seed-rag: build
	@if [ -z "$(PROJECT)" ]; then echo "PROJECT is required (project ID)"; exit 1; fi
	@echo "[seed] docs+code seeds to project $(PROJECT)"
	@BIN="bin/mycoder"; \
	if [ "$(DRY)" = "1" ]; then DRYFLAG="--dry-run"; else DRYFLAG=""; fi; \
	$$BIN seed rag --project $(PROJECT) --docs --code $$DRYFLAG; \
	if [ -n "$(WEB)" ]; then echo "[seed] web ingest from $(WEB)"; $$BIN seed rag --project $(PROJECT) --docs=false --code=false --web-json $(WEB) $$DRYFLAG; fi

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

# Seed Spring Boot docs and optional web references
# Usage: make seed-spring PROJECT=<project-id> [DRY=1]
seed-spring: build
	@if [ -z "$(PROJECT)" ]; then echo "PROJECT is required (project ID)"; exit 1; fi
	@BIN="bin/mycoder"; \
	if [ "$(DRY)" = "1" ]; then DRYFLAG="--dry-run"; else DRYFLAG=""; fi; \
	$$BIN knowledge promote-auto --project $(PROJECT) --title "Spring Boot Overview" --files docs/springboot/OVERVIEW.md $$DRYFLAG --pin; \
	$$BIN knowledge promote-auto --project $(PROJECT) --title "Spring Annotations" --files docs/springboot/ANNOTATIONS.md $$DRYFLAG --pin; \
	$$BIN knowledge promote-auto --project $(PROJECT) --title "REST Patterns" --files docs/springboot/REST.md $$DRYFLAG --pin; \
	$$BIN knowledge promote-auto --project $(PROJECT) --title "Spring Data JPA" --files docs/springboot/DATA_JPA.md $$DRYFLAG --pin; \
	$$BIN knowledge promote-auto --project $(PROJECT) --title "Spring Config" --files docs/springboot/CONFIG.md $$DRYFLAG --pin; \
	$$BIN knowledge promote-auto --project $(PROJECT) --title "Spring Testing" --files docs/springboot/TESTING.md $$DRYFLAG --pin; \
	$$BIN knowledge promote-auto --project $(PROJECT) --title "Spring Security" --files docs/springboot/SECURITY.md $$DRYFLAG --pin; \
	$$BIN knowledge promote-auto --project $(PROJECT) --title "Actuator" --files docs/springboot/ACTUATOR.md $$DRYFLAG --pin; \
	$$BIN knowledge promote-auto --project $(PROJECT) --title "Spring Build/Run" --files docs/springboot/BUILD.md $$DRYFLAG --pin; \
	$$BIN seed rag --project $(PROJECT) --docs=false --code=false --web-json docs/springboot/web_refs.json $$DRYFLAG

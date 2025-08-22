# mycoder — 프로젝트 인지형 코딩 CLI

mycoder는 로컬/사내 코드베이스를 색인하고 검색·질의(RAG)하며, 지식을 축적·검증·승격할 수 있는 경량 HTTP 서버 + CLI입니다. 개발 워크플로우에서 테스트/린트/포맷 훅을 강제하고, LLM(OpenAI 호환)을 연동하여 코드 맥락 기반 질의응답과 요약을 도와줍니다.

## TL;DR (빠른 시작)
- 빌드: `make build`
 - 전역 설치: `make install` 후 `mycoder` 바로 실행 가능
- 서버 실행: `./bin/mycoder serve --addr :8089`
- 프로젝트 생성: `mycoder projects create --name demo --root .`
- 인덱싱: `mycoder index --project <id> --mode full`
- 검색: `mycoder search "handler" --project <id>`
- Q&A: `mycoder ask --project <id> "이 서버의 /metrics 구현 요약"`
- 스트리밍 대화: `mycoder chat --project <id> "server.go 설명"`
  - 옵션: `--retries N`(스트림 오류 시 자동 재시도), `--tty`(진행 상태를 stderr에 간단히 표시)
- MCP 도구: `mycoder mcp tools` / `mycoder mcp call --name echo --json '{"text":"hi"}'`
- 지식 추가/검증/승격: `mycoder knowledge ...` (아래 참고)
- 메트릭: `mycoder metrics` (Prometheus 텍스트 포맷, `?format=json` 지원)
- 훅 실행: `mycoder hooks run --project <id> [--targets fmt-check,test,lint] [--timeout 60] [--verbose] [--save path/to/hooks.json]`
  - 실패 시 요약(✅/❌)과 힌트(suggestion) 출력. 예) 포맷 실패 → `make fmt` 제안
  - `--save`: 프로젝트 루트 상대 경로로 구조화 결과 JSON 아카이브(타겟별 ok/output/suggestion/소요/라인/바이트, reason)

필수: Go 1.21+, (선택) LLM 서버(OpenAI 호환, LM Studio 등)

## 주요 기능
- 프로젝트 색인/검색: 파일 워커, 텍스트 추출, FTS5 기반 검색, 라인/프리뷰 제공
- RAG 질의: 검색 상위 K 맥락을 활용한 Q&A/대화(LLM 연동)
- 지식 큐레이션: 수동 추가/자동 승격(promote-auto), 재검증, 신뢰점수 기반 정리(gc)
- REST API: 프로젝트/인덱싱/검색/지식/파일/쉘/챗 엔드포인트(스켈레톤 포함)
- CLI UX: `projects/index/search/ask/chat/models/metrics/knowledge` 등 일상 작업 지원
- 안정성: 최소 호출 간격 및 429/5xx 백오프(LLM 클라이언트)
- 품질 게이트: `make fmt && make test && make lint` 훅으로 커밋 차단

## 아키텍처 개요
- `cmd/mycoder`: CLI 엔트리포인트
- `internal/server`: HTTP 서버 및 REST 핸들러(`/projects`, `/index/*`, `/search`, `/chat`, `/knowledge`, `/fs/*`, `/shell/*`, `/metrics`)
- `internal/indexer`: 파일 트래버스/텍스트 수집/크기 제한/바이너리 제외
- `internal/store`: 인메모리/SQLite 저장소, FTS5 검색, 지식 스키마
- `internal/llm`: OpenAI 호환 어댑터(챗/임베딩), 재시도/백오프
- `internal/models`: 데이터 모델(프로젝트/문서/잡/검색결과/지식 등)
- `internal/version`: 빌드 메타(버전/커밋)

자세한 설계는 `docs/ARCHITECTURE.md`, API는 `docs/API.md`를 참고하세요.

## 설치
- 요구사항: Go 1.21+, macOS/Linux
- 의존성: `go mod tidy`로 자동 관리(이미 포함)

빌드 및 실행
```bash
make build
./bin/mycoder serve --addr :8089
```

개발용 실행(자동 포맷/테스트/린트 권장)
```bash
make fmt-check && make test && make lint
```

pre-commit 훅 설치(강력 추천)
```bash
make hook-install
```
- 커밋 전 자동으로 `make fmt && make fmt-check && make test && make lint`를 실행합니다
- 포맷팅 변경은 자동 스테이징되어 커밋 일관성을 보장합니다

## 설정(환경 변수/설정 파일)
- `MYCODER_SERVER_URL`: CLI가 붙을 서버 주소(기본 `http://localhost:8089`)
- `MYCODER_SQLITE_PATH`: SQLite 파일 경로 지정 시 영구 저장(미지정 시 메모리)
- `MYCODER_LLM_PROVIDER`: `openai`(기본)
- `MYCODER_OPENAI_BASE_URL`: OpenAI 호환 서버 URL
  - LM Studio 예: `http://localhost:1234/v1` 또는 사내 LLM 게이트웨이 URL
- `MYCODER_OPENAI_API_KEY`: 인증 필요 시 API 키
- `MYCODER_DISABLE_EMBEDDINGS`: `1`이면 임베딩/벡터 검색 비활성화(안전 폴백)
- `MYCODER_CHAT_MAX_CHARS`: 대화 히스토리 슬라이딩 윈도우 문자 예산(기본 6000). 시스템 메시지는 항상 우선 포함.
- 큐레이터(자동 재검증/정리) 관련
  - `MYCODER_CURATOR_DISABLE`: 비우면 활성, 값 설정 시 비활성
  - `MYCODER_CURATOR_INTERVAL`: 주기(`10m` 기본)
  - `MYCODER_KNOWLEDGE_MIN_TRUST`: 정리 기준 최소 신뢰점수(`0.4` 기본)

## CLI 사용법
도움말: `mycoder help`

핵심 명령
- 서버 실행: `mycoder serve [--addr :8089]`
- 버전 확인: `mycoder version`
- 프로젝트: `mycoder projects [list|create]`
  - 생성: `mycoder projects create --name demo --root .`
- 인덱싱: `mycoder index --project <id> [--mode full|incremental]`
- 검색: `mycoder search "<query>" [--project <id>]`
- Q&A: `mycoder ask [--project <id>] [--k 5] "<질문>"`
- 대화(SSE): `mycoder chat [--project <id>] [--k 5] "<프롬프트>"`
 - 모델 목록: `mycoder models` (OpenAI 호환 `/v1/models` 결과)
   - 옵션: `--format table|json|raw`, `--filter <substr>`, `--color`
 - 메트릭: `mycoder metrics` (Prometheus 텍스트 기본, `?format=json` 지원)
   - 옵션: `--json`(JSON pretty), `--color`(텍스트 키 컬러)
- 훅 실행: `mycoder hooks run --project <id> [--targets ...] [--timeout 60] [--verbose]`
  - 서버 API: `POST /tools/hooks` (`env` 화이트리스트 지원: `GOFLAGS` 등)
- 테스트만 실행: `mycoder test --project <id> [--timeout 60] [--verbose]`
 - 파일/FS: `mycoder fs read|write|patch|delete --project <id> --path <p> [--content ...] [--start N --length N --replace ...]`
   - 안전장치: `--dry-run`으로 미리보기, `--yes` 없으면 적용 거부(write/delete/patch)
   - 대량 변경 감지: `--large-threshold-bytes` 초과 시 차단, `--allow-large`로 우회
- 터미널 실행: `mycoder exec --project <id> -- -- <cmd> [args...]` (비스트리밍, 타임아웃/작업디렉토리/환경 전달 지원)
   - 스트리밍: `mycoder exec --project <id> --stream -- -- <cmd> [args...]` (SSE: stdout/stderr/exit)
   - 출력 제한: 비스트리밍 `--tail N`, `--max-bytes N`; 스트리밍 `--stream-tail N`

### 간편 실행: `mycoder`
- 전역 설치: `make install` (기본 설치 경로: `$HOME/.mycoder/bin/mycoder`)
  - 제거: `make uninstall`
- PATH 설정(처음 1회 권장):
  - 설치 완료 후 자동으로 PATH 설정 힌트가 출력됩니다.
  - 수동 설정: `echo 'export PATH="$HOME/.mycoder/bin:$PATH"' >> ~/.bashrc` (또는 `~/.zshrc`), 이후 새 셸 또는 `source ~/.bashrc`
  - 언제든지 힌트 재출력: `make print-path-hint`
- 동작 확인: `make smoke`
  - PATH에 설치된 `mycoder` 확인(`which`, `version`) 후 로컬 서버를 기동해 `/healthz` 헬스체크까지 수행
  - 임의 포트 사용: `make smoke PORT=8090`
- 수동 설치/대안:
  - 심볼릭 링크: `ln -sf "$(pwd)/bin/mycoder" "$HOME/.mycoder/bin/mycoder"`
  - 다른 경로로 설치하고 싶다면: `make install PREFIX=$HOME/custom/mc`

지식(knowledge) 명령
- 추가: `mycoder knowledge add --project <id> --type <code|doc|web> --text "..." [--title ...] [--url ...] [--trust 0.0] [--pin]`
- 목록: `mycoder knowledge list --project <id>`
- 검증: `mycoder knowledge vet --project <id>`
- 승격: `mycoder knowledge promote --project <id> --title "..." --text "..." [--url ...] [--commit ...] [--files ...] [--symbols ...] [--pin]`
- 재검증: `mycoder knowledge reverify --project <id>`
- 정리: `mycoder knowledge gc --project <id> [--min 0.5]`
- 자동 승격: `mycoder knowledge promote-auto --project <id> --files "path/a.go,path/b.go" [--title ...] [--pin]`

## API 개요
- 헬스: `GET /healthz`
- 메트릭: `GET /metrics` (Prometheus 텍스트, `?format=json` 시 JSON)
- 프로젝트: `GET/POST /projects`
- 인덱스: `POST /index/run`, `GET /index/jobs/:id`
- 검색: `GET /search?q=...&projectID=...`
- 챗: `POST /chat` (stream=true 시 SSE)
- 지식: `POST /knowledge`, `GET /knowledge`, `POST /knowledge/vet|promote|reverify|gc|promote/auto`
- 파일/쉘: `/fs/read|write|patch|delete`, `/shell/exec|/shell/exec/stream` (보안 경계 준수)

자세한 파라미터와 응답은 `docs/API.md` 참고.

## 메트릭(관측)
- 기본 게이지
  - `mycoder_projects`: 프로젝트 수
  - `mycoder_documents`: 문서(청크) 수
  - `mycoder_jobs`: 인덱스 잡 수
  - `mycoder_knowledge`: 지식 항목 수
- 빌드 정보
  - `mycoder_build_info{version,commit} 1`
- JSON 포맷 필요 시 `GET /metrics?format=json`

## 로그(관측)
- JSON 라인 포맷으로 표준에러(stderr)에 구조화 로그를 출력합니다.
- 레벨: `MYCODER_LOG_LEVEL=debug|info|warn|error` (기본 `info`)
- 요청 로그: `http.req` 이벤트로 메소드/경로/상태/지연/바이트를 기록합니다.
- 민감정보 마스킹: 키/값에 비밀로 추정되는 문자열(`key|token|secret|password|bearer|sk-...`)은 자동 마스킹됩니다.

## 개발 가이드
- 게이트(차단 규칙): `make fmt && make fmt-check && make test && make lint`
- pre-commit 훅 설치: `make hook-install`
- 테스트 실행: `make test`
- 린트: `make lint` (go vet)
- 포맷: `make fmt` / 검증 `make fmt-check`

문서/설계는 `docs/` 하위 파일 참조: PRD, 아키텍처, 데이터모델, API, CLI UX, RAG, TODO, ROADMAP 등.

## LLM 연동 예시
LM Studio(로컬) 예:
```bash
export MYCODER_OPENAI_BASE_URL=http://localhost:1234/v1
export MYCODER_OPENAI_API_KEY=
mycoder models
```
OpenAI(옵션) 예:
```bash
export MYCODER_OPENAI_BASE_URL=https://api.openai.com/v1
export MYCODER_OPENAI_API_KEY=sk-...
mycoder models
```

## 예시 워크플로우
```bash
# 서버 기동
./bin/mycoder serve --addr :8089 &
# 프로젝트 생성
PID=$(mycoder projects create --name demo --root . | jq -r .projectID)
# 인덱싱
mycoder index --project "$PID" --mode full
# 검색
mycoder search "index run" --project "$PID"
# Q&A
mycoder ask --project "$PID" "이 리포의 인덱싱 흐름 설명"
# 자동 승격(파일 요약 후 지식으로 등록)
mycoder knowledge promote-auto --project "$PID" --files "internal/server/server.go" --title "서버 핵심 요약" --pin
```

## 예제 출력

### hooks run (성공 사례)
```bash
$ mycoder hooks run --project $PID --targets fmt-check,test,lint --timeout 60
Hooks summary:
  ✅ fmt-check
  ✅ test
  ✅ lint
```

### hooks run (실패 + 힌트)
```bash
$ mycoder hooks run --project $PID --targets fmt-check,test
Hooks summary:
  ✅ fmt-check
  ❌ test
    Hint: 실패한 테스트를 확인하세요: go test ./... -v (필요 시 -run 으로 타겟팅)
    [test] go test ./...
    --- FAIL: TestSomething (0.00s)
    ...
    FAIL
    exit status 1
```

### fs 안전장치(`--dry-run`/`--yes`)
```bash
$ mycoder fs write --project $PID --path path/to/file.txt --content "hello"
confirmation required: pass --yes to apply or use --dry-run

$ mycoder fs write --project $PID --path path/to/file.txt --content "hello" --dry-run
[dry-run] write path/to/file.txt (len=5)

$ mycoder fs write --project $PID --path path/to/file.txt --content "hello" --yes
{"ok":true}

$ mycoder fs patch --project $PID --path path/to/file.txt --start 0 --length 5 --replace "hi" --dry-run
[dry-run] patch path/to/file.txt start=0 length=5 replace_len=2
```

### exec (비스트리밍) 출력 요약
```bash
# 마지막 50라인/4096바이트만 출력
$ mycoder exec --project $PID --tail 50 --max-bytes 4096 -- -- make test
... (마지막 50라인 중 4096바이트)
[limit] output truncated by server   # 서버 측 64KiB 캡을 넘긴 경우 표시
```

### exec (스트리밍) 요약 + 전송량 제한
```bash
# 스트리밍 중에는 버퍼링하고 종료 후 마지막 100라인을 요약 출력
$ mycoder exec --project $PID --stream --stream-tail 100 -- -- bash -lc 'seq 1 100000'
---- stdout (last 100 lines) ----
...
[limit] output truncated by server   # 서버가 64KiB 초과로 스트림을 제한한 경우
```

### chat (스트리밍) 이벤트 처리
```bash
$ mycoder chat --project $PID "요약해줘: internal/server/server.go 변경 사항"
...토큰이 스트리밍으로 출력...
```

## 로드맵(요약)
- 하이브리드 검색(BM25+벡터)
- 코드/문서 청킹 개선, 심볼 그래프
- 편집 계획/패치 적용, 도구/훅 러너 고도화
- 세션 메모리/요약/세맨틱 캐시
- 관측/보안/성능 지표 확장
자세한 항목은 `docs/ROADMAP.md`, 진행상황은 `docs/TODO.md` 참고.

## 주의사항
- 파일/쉘 API는 프로젝트 루트 경계를 검사합니다(루트 밖 접근 차단).
- LLM 호출은 최소 간격/재시도 정책이 적용되며, 로컬 환경에서는 LM Studio 또는 사내 게이트웨이 사용을 권장합니다.

---
의견/기여 환영합니다. 문제나 제안은 이슈로 남겨주세요.
### 설정 파일
- 경로: `~/.mycoder/config.yaml` (또는 `config.yml`, `config.json`)
- 환경변수가 우선이며, 설정 파일의 값은 비어있는 환경변수에만 적용됩니다.
- 예시(YAML, 평면 키:값):
  ```yaml
  MYCODER_SERVER_URL: http://localhost:8089
  MYCODER_OPENAI_BASE_URL: http://localhost:1234/v1
  MYCODER_OPENAI_API_KEY: ""
  MYCODER_CHAT_MODEL: gpt-3.5-turbo
  MYCODER_EMBEDDING_MODEL: text-embedding-3-small
  MYCODER_SQLITE_PATH: /path/to/mycoder.db
  MYCODER_SHELL_DENY_REGEX: (?i)rm\s+-rf
  MYCODER_FS_ALLOW_REGEX: ^(internal/|cmd/)
  MYCODER_CURATOR_INTERVAL: 10m
  MYCODER_KNOWLEDGE_MIN_TRUST: 0.4
  MYCODER_KNOWLEDGE_DECAY_RATE: 0.01   # 주기마다 감소량(핀 제외)
  MYCODER_KNOWLEDGE_DECAY_AFTER_DAYS: 30  # 마지막 검증/생성 이후 N일 경과 시 decay 적용
  ```

### 데이터베이스 마이그레이션/시드
- SQLite 사용 시 앱이 자동으로 스키마 버전을 관리합니다.
- 롤백은 일부 버전에 한해 지원됩니다(신규 테이블 삭제 등). 컬럼 삭제는 미지원입니다.
- 시드 데이터: `MYCODER_DB_SEED=true` 설정 시 프로젝트가 비어있을 경우 `demo` 프로젝트를 자동 생성합니다.
- `MYCODER_LOG_LEVEL`: `debug|info|warn|error` (기본 `info`)
### 벡터 스토어 설정(옵션)
- `MYCODER_VECTOR_PROVIDER`: `noop`(기본) | `pgvector`
- `MYCODER_PGVECTOR_DSN`: pgvector 연결 문자열 (예: `postgres://user:pass@host:5432/db?sslmode=disable`)
- 현재 버전에서는 pgvector 어댑터가 스텁 상태이므로, 실제 KNN 검색은 추후 통합 예정입니다.
 - 임베딩 폴백: 임베딩 모델이 없거나 실패 시 서버가 자동으로 임베딩 기능을 비활성화하고, 레키시컬 검색만 사용합니다. 강제 비활성화는 `MYCODER_DISABLE_EMBEDDINGS=1`로 설정하세요.

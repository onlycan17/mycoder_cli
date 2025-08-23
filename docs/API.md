# REST API 명세(요약)

## 공통
- Base: `http://localhost:PORT`
- 인증: 로컬 기본(무), 외부 호출시 프로파일 토큰 사용.
- 요청 ID: 클라이언트가 `X-Request-ID` 헤더를 지정하면 그대로 반영하고, 없으면 서버가 생성하여 응답헤더 `X-Request-ID`로 반환. 모든 요청 로그에 `req_id` 필드 포함.
- 스트리밍: `/chat` SSE.

## POST /chat (SSE)
- 요청: `{ messages:[{role,content}], model?, stream?, temperature?, projectID?, retrieval?:{k} }`
- 응답:
  - `stream=true`: SSE 이벤트 스트림
    - `token`: `data: <text>` (증분 토큰)
    - `error`: `data: <message>` (에러 메시지)
    - `done`: 종료 이벤트
  - `stream=false`: `{ content: string }`
  - 동작: `projectID`가 있으면 RAG 검색 결과를 시스템 컨텍스트로 주입하여 인용 가능한 답변 유도

## POST /edits/plan
- 요청: `{ goal:string, files?:string[], projectID }`
- 응답: `{ plan:[{step,reason,targets}], confidence }`

## POST /edits/apply
- 요청: `{ patches:[{path,hunks[]}], projectID, runHooks?:boolean }`
- 응답: `{ status:"ok|failed", diffSummary, hooks:{fmt,lint,test}, logsRef }`

## POST /index/run
- 요청: `{ projectID, mode:"full|incremental" }`
- 응답: `{ jobID }`; `GET /index/jobs/:id` → `{ status, stats }`
 - 옵션 필드: `maxFiles?`, `maxBytes?`, `include?:string[]`, `exclude?:string[]`

### POST /index/run/stream (SSE)
- 요청: `{ projectID, mode:"full|incremental" }`
- 이벤트: `job`(잡ID), `progress`(`{indexed,total}`), `completed`(`{documents}`), `error`(메시지)
 - 옵션 필드: `maxFiles?`, `maxBytes?`, `include?:string[]`, `exclude?:string[]` 적용 가능

## POST /knowledge
- 요청: `{ projectID, sourceType:"code|doc|web", pathOrURL?, title?, text, trustScore?, pinned? }`
- 응답: `Knowledge`

## GET /knowledge
- 쿼리: `?projectID=<id>&minScore=0`
- 응답: `Knowledge[]`

## POST /knowledge/vet
- 요청: `{ projectID }`
- 응답: `{ updated: number }` (검증/점수화 배치 결과)

## POST /knowledge/promote
- 요청: `{ projectID, title, text, pathOrURL?, commitSHA?, files?:string[], symbols?:string[], pin?:boolean }`
- 응답: `Knowledge` (승격된 항목)

## POST /knowledge/promote/auto
- 설명: 주어진 파일 목록을 요약(LLM 사용 가능)하여 Knowledge 자동 생성
- 요청: `{ projectID, files:string[], title?:string, pin?:boolean }`
- 응답: `Knowledge`

## POST /knowledge/reverify
- 요청: `{ projectID }`
- 응답: `{ updated: number }`

## POST /knowledge/gc
- 요청: `{ projectID, minScore?: number }`
- 응답: `{ removed: number }`

## GET /search
- 쿼리: `?q=...&k=10&mode=hybrid`
- 응답: `{ results:[{chunkID, path, score, startLine, endLine, preview, source}], tookMs }`

## GET/POST /projects
- 생성: `{ name, rootPath, ignore?:string[] }` → `{ projectID }`

## POST /tools/hooks
- 요청: `{ projectID, targets?:string[], timeoutSec?:number, env?:{[k:string]:string} }`
- 동작: 프로젝트 루트에서 `make <target>` 순차 실행(기본 `fmt-check`, `test`, `lint`), 실패 시 즉시 중단. `env`는 화이트리스트 키만 반영(예: `GOFLAGS`).
- 응답: `{ <target>:{ ok:boolean, output:string, suggestion?:string, durationMs:number, lines:number, bytes:number }, ... }`
  - suggestion: 출력 패턴 기반 가이드(예: 포맷 실패→`make fmt`, 테스트 실패→`go test ./... -v`, lint 오류→`go vet ./...`)

## 헬스/메트릭
- `GET /healthz` → `200 OK`
- `GET /metrics`
  - 기본: Prometheus 텍스트 포맷(`text/plain; version=0.0.4`).
  - JSON: `?format=json` 또는 `Accept: application/json` 시 `{ projects, documents, jobs, knowledge }` 반환.
  - 포함 지표: `mycoder_projects`, `mycoder_documents`, `mycoder_jobs`, `mycoder_knowledge`, `mycoder_build_info{version,commit}`
  - HTTP 지표: `mycoder_http_requests_total{method,path,status}`, `mycoder_http_request_duration_seconds_{sum,count}{method,path}`
  - 라벨 정규화: 경로 변수는 템플릿으로 축약됨(예: `/index/jobs/abc` → `/index/jobs/:id`)
  - 샘플링: `MYCODER_METRICS_SAMPLE_RATE`(0.0~1.0, 기본 1.0)로 샘플링 비율 조절
- 백그라운드 큐레이터(옵션): 서버 기동 시 지식 재검증/정리 배치가 주기적으로 실행(`MYCODER_CURATOR_DISABLE`로 비활성화, `MYCODER_CURATOR_INTERVAL`, `MYCODER_KNOWLEDGE_MIN_TRUST`로 파라미터 제어)

## 파일시스템 API
- 보안: 기본적으로 프로젝트 루트 내부만 허용. 외부 경로 접근은 정책/플래그 필요.

### POST /fs/read
- 요청: `{ projectID, path }`
- 응답: `{ path, content, sha }`

### POST /fs/write
- 요청: `{ projectID, path, content, createIfMissing?:boolean, overwrite?:boolean }`
- 응답: `{ path, sha }`
 - 정책: `MYCODER_FS_ALLOW_REGEX`/`MYCODER_FS_DENY_REGEX`로 상대경로 허용·차단 제어

### POST /fs/patch
- 요청: `{ projectID, path, hunks:[{start,length,replace}] }`
- 응답: `{ ok:true }`
 - 정책: `MYCODER_FS_ALLOW_REGEX`/`MYCODER_FS_DENY_REGEX` 적용

### POST /fs/delete
- 요청: `{ projectID, path }`
- 응답: `{ ok:true }`
 - 정책: `MYCODER_FS_ALLOW_REGEX`/`MYCODER_FS_DENY_REGEX` 적용

## 터미널 실행 API
- 스트리밍: SSE. 시간/메모리/출력 제한, 허용/차단 목록.

### POST /shell/exec (비스트리밍)
- 요청: `{ projectID, cmd:string, args?:string[], cwd?:string, env?:{[k:string]:string}, timeoutSec?:number }`
- 응답: `{ exitCode:number, output:string, truncated?:boolean, outputBytes?:number, outputLines?:number }` (output은 안전을 위해 기본 64KiB로 캡)
- 실행 셸: zsh(`/bin/zsh -lc`)로 실행. `cwd`는 프로젝트 루트 하위만 허용, `env`는 화이트리스트 키만 반영(`GOFLAGS`,`GOWORK`,`CGO_ENABLED`).
- 정책: `MYCODER_SHELL_ALLOW_REGEX`/`MYCODER_SHELL_DENY_REGEX`로 실행 커맨드라인 허용·차단(정규식). 차단 시 403 반환.

### POST /shell/exec/stream (SSE)
- 요청: `{ projectID, cmd:string, args?:string[], cwd?:string, env?:{[k:string]:string}, timeoutSec?:number }`
- 이벤트: `stdout`, `stderr`, `summary`(`{bytes,lines,limited}`), 마지막 `exit` 이벤트에 종료코드 문자열 포함
- 실행 셸/보안 규칙은 `/shell/exec`와 동일

### POST /shell/exec/stream
- 설명: 단순 SSE 스트림(조합 출력). 요청 본문은 `/shell/exec`와 동일.
- 이벤트: `stdout`, `stderr`, 마지막 `exit` 이벤트에 종료코드 포함.

## MCP 연동 API(옵션)
### GET /mcp/tools
- 응답: `{ tools:[{name,description,params,paramsSchema}...] }`
  - `params`: 구버전 호환을 위한 파라미터 이름 리스트
  - `paramsSchema`: `{name,type,required}` 스키마 목록(가능 타입: string|number|boolean)
  - 보안: `MYCODER_MCP_ALLOWED_TOOLS` 설정 시 해당 목록에 포함된 도구만 노출

- ### POST /mcp/call
- 요청: `{ name:string, params:object }`
- 응답: `{ result:any, logs?:string }`
  - 보안:
    - `MYCODER_MCP_ALLOWED_TOOLS` 설정 시 목록 외 도구 호출 차단(403)
    - `MYCODER_MCP_REQUIRED_SCOPE` 설정 시 헤더 `X-MYCODER-Scope: <scope>:<tool>` 필요(예:`mcp:call:echo`)
## MCP (Minimal)

- GET `/mcp/tools`
  - 응답: `{ "tools": [{"name":"echo","description":"...","params":["text"],"paramsSchema":[{"name":"text","type":"string","required":true}]}] }`

- POST `/mcp/call`
  - 요청: `{ "name": "echo", "params": {"text": "hello"} }` (서버가 스키마 기반 검증 수행)
  - 응답: `{ "ok": true, "result": "hello" }` 혹은 `{ "ok": false, "error": "unknown tool" }`

## 에러 응답 형식(표준)

- 공통 에러 포맷(JSON):
  ```json
  {
    "error": "invalid_request",
    "message": "projectID required",
    "code": 400
  }
  ```
- 적용 대상: 잘못된 메서드(405), 잘못된 JSON(400), 필수 필드 누락(400), 미존재 리소스(400/404), 내부 오류(500)
- `/projects`, `/index/run`, `/index/run/stream` 등에서 표준 에러 포맷을 우선 적용했습니다. 나머지 엔드포인트도 순차 교체 예정입니다.
## Web Enrichment (Optional)

- POST `/web/search`
  - 요청: `{ "query": string, "limit"?: number }`
  - 응답: `{ "results": [{"title": string, "url": string, "snippet": string, "score": number}] }`
  - 비고: 기본은 비활성. `MYCODER_WEB_SEARCH_MOCK=1` 설정 시 모의 결과 반환.

- POST `/web/ingest`
  - 요청: `{ "projectID": string, "results": [{"title"?:string,"url":string,"snippet"?:string,"score"?:number}], "minScore"?: number, "dedupe"?: boolean }`
  - 동작: 결과를 정규화/중복 제거 후 Knowledge(sourceType="web")로 저장. 초기 trustScore는 `score` 기반 부여.
  - 응답: `{ "added": number }`

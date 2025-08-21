# REST API 명세(요약)

## 공통
- Base: `http://localhost:PORT`
- 인증: 로컬 기본(무), 외부 호출시 프로파일 토큰 사용.
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
- 응답: `{ <target>:{ok:boolean, output:string, suggestion?:string}, ... }`
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

### POST /fs/patch
- 요청: `{ projectID, path, hunks:[{start,length,replace}] }`
- 응답: `{ ok:true }`

### POST /fs/delete
- 요청: `{ projectID, path }`
- 응답: `{ ok:true }`

## 터미널 실행 API
- 스트리밍: SSE. 시간/메모리/출력 제한, 허용/차단 목록.

### POST /shell/exec (비스트리밍)
- 요청: `{ projectID, cmd:string, args?:string[], cwd?:string, env?:{[k:string]:string}, timeoutSec?:number }`
- 응답: `{ exitCode:number, output:string, truncated?:boolean }` (output은 안전을 위해 기본 64KiB로 캡)
- 실행 셸: zsh(`/bin/zsh -lc`)로 실행. `cwd`는 프로젝트 루트 하위만 허용, `env`는 화이트리스트 키만 반영(`GOFLAGS`,`GOWORK`,`CGO_ENABLED`).

### POST /shell/exec/stream (SSE)
- 요청: `{ projectID, cmd:string, args?:string[], cwd?:string, env?:{[k:string]:string}, timeoutSec?:number }`
- 이벤트: `stdout`, `stderr`, 마지막 `exit` 이벤트에 종료코드 문자열 포함
- 실행 셸/보안 규칙은 `/shell/exec`와 동일

### POST /shell/exec/stream
- 설명: 단순 SSE 스트림(조합 출력). 요청 본문은 `/shell/exec`와 동일.
- 이벤트: `stdout`, `stderr`, 마지막 `exit` 이벤트에 종료코드 포함.

## MCP 연동 API(옵션)
### GET /mcp/tools
- 응답: `{ tools:[{name,description,paramsSchema}...] }`

### POST /mcp/call
- 요청: `{ tool:string, params:any }`
- 응답: `{ result:any, logs?:string }`

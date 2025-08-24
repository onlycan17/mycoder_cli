# RAG 사전 시드(Seed) 계획서

## 목적
- 실행 전에 팀의 개발 지식(아키텍처, API, 데이터 모델, 운영 규칙, 레퍼런스)을 RAG 지식베이스에 일괄 주입하여, 초기 질의/검색/챗 품질을 끌어올린다.
- 핵심 문서는 고정(pin)하여 맥락에서 항상 우선 제공되도록 하고, 외부 레퍼런스는 TTL/신뢰도(trustScore)로 관리한다.

## 범위
- 내부 문서(이 리포지토리 내 `docs/`): PRD, 아키텍처, API, 데이터 모델, RAG, LLM, CLI UX, TESTING/CI, TOOLS, ROADMAP, MEMORY.
- 핵심 코드 가이드/설명: 서버/스토어/인덱서/리트리버 주요 흐름을 요약해 Knowledge로 승격.
- 외부 레퍼런스: RAG 논문/가이드, OpenAI/LM Studio API, Go/SQLite/FTS5/pgvector 레퍼런스.

## 분류 체계(태그)
- `kind`: `spec|guide|howto|reference|policy|summary`
- `domain`: `architecture|api|data|rag|llm|cli|testing|devops|search|vector|storage|server`
- `sourceType`: `doc|code|web`
- 선택: `ttlUntil`(웹), `version`, `component`, `pathOrURL`

## 시드 대상 목록
- 내부 문서(고정 pin, trust 0.9)
  - docs/PRD.md            {kind: spec, domain: architecture}
  - docs/ARCHITECTURE.md   {kind: spec, domain: architecture}
  - docs/API.md            {kind: spec, domain: api}
  - docs/DATA_MODEL.md     {kind: spec, domain: data}
  - docs/RAG.md            {kind: guide, domain: rag}
  - docs/LLM.md            {kind: guide, domain: llm}
  - docs/CLI_UX.md         {kind: guide, domain: cli}
  - docs/TESTING_CI.md     {kind: policy, domain: testing}
  - docs/TOOLS.md          {kind: guide, domain: devops}
  - docs/ROADMAP.md        {kind: reference, domain: architecture}
  - docs/MEMORY.md         {kind: guide, domain: rag}

- 코드 요약(요약 생성 pin, trust 0.8)
  - internal/server/server.go      {kind: summary, domain: server}
  - internal/indexer/indexer.go   {kind: summary, domain: search}
  - internal/rag/retriever/*      {kind: summary, domain: rag}
  - internal/patch/*              {kind: summary, domain: devops}
  - cmd/mycoder/main.go           {kind: summary, domain: cli}

- 외부 레퍼런스(웹, pin 아님, trust 0.6, TTL 90일)
  - RAG survey/tutorial: arXiv/Blog/Guide
  - OpenAI API: `/v1/chat`, `/v1/embeddings` 스펙
  - LM Studio(OpenAI 호환) 설명
  - Go 공식 문서(패키지 `net/http`, `database/sql`)
  - SQLite FTS5, pgvector 문서

## 메타데이터/신뢰도 정책
- 내부 문서: `pinned=true`, `trustScore=0.9`, `tags.kind=spec|guide`, `tags.domain` 지정
- 코드 요약: `pinned=true`, `trustScore=0.8`, `tags.kind=summary`, `tags.component=파일경로`
- 외부 레퍼런스: `pinned=false`, `trustScore=0.6`, `tags.kind=reference`, `tags.ttlUntil=+90d`
- 경로 중복/유사 문서 중복은 서버의 dedupe 규칙과 하이브리드 리랭크에서 보정

## 위험/품질
- 과도한 본문 길이 → `promote-auto`(요약) 사용, 요약 길이 제한
- 유사/중복 → 서버 측 dedupe 및 동일 `pathOrURL`/`domain`로 통합
- 민감정보 → 내부 문서 기준, 민감정보 포함시 제외/편집 후 저장

## 시드 절차(초안 커맨드)
- 프로젝트 생성: `mycoder projects create --name dev --root .`
- 내부 문서 자동 요약 승격(pin):
  - `mycoder knowledge promote-auto --project <ID> --title "PRD" --files docs/PRD.md --pin`
  - `mycoder knowledge promote-auto --project <ID> --title "Architecture" --files docs/ARCHITECTURE.md --pin`
  - `mycoder knowledge promote-auto --project <ID> --title "API" --files docs/API.md --pin`
  - `mycoder knowledge promote-auto --project <ID> --title "Data Model" --files docs/DATA_MODEL.md --pin`
  - `mycoder knowledge promote-auto --project <ID> --title "RAG" --files docs/RAG.md,docs/MEMORY.md --pin`
  - `mycoder knowledge promote-auto --project <ID> --title "LLM" --files docs/LLM.md --pin`
  - `mycoder knowledge promote-auto --project <ID> --title "CLI/Tools" --files docs/CLI_UX.md,docs/TOOLS.md --pin`
  - `mycoder knowledge promote-auto --project <ID> --title "Testing/CI" --files docs/TESTING_CI.md --pin`
  - `mycoder knowledge promote-auto --project <ID> --title "Roadmap" --files docs/ROADMAP.md --pin`
- 코드 요약 승격(pin):
  - `mycoder knowledge promote-auto --project <ID> --title "Server Overview" --files internal/server/server.go --pin`
  - `mycoder knowledge promote-auto --project <ID> --title "Indexer" --files internal/indexer/indexer.go --pin`
  - `mycoder knowledge promote-auto --project <ID> --title "Retriever" --files internal/rag/retriever/knn.go,internal/rag/retriever/bm25.go,internal/rag/retriever/hybrid.go --pin`
  - `mycoder knowledge promote-auto --project <ID> --title "Patch Utilities" --files internal/patch/unified.go,internal/patch/apply.go --pin`
  - `mycoder knowledge promote-auto --project <ID> --title "CLI Entrypoint" --files cmd/mycoder/main.go --pin`
- 외부 레퍼런스(예시):
  - `curl -X POST http://localhost:8089/web/ingest -H 'Content-Type: application/json' -d '{"projectID":"<ID>","results":[{"title":"RAG Survey 2024","url":"https://arxiv.org/abs/2407...","score":0.9},{"title":"OpenAI Chat API","url":"https://platform.openai.com/docs/api-reference/chat","score":0.8},{"title":"SQLite FTS5","url":"https://www.sqlite.org/fts5.html","score":0.8},{"title":"pgvector","url":"https://github.com/pgvector/pgvector","score":0.7}],"dedupe":true}'`

## 운영/정리
- 주기 재검증/Decay/GC: 기본 백그라운드 큐레이터 활성(환경 변수로 비활성 가능)
- 외부 레퍼런스는 TTL 경과 시 재수집 또는 제거
- 중복/낮은 신뢰도 항목은 `knowledge gc`로 정리

## 수용 기준(AC)
- `knowledge list`에서 내부 문서/요약 항목이 pin 및 태그/신뢰도로 저장됨
- 검색 질의에서 `architecture`, `API`, `retriever` 등 키워드에 인용이 안정적으로 포함됨
- 외부 레퍼런스는 TTL·신뢰도에 따라 리랭크되며, 중복 삽입되지 않음

## 후속 작업
- Makefile `seed-rag` 타깃 추가(옵션): 위 커맨드 일괄 실행 + 결과 요약 출력
- `tags.ttlUntil` 지원을 위한 간단 헬퍼(서버 측) 검토
- 기본 시드 이후 변경분 자동 promote 정책(옵션) 검토

